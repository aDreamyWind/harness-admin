package mcpdeploy

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRepositoryURL(t *testing.T) {
	for _, test := range []struct{ raw, provider, repository string }{
		{"https://github.com/acme/tool", "github", "acme/tool"},
		{"https://gitee.com/acme/tool.git", "gitee", "acme/tool"},
		{"https://github.com/acme/tool.git/", "github", "acme/tool"},
		{"https://gitee.com/acme/tool/", "gitee", "acme/tool"},
	} {
		provider, repository, err := ParseRepositoryURL(test.raw)
		if err != nil || provider != test.provider || repository != test.repository {
			t.Fatalf("解析错误 %q: %s %s %v", test.raw, provider, repository, err)
		}
	}
	for _, raw := range []string{"http://github.com/a/b", "https://github.com.evil.test/a/b", "https://github.com/a/b/tree/main", "https://user@github.com/a/b", "https://gitee.com/a/b?token=secret", "https://github.com/a/%2e%2e"} {
		if _, _, err := ParseRepositoryURL(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("应拒绝仓库地址 %q: %v", raw, err)
		}
	}
}

func TestReadmeFromArchive(t *testing.T) {
	archive := testArchive(t, "# MCP 工具\n\n服务介绍")
	got, err := readmeFromArchive(archive)
	if err != nil || got != "# MCP 工具\n\n服务介绍" {
		t.Fatalf("README 读取失败: %q %v", got, err)
	}
	if _, err := readmeFromArchive(testArchive(t, "  ")); err == nil {
		t.Fatal("空 README 应失败")
	}
	longReadme := strings.Repeat("内容", 6000)
	if got, err := readmeFromArchive(testArchive(t, longReadme)); err != nil || got != longReadme {
		t.Fatalf("较长 README 不应被截断: 长度=%d 错误=%v", len(got), err)
	}
	if _, err := readmeFromArchive([]byte("not gzip")); err == nil {
		t.Fatal("损坏代码包应失败")
	}
}

func TestReadmeFromArchiveIgnoresPAXGlobalHeader(t *testing.T) {
	archive := manifestArchive(t, map[string]string{
		"repo-commit/README.md":    "# 仓库说明",
		"repo-commit/package.json": `{"main":"build/index.js"}`,
	})
	readme, err := readmeFromArchive(archive)
	if err != nil || readme != "# 仓库说明" {
		t.Fatalf("PAX 头不应被误认为另一个顶层目录: %q %v", readme, err)
	}
}
