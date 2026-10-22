package mcpdeploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"harness-admin/internal/mcpadmin"
)

func manifestArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	if err := writer.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "GitHub archive"}}); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestNodeEntryFromArchive(t *testing.T) {
	for _, test := range []struct {
		name, manifest, want string
	}{
		{"start 优先", `{"main":"dist/index.js","scripts":{"start":"node build/index.js --transport stdio","build":"tsc"}}`, "build/index.js"},
		{"main 回退", `{"main":"./build/index.js","scripts":{"start":"npm run server"}}`, "build/index.js"},
		{"不安全入口", `{"main":"../../secret.js"}`, ""},
		{"无入口", `{"scripts":{"build":"tsc"}}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := manifestArchive(t, map[string]string{"repo-commit/package.json": test.manifest, "repo-commit/README.md": "# MCP"})
			got, err := nodeEntryFromArchive(archive)
			if got != test.want || (test.want == "" && err == nil) || (test.want != "" && err != nil) {
				t.Fatalf("入口=%q, 错误=%v, 期望=%q", got, err, test.want)
			}
		})
	}
}

func TestImportAutoDetectsNodeEntry(t *testing.T) {
	archive := manifestArchive(t, map[string]string{
		"repo-commit/README.md":    "# 菜谱 MCP",
		"repo-commit/package.json": `{"main":"build/index.js","scripts":{"build":"tsc","start":"node build/index.js"}}`,
		"repo-commit/src/index.ts": "console.log('test')",
	})
	repository := &repositoryStub{}
	runtime := &runtimeStub{}
	service := NewService(repository, &sourceStub{archive: archive}, runtime, nil, true)
	result, err := service.Import(t.Context(), Input{Actor: mcpadmin.Actor{OrganizationID: "org", UserID: "user"}, Name: "菜谱", SourceType: "github",
		SourceRef: "https://github.com/owner/repo", RuntimeType: "node"})
	if err != nil || result.Version.BuildStatus != "building" {
		t.Fatalf("构建未异步提交: %+v %v", result, err)
	}
	final := waitBuild(t, repository)
	if final.Version.BuildStatus != "succeeded" || repository.created.EntryFile != "build/index.js" ||
		!bytes.Equal(runtime.archive, archive) {
		t.Fatalf("自动识别失败: result=%+v input=%+v err=%v", final, repository.created, err)
	}
}

func TestImportMissingNodeEntryExplainsManualOverride(t *testing.T) {
	archive := manifestArchive(t, map[string]string{"repo-commit/README.md": "# MCP", "repo-commit/package.json": `{}`})
	repository := &repositoryStub{}
	service := NewService(repository, &sourceStub{archive: archive}, &runtimeStub{}, nil, true)
	result, err := service.Import(t.Context(), Input{Actor: mcpadmin.Actor{OrganizationID: "org", UserID: "user"}, Name: "测试", SourceType: "github",
		SourceRef: "https://github.com/owner/repo", RuntimeType: "node"})
	if err != nil || result.Version.BuildStatus != "building" {
		t.Fatalf("构建未异步提交: %+v %v", result, err)
	}
	final := waitBuild(t, repository)
	if final.Version.BuildStatus != "failed" || !strings.Contains(final.Version.ErrorMessage, "运行入口") {
		t.Fatalf("缺少入口时未提示手动指定: %+v", final)
	}
}

func TestInspectRepositoryReturnsFixedRevisionAndPlan(t *testing.T) {
	archive := manifestArchive(t, map[string]string{
		"repo-commit/README.md":    "# MCP",
		"repo-commit/package.json": `{"main":"build/index.js","scripts":{"build":"tsc"}}`,
	})
	service := NewService(&repositoryStub{}, &sourceStub{archive: archive}, &runtimeStub{}, nil, true)
	got, err := service.InspectRepository(t.Context(), "https://github.com/owner/repo", "main")
	if err != nil || got.Revision != strings.Repeat("a", 40) || got.EntryFile != "build/index.js" ||
		got.BuildCommand != "npm run build" || got.HasLockfile {
		t.Fatalf("源码预检结果不正确: %+v %v", got, err)
	}
}
