package mcpdeploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"harness-admin/internal/mcpadmin"
)

type repositoryStub struct {
	created             Input
	completed           bool
	versions            []Version
	stopped             bool
	metadataDescription string
	done                chan struct{}
	buildResult         Result
}

func (*repositoryStub) AcquireLock(context.Context, string) (func() error, error) {
	return func() error { return nil }, nil
}

func (r *repositoryStub) Create(_ context.Context, input Input) (Result, error) {
	r.created = input
	r.done = make(chan struct{})
	return Result{Service: mcpadmin.ServiceRecord{ID: "service-1", SourceType: input.SourceType},
		Version: Version{ID: "version-1", ServiceID: "service-1", BuildStatus: "building"}}, nil
}
func (r *repositoryStub) AddVersion(ctx context.Context, input Input) (Result, error) {
	return r.Create(ctx, input)
}
func (r *repositoryStub) UpdateBuildSource(_ context.Context, _ mcpadmin.Actor, _, _, revision, entryFile, description string) error {
	r.created.Revision, r.created.EntryFile, r.created.Description = revision, entryFile, description
	return nil
}
func (r *repositoryStub) CompleteBuild(_ context.Context, _ mcpadmin.Actor, _, _, image, log, message string) (Result, error) {
	r.completed = true
	status := "succeeded"
	if message != "" {
		status = "failed"
	}
	r.buildResult = Result{Service: mcpadmin.ServiceRecord{ID: "service-1", ImageRef: image},
		Version: Version{ID: "version-1", BuildStatus: status, BuildLog: log, ErrorMessage: message}}
	close(r.done)
	return r.buildResult, nil
}

