package mcpruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpdeploy"
)

type Docker struct {
	NodeImage    string
	PythonImage  string
	BuildNetwork string
}

func NewDocker(nodeImage, pythonImage, buildNetwork string) *Docker {
	return &Docker{NodeImage: nodeImage, PythonImage: pythonImage, BuildNetwork: buildNetwork}
}

func dockerfileFor(runtime, baseImage, entry string) string {
	if runtime == "python" {
		return fmt.Sprintf("FROM %s\nWORKDIR /app\nCOPY . /app\nRUN if [ -f requirements.txt ]; then pip install --no-cache-dir --only-binary=:all: -r requirements.txt; fi\nRUN adduser --disabled-password --gecos '' --uid 10001 mcpuser 2>/dev/null || adduser -D -u 10001 mcpuser\nRUN chown -R 10001:10001 /app\nUSER 10001\n", baseImage)
	}
	// package-lock.json 缺失时允许 npm install，但生成的锁文件保留在镜像中供排查。
	// npm 安装脚本禁用；仓库声明的 build 脚本仅在 Docker 隔离构建阶段执行，且该步骤断网。
	return fmt.Sprintf(`FROM %[1]s AS builder
WORKDIR /app
COPY . /app
RUN adduser --disabled-password --gecos '' --uid 10001 mcpuser 2>/dev/null || adduser -D -u 10001 mcpuser
RUN chown -R 10001:10001 /app
USER 10001
ENV HOME=/tmp
RUN if [ -f package-lock.json ]; then npm ci --ignore-scripts --no-audit --no-fund; elif [ -f package.json ]; then echo '警告：缺少 package-lock.json，本次构建将生成依赖锁文件'; npm install --ignore-scripts --no-audit --no-fund; fi
RUN --network=none if [ -f package.json ]; then npm run build --if-present; fi
RUN test -f /app/%[2]s || (echo '运行入口未在构建后生成：%[2]s' >&2; exit 1)
RUN --network=none if [ -f package.json ]; then npm prune --omit=dev --ignore-scripts --offline --no-audit --no-fund; fi
FROM %[1]s
WORKDIR /app
RUN adduser --disabled-password --gecos '' --uid 10001 mcpuser 2>/dev/null || adduser -D -u 10001 mcpuser
COPY --from=builder --chown=10001:10001 /app /app
USER 10001
`, baseImage, entry)
}

func (d *Docker) Build(ctx context.Context, serviceID, versionID, runtime, entry string, archive []byte) (string, string, error) {
	if (runtime != "node" && runtime != "python") || entry == "" || len(entry) > 240 ||
		path.Clean(entry) != entry || strings.HasPrefix(entry, "/") || strings.HasPrefix(entry, "-") ||
		strings.HasPrefix(entry, "../") || strings.IndexFunc(entry, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '-' || r == '.' || r == '/')
	}) >= 0 {
		return "", "", errors.New("运行入口路径不正确")
	}
	directory, err := os.MkdirTemp("", "hadmin-mcp-build-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(directory)
	sourceRoot, err := unpack(archive, directory)
	if err != nil {
		return "", "", err
	}
	if d.BuildNetwork == "none" {
		if err := requireDependencyNetwork(runtime, sourceRoot); err != nil {
			return "", "", err
		}
	}
	baseImage := d.NodeImage
	if runtime == "python" {
		baseImage = d.PythonImage
		info, err := os.Stat(filepath.Join(sourceRoot, filepath.FromSlash(entry)))
		if err != nil || !info.Mode().IsRegular() {
			return "", "", errors.New("Python 运行入口在代码包中不存在")
		}
	}
	if baseImage == "" {
		return "", "", errors.New("MCP 容器基础镜像未配置")
	}
	// Dockerfile 不取自第三方仓库。Node 构建阶段安装开发依赖并离线编译，
	// 编译后才校验入口；正式运行镜像仅保留生产依赖，并以非 root 用户运行。
	dockerfile := dockerfileFor(runtime, baseImage, entry)
	if err := os.WriteFile(filepath.Join(sourceRoot, "Dockerfile.hadmin"), []byte(dockerfile), 0600); err != nil {
		return "", "", err
	}
	tag := "hadmin-mcp-" + strings.ReplaceAll(serviceID, "-", "") + ":" + strings.ReplaceAll(versionID, "-", "")
	log, err := run(ctx, "build", "--network", d.BuildNetwork, "--label", "com.harness-admin.service-id="+serviceID,
		"--label", "com.harness-admin.version-id="+versionID,
		"-f", filepath.Join(sourceRoot, "Dockerfile.hadmin"), "-t", tag, sourceRoot)
	if err != nil {
		return "", log, fmt.Errorf("容器构建失败: %w", err)
	}
	imageID, err := run(ctx, "image", "inspect", "--format", "{{.Id}}", tag)
	if err != nil {
		return "", log, fmt.Errorf("读取构建镜像失败: %w", err)
	}
	return strings.TrimSpace(imageID), log, nil
}

func requireDependencyNetwork(runtime, root string) error {
	if runtime == "python" {
		contents, err := os.ReadFile(filepath.Join(root, "requirements.txt"))
		if err == nil && strings.TrimSpace(string(contents)) != "" {
			return mcpdeploy.ErrDependencyNetwork
		}
		return errIfNotMissing(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return errIfNotMissing(err)
	}
	var manifest struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return errors.New("package.json 格式不正确")
	}
	if len(manifest.Dependencies)+len(manifest.DevDependencies) > 0 {
		return mcpdeploy.ErrDependencyNetwork
	}
	return nil
}

