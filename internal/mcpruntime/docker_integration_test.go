package mcpruntime_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/mcpadmin"
	"harness-admin/internal/mcpclient"
	"harness-admin/internal/mcpruntime"
)

const echoServer = `const readline = require('readline');
const rl = readline.createInterface({input: process.stdin});
rl.on('line', line => {
  const request = JSON.parse(line);
  if (request.id === undefined) return;
  let result;
  if (request.method === 'initialize') {
    result = {protocolVersion: request.params.protocolVersion, capabilities: {tools: {}}, serverInfo: {name: 'echo-test', version: '1.0.0'}};
  } else if (request.method === 'tools/list') {
    result = {tools: [{name: 'echo', description: '回显文本', inputSchema: {type: 'object', properties: {text: {type: 'string'}}, required: ['text']}}]};
  } else if (request.method === 'tools/call') {
    result = {content: [{type: 'text', text: request.params.arguments.text}]};
  } else {
    process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: request.id, error: {code: -32601, message: 'Not found'}}) + '\n');
    return;
  }
  process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: request.id, result}) + '\n');
});`

const pythonEchoServer = `import sys, json
for line in sys.stdin:
    request = json.loads(line)
    if 'id' not in request:
        continue
    method = request.get('method')
    if method == 'initialize':
        result = {'protocolVersion': request['params']['protocolVersion'], 'capabilities': {'tools': {}}, 'serverInfo': {'name': 'python-echo-test', 'version': '1.0.0'}}
    elif method == 'tools/list':
        result = {'tools': [{'name': 'echo', 'inputSchema': {'type': 'object', 'properties': {'text': {'type': 'string'}}}}]}
    elif method == 'tools/call':
        result = {'content': [{'type': 'text', 'text': request['params']['arguments']['text']}]}
    else:
        print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'error': {'code': -32601, 'message': 'Not found'}}), flush=True)
        continue
    print(json.dumps({'jsonrpc': '2.0', 'id': request['id'], 'result': result}), flush=True)
`

func TestDockerStdioMCP(t *testing.T) {
	if os.Getenv("HARNESS_TEST_DOCKER_MCP") != "1" {
		t.Skip("设置 HARNESS_TEST_DOCKER_MCP=1 后验证真实 Docker Stdio 工具调用")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	serviceID, versionID := uuid.NewString(), uuid.NewString()
	tag := "hadmin-mcp-" + strings.ReplaceAll(serviceID, "-", "") + ":" + strings.ReplaceAll(versionID, "-", "")
	runtime := mcpruntime.NewDocker("node:22-alpine", "python:3.12-slim", "none")
	archive := testArchive(t, "echo.js", echoServer)
	image, log, err := runtime.Build(ctx, serviceID, versionID, "node", "echo.js", archive)
	if err != nil {
		t.Fatalf("构建失败: %v\n%s", err, log)
	}
	t.Cleanup(func() {
		command := exec.Command("docker", "image", "rm", tag)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("清理测试镜像失败: %v %s", err, output)
		}
	})
	service := mcpadmin.ServiceRecord{ID: serviceID, SourceType: "upload", Transport: "stdio", RuntimeType: "node",
		EntryFile: "echo.js", ImageRef: image, ContainerName: "hadmin-mcp-" + serviceID, DeploymentStatus: "running"}
	t.Cleanup(func() {
		if err := runtime.Stop(context.Background(), service); err != nil {
			t.Errorf("清理测试容器失败: %v", err)
		}
	})
	if err := runtime.Start(ctx, service); err != nil {
		t.Fatal(err)
	}
	labels := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .Config.Labels}}", service.ContainerName)
	output, err := labels.CombinedOutput()
	if err != nil {
		t.Fatalf("读取容器标签失败: %v %s", err, output)
	}
	var containerLabels map[string]string
	if err := json.Unmarshal(output, &containerLabels); err != nil {
		t.Fatalf("解析容器标签失败: %v", err)
	}
	if containerLabels["com.docker.compose.project"] != "hadmin-mcp" ||
		containerLabels["com.docker.compose.service"] != service.ContainerName ||
		containerLabels["com.harness-admin.service-id"] != service.ID {
		t.Fatalf("容器分组或归属标签错误: %v", containerLabels)
	}
	status, err := runtime.Inspect(ctx, service)
	if err != nil || !status.Running {
		t.Fatalf("容器未运行: %+v %v", status, err)
	}
	client := mcpclient.New(nil)
	tools, err := client.ListTools(ctx, service, mcpadmin.UpstreamCredentials{})
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("工具发现失败: %+v %v", tools, err)
	}
	result, err := client.CallTool(ctx, service, mcpadmin.UpstreamCredentials{}, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil || !strings.Contains(string(result), "hello") {
		t.Fatalf("工具调用失败: %s %v", result, err)
	}
}

