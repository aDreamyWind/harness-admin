package skilladmin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
)

var githubRepositoryPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var githubRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
var githubCommit = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type RepositorySource interface {
	Resolve(context.Context, string, string, string) (string, error)
	Fetch(context.Context, string, string, string) ([]byte, error)
}

func parseGitHubRepository(raw string) (normalized, repository string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", "", ErrInvalidInput
	}
	parts := strings.Split(strings.TrimSuffix(strings.Trim(u.EscapedPath(), "/"), ".git"), "/")
	if len(parts) != 2 || !githubRepositoryPart.MatchString(parts[0]) || !githubRepositoryPart.MatchString(parts[1]) ||
		strings.Contains(parts[0], "..") || strings.Contains(parts[1], "..") {
		return "", "", ErrInvalidInput
	}
	repository = parts[0] + "/" + parts[1]
	return "https://github.com/" + repository, repository, nil
}

func validGitHubRef(ref string) bool {
	return ref == "" || githubRef.MatchString(ref) && !strings.Contains(ref, "..") &&
		!strings.Contains(ref, "//") && !strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".lock")
}

// ParseRepositoryArchive 只取指定 Skill 目录，忽略仓库其他文件，同时限制整体解压规模。
func ParseRepositoryArchive(input []byte, skillPath string) (Package, error) {
	if len(input) == 0 || len(input) > 20*1024*1024 ||
		(skillPath != "" && (skillPath == "." || !validSkillPath(skillPath))) {
		return Package{}, ErrInvalidPackage
	}
	gz, err := gzip.NewReader(bytes.NewReader(input))
	if err != nil {
		return Package{}, ErrInvalidPackage
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	root := ""
	files := make(map[string][]byte)
	var archiveBytes, selectedBytes int64
	for entries := 0; ; entries++ {
		if entries >= 20000 {
			return Package{}, ErrInvalidPackage
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || header == nil || header.Size < 0 {
			return Package{}, ErrInvalidPackage
		}
		archiveBytes += header.Size
		if archiveBytes > 128*1024*1024 {
			return Package{}, ErrInvalidPackage
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") ||
			path.Clean(name) != strings.TrimSuffix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return Package{}, ErrInvalidPackage
		}
		parts := strings.SplitN(strings.TrimSuffix(name, "/"), "/", 2)
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return Package{}, ErrInvalidPackage
		}
		if len(parts) < 2 || header.Typeflag == tar.TypeDir {
			continue
		}
		relative := parts[1]
		if skillPath != "" {
			if !strings.HasPrefix(relative, skillPath+"/") {
				continue
			}
			relative = strings.TrimPrefix(relative, skillPath+"/")
		}
		if !validSkillPath(relative) || relative == "." ||
			(header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) ||
			len(files) >= maxPackageFiles || header.Size > maxPackageBytes-selectedBytes {
			return Package{}, ErrInvalidPackage
		}
		if _, exists := files[relative]; exists {
			return Package{}, ErrInvalidPackage
		}
		content, err := io.ReadAll(io.LimitReader(archive, header.Size+1))
		if err != nil || int64(len(content)) != header.Size {
			return Package{}, ErrInvalidPackage
		}
		selectedBytes += int64(len(content))
		files[relative] = content
	}
	return packageFromFiles(files, "")
}
