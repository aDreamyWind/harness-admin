package skilladmin

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
)

const maxPackageBytes = 16 * 1024 * 1024
const maxPackageFiles = 512

var ErrInvalidPackage = errors.New("Skill 代码包格式不正确")
var ErrSkillManifestMissing = fmt.Errorf("%w: 缺少 SKILL.md", ErrInvalidPackage)
var ErrMultipleSkillManifests = fmt.Errorf("%w: 包含多个 SKILL.md", ErrInvalidPackage)
var skillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type File struct {
	Path     string `json:"path"`
	Content  []byte `json:"-"`
	MIMEType string `json:"mimeType"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
}

type Package struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Frontmatter    json.RawMessage `json:"frontmatter"`
	ManifestDigest string          `json:"manifestDigest"`
	Files          []File          `json:"files"`
	SourceType     string          `json:"sourceType"`
	SourceRef      string          `json:"sourceRef"`
	SourceRevision string          `json:"sourceRevision"`
	SourcePath     string          `json:"sourcePath"`
}

func ParsePackage(input []byte) (Package, error) {
	if len(input) == 0 || len(input) > maxPackageBytes {
		return Package{}, ErrInvalidPackage
	}
	var files map[string][]byte
	var err error
	isZIP := len(input) >= 2 && input[0] == 'P' && input[1] == 'K'
	switch {
	case isZIP:
		files, err = parseZIPFiles(input)
	case len(input) >= 2 && input[0] == 0x1f && input[1] == 0x8b:
		files, err = parseTarGzipFiles(input)
	default:
		return Package{}, ErrInvalidPackage
	}
	if err != nil {
		return Package{}, err
	}
	if isZIP {
		return packageFromZIPFiles(files)
	}
	root := ""
	for file := range files {
		if file == "SKILL.md" {
			root = ""
			break
		}
		if strings.Count(file, "/") == 1 && strings.HasSuffix(file, "/SKILL.md") {
			if root != "" {
				return Package{}, ErrInvalidPackage
			}
			root = strings.TrimSuffix(file, "SKILL.md")
		}
	}
	return packageFromFiles(files, root)
}

// ZIP 中 GitHub 自动生成的 "仓库名-分支" 只是归档目录，不作为 Skill 名称。
// 只接收唯一的 SKILL.md 所在目录，避免把同仓库的其他内容误导入。
func packageFromZIPFiles(files map[string][]byte) (Package, error) {
	root := ""
	found := false
	for name := range files {
		if name != "SKILL.md" && path.Base(name) != "SKILL.md" {
			continue
		}
		if found {
			return Package{}, ErrMultipleSkillManifests
		}
		root = strings.TrimSuffix(name, "SKILL.md")
		found = true
	}
	if !found {
		return Package{}, ErrSkillManifestMissing
	}
	selected := make(map[string][]byte)
	for name, content := range files {
		if strings.HasPrefix(name, root) {
			selected[strings.TrimPrefix(name, root)] = content
		}
	}
	return packageFromFiles(selected, "")
}

func parseTarGzipFiles(input []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(input))
	if err != nil {
		return nil, ErrInvalidPackage
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	files := make(map[string][]byte)
	total := 0
	entries := 0
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || header == nil {
			return nil, ErrInvalidPackage
		}
		entries++
		if entries > 2048 {
			return nil, ErrInvalidPackage
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") ||
			path.Clean(name) != strings.TrimSuffix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return nil, ErrInvalidPackage
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return nil, ErrInvalidPackage
		}
		if header.Size < 0 || header.Size > maxPackageBytes-int64(total) || len(files) >= maxPackageFiles {
			return nil, ErrInvalidPackage
		}
		if _, exists := files[name]; exists {
			return nil, ErrInvalidPackage
		}
		data, err := io.ReadAll(io.LimitReader(archive, header.Size+1))
		if err != nil || int64(len(data)) != header.Size {
			return nil, ErrInvalidPackage
		}
		total += len(data)
		files[name] = data
	}
	return files, nil
}

func parseZIPFiles(input []byte) (map[string][]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil || len(archive.File) > 2048 {
		return nil, ErrInvalidPackage
	}
	files := make(map[string][]byte)
	total := 0
	for _, entry := range archive.File {
		name := strings.TrimPrefix(entry.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") ||
			path.Clean(name) != strings.TrimSuffix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return nil, ErrInvalidPackage
		}
		mode := entry.Mode()
		if mode.IsDir() {
			continue
		}
		if !mode.IsRegular() || mode&os.ModeSymlink != 0 ||
			entry.UncompressedSize64 > uint64(maxPackageBytes-total) {
			return nil, ErrInvalidPackage
		}
		// Finder 创建的 ZIP 可能附带资源分叉及目录元数据；它们不是 Skill 文件。
		if strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store" {
			continue
		}
		if len(files) >= maxPackageFiles {
			return nil, ErrInvalidPackage
		}
		if _, exists := files[name]; exists {
			return nil, ErrInvalidPackage
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, ErrInvalidPackage
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, int64(maxPackageBytes-total)+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) > maxPackageBytes-total ||
			uint64(len(data)) != entry.UncompressedSize64 {
			return nil, ErrInvalidPackage
		}
		total += len(data)
		files[name] = data
	}
	return files, nil
}

func packageFromFiles(files map[string][]byte, root string) (Package, error) {
	skillMD, exists := files[root+"SKILL.md"]
	if !exists || len(skillMD) == 0 || len(skillMD) > 1024*1024 || !utf8.Valid(skillMD) {
		return Package{}, ErrInvalidPackage
	}
	if root != "" {
		for file := range files {
			if !strings.HasPrefix(file, root) {
				return Package{}, ErrInvalidPackage
			}
		}
	}
	frontmatter, err := parseFrontmatter(skillMD)
	if err != nil {
		return Package{}, err
	}
	name, _ := frontmatter["name"].(string)
	description, _ := frontmatter["description"].(string)
	if !skillName.MatchString(name) || len(name) > 64 || len(description) == 0 || len(description) > 1024 ||
		(root != "" && strings.TrimSuffix(root, "/") != name) {
		return Package{}, ErrInvalidPackage
	}
	encoded, err := json.Marshal(frontmatter)
	if err != nil {
		return Package{}, ErrInvalidPackage
	}
	paths := make([]string, 0, len(files))
	for file := range files {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	manifest := sha256.New()
	result := Package{Name: name, Description: description, Frontmatter: encoded, Files: make([]File, 0, len(paths)), SourceType: "upload"}
	for _, original := range paths {
		relative := strings.TrimPrefix(original, root)
		data := files[original]
		digest := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		mimeType := http.DetectContentType(data)
		if strings.HasSuffix(strings.ToLower(relative), ".md") {
			mimeType = "text/markdown"
		}
		result.Files = append(result.Files, File{Path: relative, Content: data, MIMEType: mimeType, Digest: digest, Size: len(data)})
		_, _ = fmt.Fprintf(manifest, "%s\x00%s\x00%d\x00", relative, digest, len(data))
	}
	result.ManifestDigest = fmt.Sprintf("sha256:%x", manifest.Sum(nil))
	return result, nil
}

func parseFrontmatter(data []byte) (map[string]any, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, ErrInvalidPackage
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return nil, ErrInvalidPackage
	}
	var value map[string]any
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &value); err != nil || value == nil {
		return nil, ErrInvalidPackage
	}
	return value, nil
}

func validSkillPath(value string) bool {
	return value != "" && len(value) <= 2048 && !strings.HasPrefix(value, "/") &&
		!strings.ContainsAny(value, "\\\x00") && path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../")
}