func TestDockerPythonStdioMCP(t *testing.T) {
	if os.Getenv("HARNESS_TEST_DOCKER_MCP") != "1" {
		t.Skip("设置 HARNESS_TEST_DOCKER_MCP=1 后验证 Python 容器")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	serviceID, versionID := uuid.NewString(), uuid.NewString()
	tag := "hadmin-mcp-" + strings.ReplaceAll(serviceID, "-", "") + ":" + strings.ReplaceAll(versionID, "-", "")
	runtime := mcpruntime.NewDocker("node:22-alpine", "python:3.11-slim", "none")
	image, log, err := runtime.Build(ctx, serviceID, versionID, "python", "echo.py", testArchive(t, "echo.py", pythonEchoServer))
	if err != nil {
		t.Fatalf("Python 构建失败: %v\n%s", err, log)
	}
	t.Cleanup(func() {
		command := exec.Command("docker", "image", "rm", tag)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("清理 Python 测试镜像失败: %v %s", err, output)
		}
	})
	service := mcpadmin.ServiceRecord{ID: serviceID, SourceType: "upload", Transport: "stdio", RuntimeType: "python",
		EntryFile: "echo.py", ImageRef: image, ContainerName: "hadmin-mcp-" + serviceID, DeploymentStatus: "running"}
	t.Cleanup(func() {
		if err := runtime.Stop(context.Background(), service); err != nil {
			t.Errorf("清理 Python 测试容器失败: %v", err)
		}
	})
	if err := runtime.Start(ctx, service); err != nil {
		t.Fatal(err)
	}
	check := exec.CommandContext(ctx, "docker", "exec", service.ContainerName, "python", "-c", "print('python-ok')")
	if output, err := check.CombinedOutput(); err != nil || !strings.Contains(string(output), "python-ok") {
		t.Fatalf("Python 容器执行失败: %v %s", err, output)
	}
	manualCtx, manualCancel := context.WithTimeout(ctx, 5*time.Second)
	defer manualCancel()
	manual := exec.CommandContext(manualCtx, "docker", "exec", "-i", service.ContainerName, "python", "/app/echo.py")
	manual.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n")
	if output, err := manual.CombinedOutput(); err != nil || !strings.Contains(string(output), "python-echo-test") {
		t.Fatalf("Python 脚本未响应初始化: %v %s", err, output)
	}
	streamCtx, streamCancel := context.WithTimeout(ctx, 5*time.Second)
	defer streamCancel()
	stream := exec.CommandContext(streamCtx, "docker", "exec", "-i", service.ContainerName, "python", "-u", "/app/echo.py")
	stdin, err := stream.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := stream.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	response := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); response <- line }()
	select {
	case line := <-response:
		if !strings.Contains(line, "python-echo-test") {
			t.Fatalf("Python 流式响应错误: %s", line)
		}
	case <-streamCtx.Done():
		t.Fatal("Python 在 stdin 保持打开时未输出响应")
	}
	stdin.Close()
	stream.Wait()
	client := mcpclient.New(nil)
	probeCtx, probeCancel := context.WithTimeout(ctx, 12*time.Second)
	defer probeCancel()
	tools, err := client.ListTools(probeCtx, service, mcpadmin.UpstreamCredentials{})
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("Python 工具发现失败: %+v %v", tools, err)
	}
}

func TestDockerBuildGeneratesNodeEntry(t *testing.T) {
	if os.Getenv("HARNESS_TEST_DOCKER_MCP") != "1" {
		t.Skip("设置 HARNESS_TEST_DOCKER_MCP=1 后验证 Node.js 编译产物")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	serviceID, versionID := uuid.NewString(), uuid.NewString()
	tag := "hadmin-mcp-" + strings.ReplaceAll(serviceID, "-", "") + ":" + strings.ReplaceAll(versionID, "-", "")
	archive := testArchiveFiles(t, map[string]string{
		"package.json": `{"name":"generated-entry-test","version":"1.0.0","scripts":{"build":"mkdir -p build && cp src/index.js build/index.js"}}`,
		"src/index.js": `console.log('generated-entry-ok')`,
	})
	runtime := mcpruntime.NewDocker("node:22-alpine", "python:3.12-slim", "none")
	image, log, err := runtime.Build(ctx, serviceID, versionID, "node", "build/index.js", archive)
	if err != nil {
		t.Fatalf("Node 编译构建失败: %v\n%s", err, log)
	}
	t.Cleanup(func() {
		command := exec.Command("docker", "image", "rm", tag)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("清理测试镜像失败: %v %s", err, output)
		}
	})
	service := mcpadmin.ServiceRecord{ID: serviceID, SourceType: "upload", Transport: "stdio", RuntimeType: "node",
		EntryFile: "build/index.js", ImageRef: image, ContainerName: "hadmin-mcp-" + serviceID, DeploymentStatus: "running"}
	t.Cleanup(func() {
		if err := runtime.Stop(context.Background(), service); err != nil {
			t.Errorf("清理测试容器失败: %v", err)
		}
	})
	if err := runtime.Start(ctx, service); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "docker", "exec", service.ContainerName, "node", "/app/build/index.js")
	if output, err := command.CombinedOutput(); err != nil || !strings.Contains(string(output), "generated-entry-ok") {
		t.Fatalf("编译后入口未运行: %v %s", err, output)
	}
}

func testArchive(t *testing.T, name, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testArchiveFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
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