func errIfNotMissing(err error) error {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (d *Docker) Start(ctx context.Context, service mcpadmin.ServiceRecord) error {
	if !validManaged(service) || service.ImageRef == "" {
		return mcpdeploy.ErrNotReady
	}
	label, currentImage, running, err := d.inspect(ctx, service.ContainerName)
	if err == nil {
		if label != service.ID {
			return errors.New("容器名称已被其他对象占用")
		}
		if running && currentImage == service.ImageRef {
			return nil
		}
		if _, err := run(ctx, "rm", "-f", service.ContainerName); err != nil {
			return err
		}
	} else if !isMissingContainer(err) {
		return err
	}
	_, err = run(ctx, "run", "-d", "--name", service.ContainerName,
		"--label", "com.harness-admin.service-id="+service.ID,
		"--network", "none", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--pids-limit", "64", "--memory", "256m", "--cpus", "1",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=32m", service.ImageRef, "sleep", "infinity")
	return err
}

func (d *Docker) Stop(ctx context.Context, service mcpadmin.ServiceRecord) error {
	if !validManaged(service) {
		return mcpdeploy.ErrInvalidInput
	}
	label, _, _, err := d.inspect(ctx, service.ContainerName)
	if err != nil {
		if isMissingContainer(err) {
			return nil
		}
		return err
	}
	if label != service.ID {
		return errors.New("容器名称已被其他对象占用")
	}
	_, err = run(ctx, "rm", "-f", service.ContainerName)
	return err
}

func (d *Docker) Inspect(ctx context.Context, service mcpadmin.ServiceRecord) (mcpdeploy.RuntimeStatus, error) {
	if !validManaged(service) {
		return mcpdeploy.RuntimeStatus{}, mcpdeploy.ErrInvalidInput
	}
	label, image, running, err := d.inspect(ctx, service.ContainerName)
	if err != nil {
		if isMissingContainer(err) {
			return mcpdeploy.RuntimeStatus{Status: "stopped"}, nil
		}
		return mcpdeploy.RuntimeStatus{}, err
	}
	if label != service.ID || image != service.ImageRef {
		return mcpdeploy.RuntimeStatus{}, errors.New("运行容器与登记版本不一致")
	}
	status := "stopped"
	if running {
		status = "running"
	}
	logs, logErr := run(ctx, "logs", "--tail", "200", service.ContainerName)
	if logErr != nil {
		return mcpdeploy.RuntimeStatus{Running: running, Status: status}, logErr
	}
	return mcpdeploy.RuntimeStatus{Running: running, Status: status, Logs: logs}, nil
}

func (d *Docker) inspect(ctx context.Context, container string) (string, string, bool, error) {
	result, err := run(ctx, "inspect", "--format", "{{index .Config.Labels \"com.harness-admin.service-id\"}}|{{.Config.Image}}|{{.State.Running}}", container)
	if err != nil {
		return "", "", false, err
	}
	parts := strings.Split(strings.TrimSpace(result), "|")
	if len(parts) != 3 {
		return "", "", false, errors.New("容器状态格式不正确")
	}
	return parts[0], parts[1], parts[2] == "true", nil
}

func validManaged(service mcpadmin.ServiceRecord) bool {
	return service.Transport == "stdio" && service.SourceType != "remote" &&
		service.ContainerName == "hadmin-mcp-"+service.ID &&
		(service.RuntimeType == "node" || service.RuntimeType == "python")
}

type tailBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return n, nil
}

type dockerError struct {
	output string
	cause  error
}

func (e dockerError) Error() string { return e.cause.Error() + ": " + strings.TrimSpace(e.output) }
func (e dockerError) Unwrap() error { return e.cause }

func isMissingContainer(err error) bool {
	var value dockerError
	if !errors.As(err, &value) {
		return false
	}
	message := strings.ToLower(value.output)
	return strings.Contains(message, "no such object") || strings.Contains(message, "no such container")
}

func run(ctx context.Context, arguments ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", arguments...)
	output := &tailBuffer{limit: 64 * 1024}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if err != nil {
		return string(output.data), dockerError{output: string(output.data), cause: err}
	}
	return string(output.data), nil
}

func unpack(archive []byte, root string) (string, error) {
	if len(archive) == 0 || len(archive) > 20*1024*1024 {
		return "", errors.New("代码包大小超出限制")
	}
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", errors.New("代码包不是 .tar.gz 格式")
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var count int
	var total int64
	topLevels := map[string]bool{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errors.New("代码包解压失败")
		}
		count++
		if count > 2000 {
			return "", errors.New("代码包文件过多")
		}
		// PAX 扩展头仅携带 tar 元信息，不能写入构建目录或算作顶层目录。
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name ||
			name == ".." || strings.HasPrefix(name, "../") || len(name) > 400 {
			return "", errors.New("代码包包含不安全路径")
		}
		parts := strings.Split(name, "/")
		for _, part := range parts {
			if part == ".git" || part == "Dockerfile.hadmin" {
				return "", errors.New("代码包包含保留文件")
			}
		}
		topLevels[parts[0]] = true
		full := filepath.Join(root, filepath.FromSlash(name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(full, 0755); err != nil {
				return "", err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 10*1024*1024 || total+header.Size > 80*1024*1024 {
				return "", errors.New("代码包解压后超过限制")
			}
			total += header.Size
			if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
				return "", err
			}
			file, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err != nil {
				return "", err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		default:
			return "", errors.New("代码包包含链接或特殊文件")
		}
	}
	if count == 0 {
		return "", errors.New("代码包为空")
	}
	if len(topLevels) == 1 {
		for name := range topLevels {
			candidate := filepath.Join(root, name)
			info, err := os.Stat(candidate)
			if err == nil && info.IsDir() {
				return candidate, nil
			}
		}
	}
	return root, nil
}
