package mcpruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"harness-admin/internal/mcpdeploy"
)

func tarball(t *testing.T, name string, kind byte, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: kind, Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if content != "" {
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

func TestUnpackAcceptsSingleRootDirectory(t *testing.T) {
	root := t.TempDir()
	source, err := unpack(tarball(t, "repo-commit/src/server.js", tar.TypeReg, "console.log(1)"), root)
	if err != nil || source != filepath.Join(root, "repo-commit") {
		t.Fatalf("根目录 = %q, err=%v", source, err)
	}
	data, err := os.ReadFile(filepath.Join(source, "src/server.js"))
	if err != nil || string(data) != "console.log(1)" {
		t.Fatalf("解包内容错误: %q, %v", data, err)
	}
}

func TestUnpackIgnoresPAXGlobalHeader(t *testing.T) {
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	if err := writer.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "GitHub archive"}}); err != nil {
		t.Fatal(err)
	}
	contents := []byte("console.log(1)")
	if err := writer.WriteHeader(&tar.Header{Name: "repo-commit/src/server.js", Mode: 0644, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	source, err := unpack(buffer.Bytes(), root)
	if err != nil || source != filepath.Join(root, "repo-commit") {
		t.Fatalf("PAX 头不应影响源码根目录: %q %v", source, err)
	}
}

func TestUnpackRejectsTraversalAndLinks(t *testing.T) {
	for _, test := range []struct {
		name string
		kind byte
	}{
		{"../secret", tar.TypeReg}, {"/absolute", tar.TypeReg}, {"safe/../secret", tar.TypeReg},
		{"src/link", tar.TypeSymlink}, {"Dockerfile.hadmin", tar.TypeReg},
	} {
		root := t.TempDir()
		_, err := unpack(tarball(t, test.name, test.kind, ""), root)
		if err == nil {
			t.Fatalf("未拒绝 %s", test.name)
		}
		if strings.Contains(test.name, "secret") {
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "secret")); !os.IsNotExist(statErr) {
				t.Fatalf("解包越界写入: %v", statErr)
			}
		}
	}
}

func TestNodeDockerfileBuildsBeforeCheckingEntry(t *testing.T) {
	dockerfile := dockerfileFor("node", "node:22-alpine", "build/index.js")
	install := strings.Index(dockerfile, "npm install --ignore-scripts")
	compile := strings.Index(dockerfile, "npm run build --if-present")
	check := strings.Index(dockerfile, "test -f /app/build/index.js")
	prune := strings.Index(dockerfile, "npm prune --omit=dev")
	if install < 0 || compile <= install || check <= compile || prune <= check ||
		!strings.Contains(dockerfile, "RUN --network=none if [ -f package.json ]; then npm run build") ||
		!strings.Contains(dockerfile, "COPY --from=builder") {
		t.Fatalf("Node 构建与运行阶段顺序不正确:\n%s", dockerfile)
	}
}

func TestDockerBuildRejectsUnsafeEntryBeforeCreatingFiles(t *testing.T) {
	runtime := NewDocker("node:22-alpine", "python:3.12-slim", "none")
	for _, entry := range []string{"../secret.js", "index.js;touch hacked", "/tmp/server.js"} {
		if _, _, err := runtime.Build(t.Context(), "service", "version", "node", entry, nil); err == nil {
			t.Fatalf("未拒绝运行入口 %q", entry)
		}
	}
}

func TestOfflineBuildRejectsExternalDependenciesBeforeDockerRuns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"zod":"^3.0.0"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireDependencyNetwork("node", root); !errors.Is(err, mcpdeploy.ErrDependencyNetwork) {
		t.Fatalf("离线 npm 依赖未提前拒绝: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"build":"echo ok"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireDependencyNetwork("node", root); err != nil {
		t.Fatalf("无依赖项目不应被拒绝: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "requirements.txt"), []byte("httpx==0.27.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireDependencyNetwork("python", root); !errors.Is(err, mcpdeploy.ErrDependencyNetwork) {
		t.Fatalf("离线 Python 依赖未提前拒绝: %v", err)
	}
}