func waitBuild(t *testing.T, repository *repositoryStub) Result {
	t.Helper()
	select {
	case <-repository.done:
		return repository.buildResult
	case <-time.After(5 * time.Second):
		t.Fatal("后台构建未完成")
		return Result{}
	}
}
func (*repositoryStub) Get(context.Context, string, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{ID: "service-1", SourceType: "github", ImageRef: "sha256:image"}, nil
}
func (r *repositoryStub) ListVersions(context.Context, string, string) ([]Version, error) {
	return r.versions, nil
}
func (*repositoryStub) SetRuntimeStatus(context.Context, mcpadmin.Actor, string, string, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (*repositoryStub) ActivateVersion(context.Context, mcpadmin.Actor, string, string) (mcpadmin.ServiceRecord, error) {
	return mcpadmin.ServiceRecord{}, nil
}
func (r *repositoryStub) UpdateMetadata(_ context.Context, _ mcpadmin.Actor, _, _, description string) (mcpadmin.ServiceRecord, error) {
	r.metadataDescription = description
	return mcpadmin.ServiceRecord{}, nil
}

type sourceStub struct {
	archive []byte
	calls   int
}

func (*sourceStub) Resolve(_ context.Context, _, _, _ string) (string, error) {
	return strings.Repeat("a", 40), nil
}
func (s *sourceStub) Fetch(context.Context, string, string, string) ([]byte, error) {
	s.calls++
	return s.archive, nil
}

func testArchive(t *testing.T, readme string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gz)
	contents := []byte(readme)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "repo-abc/README.md", Mode: 0644, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type runtimeStub struct {
	archive   []byte
	buildErr  error
	stopCalls int
	running   bool
}

type blockingRuntime struct {
	runtimeStub
	release chan struct{}
}

func (r *blockingRuntime) Build(ctx context.Context, serviceID, versionID, runtimeType, entryFile string, archive []byte) (string, string, error) {
	select {
	case <-r.release:
		return r.runtimeStub.Build(ctx, serviceID, versionID, runtimeType, entryFile, archive)
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (r *runtimeStub) Build(_ context.Context, _, _, _, _ string, archive []byte) (string, string, error) {
	r.archive = archive
	if r.buildErr != nil {
		return "", "build failed", r.buildErr
	}
	return "sha256:image", "build succeeded", nil
}
func (*runtimeStub) Start(context.Context, mcpadmin.ServiceRecord) error  { return nil }
func (r *runtimeStub) Stop(context.Context, mcpadmin.ServiceRecord) error { r.stopCalls++; return nil }
func (r *runtimeStub) Inspect(context.Context, mcpadmin.ServiceRecord) (RuntimeStatus, error) {
	return RuntimeStatus{Running: r.running, Status: "running"}, nil
}

type probeStub struct {
	called bool
	err    error
}

func (p *probeStub) ListTools(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error) {
	p.called = true
	return []mcpadmin.Tool{}, p.err
}

func TestImportGitHubUsesFixedRevisionAndRecordsBuild(t *testing.T) {
	repository := &repositoryStub{}
	source := &sourceStub{archive: testArchive(t, "# 服务说明")}
	runtime := &runtimeStub{}
	service := NewService(repository, source, runtime, nil, true)
	input := Input{Actor: mcpadmin.Actor{OrganizationID: "org", UserID: "user"}, Name: "测试工具",
		SourceType: "github", SourceRef: "https://github.com/owner/repo", Revision: "main", RuntimeType: "node", EntryFile: "src/server.js"}
	result, err := service.Import(context.Background(), input)
	if err != nil || result.Version.BuildStatus != "building" {
		t.Fatalf("构建未异步提交: result=%+v err=%v", result, err)
	}
	final := waitBuild(t, repository)
	if final.Version.BuildStatus != "succeeded" || !repository.completed || source.calls != 1 ||
		!bytes.Equal(runtime.archive, source.archive) || repository.created.Revision != strings.Repeat("a", 40) || repository.created.Description != "# 服务说明" {
		t.Fatalf("导入结果不正确: result=%+v err=%v", final, err)
	}
}

func TestImportReturnsBeforeBackgroundBuildCompletes(t *testing.T) {
	repository := &repositoryStub{}
	runtime := &blockingRuntime{release: make(chan struct{})}
	service := NewService(repository, &sourceStub{archive: testArchive(t, "# MCP")}, runtime, nil, true)
	resultCh := make(chan Result, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := service.Import(context.Background(), Input{Actor: mcpadmin.Actor{OrganizationID: "org", UserID: "user"},
			Name: "测试", SourceType: "github", SourceRef: "https://github.com/owner/repo", RuntimeType: "node", EntryFile: "server.js"})
		resultCh <- result
		errCh <- err
	}()
	select {
	case result := <-resultCh:
		if err := <-errCh; err != nil || result.Version.BuildStatus != "building" {
			t.Fatalf("异步提交结果不正确: %+v %v", result, err)
		}
	case <-time.After(time.Second):
		t.Fatal("导入请求仍在等待后台构建")
	}
	close(runtime.release)
	if result := waitBuild(t, repository); result.Version.BuildStatus != "succeeded" {
		t.Fatalf("后台构建未完成: %+v", result)
	}
}

func TestImportGiteeAndUploadUseReadmeAsDescription(t *testing.T) {
	archive := testArchive(t, "# 来自 README")
	for _, input := range []Input{
		{SourceType: "gitee", SourceRef: "https://gitee.com/team/tool", Revision: "v1.0", RuntimeType: "node", EntryFile: "server.js"},
		{SourceType: "upload", SourceRef: "tool.tar.gz", RuntimeType: "node", EntryFile: "server.js", Archive: archive},
	} {
		input.Actor = mcpadmin.Actor{OrganizationID: "org", UserID: "user"}
		input.Name = "测试工具"
		repository := &repositoryStub{}
		service := NewService(repository, &sourceStub{archive: archive}, &runtimeStub{}, nil, true)
		result, err := service.Import(context.Background(), input)
		if err != nil || result.Version.BuildStatus != "building" {
			t.Fatalf("构建未异步提交: %+v %v", result, err)
		}
		final := waitBuild(t, repository)
		if final.Version.BuildStatus != "succeeded" || repository.created.Description != "# 来自 README" {
			t.Fatalf("描述未取自 README: %+v %v", repository.created, err)
		}
	}
}

func TestManagedMetadataDoesNotOverrideReadme(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &sourceStub{}, &runtimeStub{}, nil, true)
	_, err := service.UpdateMetadata(context.Background(), mcpadmin.Actor{OrganizationID: "org", UserID: "user"}, "service-id", "新名称", "手工覆盖")
	if err != nil || repository.metadataDescription != "" {
		t.Fatalf("托管服务描述不应由元信息接口覆盖: %q %v", repository.metadataDescription, err)
	}
}

func TestImportRejectsUnsafeSourceAndEntry(t *testing.T) {
	for _, input := range []Input{
		{SourceType: "github", SourceRef: "../../evil", Revision: strings.Repeat("a", 40), RuntimeType: "node", EntryFile: "server.js"},
		{SourceType: "github", SourceRef: "https://evil.example/owner/repo", Revision: "main", RuntimeType: "node", EntryFile: "server.js"},
		{SourceType: "github", SourceRef: "https://github.com/owner/repo", Revision: "main", RuntimeType: "node", EntryFile: "../server.js"},
	} {
		input.Actor = mcpadmin.Actor{OrganizationID: "org", UserID: "user"}
		input.Name = "test"
		service := NewService(&repositoryStub{}, &sourceStub{}, &runtimeStub{}, nil, true)
		if _, err := service.Import(context.Background(), input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("未拒绝不安全输入 %+v: %v", input, err)
		}
	}
}

func TestBuildFailureIsRecorded(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, &sourceStub{archive: testArchive(t, "# 测试")}, &runtimeStub{buildErr: errors.New("docker unavailable")}, nil, true)
	result, err := service.Import(context.Background(), Input{Actor: mcpadmin.Actor{OrganizationID: "org", UserID: "user"},
		Name: "test", SourceType: "github", SourceRef: "https://github.com/owner/repo", Revision: "v1.0.0",
		RuntimeType: "python", EntryFile: "server.py"})
	if err != nil || result.Version.BuildStatus != "building" {
		t.Fatalf("构建未异步提交: result=%+v err=%v", result, err)
	}
	final := waitBuild(t, repository)
	if final.Version.BuildStatus != "failed" || final.Version.ErrorMessage == "" || !repository.completed {
		t.Fatalf("失败构建未落库: result=%+v err=%v", final, err)
	}
}

func TestRollbackRejectsUnknownVersionBeforeStopping(t *testing.T) {
	runtime := &runtimeStub{}
	service := NewService(&repositoryStub{}, &sourceStub{}, runtime, nil, true)
	_, err := service.Rollback(context.Background(), mcpadmin.Actor{OrganizationID: "org", UserID: "user"},
		"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001")
	if !errors.Is(err, ErrNotFound) || runtime.stopCalls != 0 {
		t.Fatalf("回退验证失败: %v, stop=%d", err, runtime.stopCalls)
	}
}

func TestInspectChecksMCPProtocol(t *testing.T) {
	probe := &probeStub{}
	service := NewService(&repositoryStub{}, &sourceStub{}, &runtimeStub{running: true}, probe, true)
	status, err := service.Inspect(context.Background(), "org", "service-1")
	if err != nil || !status.ProtocolHealthy || !probe.called {
		t.Fatalf("未执行 MCP 健康检查: %+v %v", status, err)
	}
	probe.err = errors.New("handshake failed")
	status, err = service.Inspect(context.Background(), "org", "service-1")
	if err != nil || status.ProtocolHealthy || status.ProtocolError == "" {
		t.Fatalf("握手失败未体现: %+v %v", status, err)
	}
}
