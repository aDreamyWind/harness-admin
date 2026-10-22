package mcpdeploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var repositoryPartPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var sourceRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)

// ParseRepositoryURL 只接受两个已支持平台的公开仓库地址，拒绝自定义下载目标。
func ParseRepositoryURL(raw string) (provider, repository string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", ErrInvalidInput
	}
	switch u.Hostname() {
	case "github.com", "gitee.com":
	default:
		return "", "", ErrInvalidInput
	}
	repositoryPath := strings.Trim(u.EscapedPath(), "/")
	parts := strings.Split(strings.TrimSuffix(repositoryPath, ".git"), "/")
	if len(parts) != 2 || !repositoryPartPattern.MatchString(parts[0]) || !repositoryPartPattern.MatchString(parts[1]) ||
		strings.Contains(parts[0], "..") || strings.Contains(parts[1], "..") {
		return "", "", ErrInvalidInput
	}
	if u.Hostname() == "github.com" {
		provider = "github"
	} else {
		provider = "gitee"
	}
	return provider, parts[0] + "/" + parts[1], nil
}

func validRef(ref string) bool {
	return ref == "" || (sourceRefPattern.MatchString(ref) && !strings.Contains(ref, "..") &&
		!strings.Contains(ref, "//") && !strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".lock"))
}

func readmeFromArchive(archive []byte) (string, error) {
	if len(archive) == 0 || len(archive) > 20*1024*1024 {
		return "", errors.New("代码包为空或超过 20 MiB")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", errors.New("代码包不是有效的 .tar.gz 文件")
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	var rootReadme string
	wrappedReadmes := make(map[string]string)
	topLevels := make(map[string]bool)
	var total int64
	for count := 0; count < 2000; count++ {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			if rootReadme != "" {
				return rootReadme, nil
			}
			if len(topLevels) == 1 {
				for root := range topLevels {
					if readme := wrappedReadmes[root]; readme != "" {
						return readme, nil
					}
				}
			}
			return "", errors.New("代码包根目录缺少非空的 README.md")
		}
		if err != nil {
			return "", errors.New("代码包 tar 内容损坏")
		}
		total += header.Size
		if total > 80*1024*1024 {
			return "", errors.New("代码包解压后超过 80 MiB")
		}
		// GitHub tar 包可能包含 PAX 全局元数据；它不是源码根目录。
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name := strings.TrimPrefix(header.Name, "./")
		parts := strings.Split(strings.Trim(name, "/"), "/")
		if len(parts) > 0 {
			topLevels[parts[0]] = true
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if len(parts) < 1 || len(parts) > 2 || !strings.EqualFold(parts[len(parts)-1], "README.md") {
			continue
		}
		if header.Size > 64*1024 {
			return "", errors.New("README.md 超过 64 KiB")
		}
		contents, err := io.ReadAll(io.LimitReader(reader, 64*1024+1))
		if err != nil || !utf8.Valid(contents) || bytes.IndexByte(contents, 0) >= 0 {
			return "", errors.New("README.md 不是有效 UTF-8 文本")
		}
		readme := strings.TrimSpace(string(contents))
		if len(parts) == 1 {
			if rootReadme != "" {
				return "", errors.New("代码包中存在多个根目录 README.md")
			}
			rootReadme = readme
		} else {
			if wrappedReadmes[parts[0]] != "" {
				return "", errors.New("代码包中存在多个根目录 README.md")
			}
			wrappedReadmes[parts[0]] = readme
		}
	}
	return "", errors.New("代码包文件数量过多")
}
