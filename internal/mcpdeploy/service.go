package mcpdeploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"harness-admin/internal/mcpadmin"
)

var (
	ErrInvalidInput      = errors.New("MCP 部署参数不正确")
	ErrNotFound          = errors.New("MCP 部署版本不存在")
	ErrNotReady          = errors.New("MCP 服务尚未构建成功")
	ErrDisabled          = errors.New("MCP 源码部署未启用")
	ErrBusy              = errors.New("MCP 服务正在执行其他部署操作")
	ErrSource            = errors.New("MCP 源码读取失败")
	ErrDependencyNetwork = errors.New("构建网络未开启，代码依赖无法下载；请由运维将 MCP_BUILD_NETWORK 设为 default 后重试")
)

var revisionPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var entryFilePattern = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

type Input struct {
	mcpadmin.Actor
	ServiceID   string
	Name        string
	Description string
	SourceType  string
	SourceRef   string
	Revision    string
	RuntimeType string
	EntryFile   string
	Archive     []byte
}

type Version struct {
	ID           string     `json:"id"`
	ServiceID    string     `json:"serviceId"`
	SourceType   string     `json:"sourceType"`
	SourceRef    string     `json:"sourceRef"`
	Revision     string     `json:"revision"`
	RuntimeType  string     `json:"runtimeType"`
	EntryFile    string     `json:"entryFile"`
	ImageRef     string     `json:"-"`
	BuildStatus  string     `json:"buildStatus"`
	BuildLog     string     `json:"buildLog"`
	ErrorMessage string     `json:"errorMessage"`
	CreatedAt    time.Time  `json:"createdAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
}

type Result struct {
	Service mcpadmin.ServiceRecord `json:"service"`
	Version Version                `json:"version"`
}

type SourceInspection struct {
	Revision string `json:"revision"`
	NodePlan
}

type RuntimeStatus struct {
	Running         bool   `json:"running"`
	Status          string `json:"status"`
	Logs            string `json:"logs"`
	ProtocolHealthy bool   `json:"protocolHealthy"`
	ProtocolError   string `json:"protocolError,omitempty"`
}

type Repository interface {
	AcquireLock(context.Context, string) (func() error, error)
	Create(context.Context, Input) (Result, error)
	AddVersion(context.Context, Input) (Result, error)
	UpdateBuildSource(context.Context, mcpadmin.Actor, string, string, string, string, string) error
	CompleteBuild(context.Context, mcpadmin.Actor, string, string, string, string, string) (Result, error)
	Get(context.Context, string, string) (mcpadmin.ServiceRecord, error)
	ListVersions(context.Context, string, string) ([]Version, error)
	SetRuntimeStatus(context.Context, mcpadmin.Actor, string, string, string) (mcpadmin.ServiceRecord, error)
	ActivateVersion(context.Context, mcpadmin.Actor, string, string) (mcpadmin.ServiceRecord, error)
	UpdateMetadata(context.Context, mcpadmin.Actor, string, string, string) (mcpadmin.ServiceRecord, error)
}

type Source interface {
	Resolve(context.Context, string, string, string) (string, error)
	Fetch(context.Context, string, string, string) ([]byte, error)
}

type Runtime interface {
	Build(context.Context, string, string, string, string, []byte) (string, string, error)
	Start(context.Context, mcpadmin.ServiceRecord) error
	Stop(context.Context, mcpadmin.ServiceRecord) error
	Inspect(context.Context, mcpadmin.ServiceRecord) (RuntimeStatus, error)
}

type Probe interface {
	ListTools(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error)
}

type Service struct {
	repository Repository
	source     Source
	runtime    Runtime
	probe      Probe
	enabled    bool
	buildSlots chan struct{}
}

func NewService(repository Repository, source Source, runtime Runtime, probe Probe, enabled bool) *Service {
	return &Service{repository: repository, source: source, runtime: runtime, probe: probe, enabled: enabled, buildSlots: make(chan struct{}, 2)}
}

func (s *Service) Enabled() bool { return s.enabled }

// InspectRepository 仅读取公开仓库的固定版本与 package.json，不创建服务或执行源码。
func (s *Service) InspectRepository(ctx context.Context, repositoryURL, ref string) (SourceInspection, error) {
	if !s.enabled {
		return SourceInspection{}, ErrDisabled
	}
	provider, repository, err := ParseRepositoryURL(repositoryURL)
	if err != nil || !validRef(strings.TrimSpace(ref)) {
		return SourceInspection{}, ErrInvalidInput
	}
	inspectCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	sha, err := s.source.Resolve(inspectCtx, provider, repository, strings.TrimSpace(ref))
	if err != nil {
		return SourceInspection{}, fmt.Errorf("%w: 解析仓库版本失败: %v", ErrSource, err)
	}
	if !revisionPattern.MatchString(sha) {
		return SourceInspection{}, fmt.Errorf("%w: 平台未返回有效的 Git 提交号", ErrSource)
	}
	archive, err := s.source.Fetch(inspectCtx, provider, repository, sha)
	if err != nil {
		return SourceInspection{}, fmt.Errorf("%w: 下载固定版本失败: %v", ErrSource, err)
	}
	if _, err := readmeFromArchive(archive); err != nil {
		return SourceInspection{}, fmt.Errorf("%w: %v", ErrSource, err)
	}
	plan, err := nodePlanFromArchive(archive)
	if err != nil {
		return SourceInspection{}, fmt.Errorf("%w: %v", ErrSource, err)
	}
	return SourceInspection{Revision: strings.ToLower(sha), NodePlan: plan}, nil
}

func (s *Service) Import(ctx context.Context, input Input) (Result, error) {
	return s.build(ctx, input, false)
}

func (s *Service) NewVersion(ctx context.Context, input Input) (Result, error) {
	if !s.enabled {
		return Result{}, ErrDisabled
	}
	if _, err := uuid.Parse(input.ServiceID); err != nil {
		return Result{}, ErrInvalidInput
	}
	release, err := s.repository.AcquireLock(ctx, input.ServiceID)
	if err != nil {
		return Result{}, err
	}
	result, operationErr := s.build(ctx, input, true)
	if releaseErr := release(); releaseErr != nil {
		operationErr = errors.Join(operationErr, fmt.Errorf("释放 MCP 部署锁失败: %w", releaseErr))
	}
	return result, operationErr
}

func (s *Service) build(ctx context.Context, input Input, existing bool) (Result, error) {
	if !s.enabled {
		return Result{}, ErrDisabled
	}
	input.Name = strings.TrimSpace(input.Name)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.Revision = strings.TrimSpace(input.Revision)
	input.EntryFile = strings.TrimSpace(input.EntryFile)
	if input.OrganizationID == "" || input.UserID == "" || (!existing && (input.Name == "" || len([]rune(input.Name)) > 120)) ||
		(existing && input.ServiceID == "") ||
		len(input.SourceRef) > 240 || !utf8.ValidString(input.SourceRef) ||
		(input.RuntimeType != "node" && input.RuntimeType != "python") ||
		(input.EntryFile != "" && !validEntry(input.EntryFile, input.RuntimeType)) ||
		(input.RuntimeType == "python" && input.EntryFile == "") {
		return Result{}, ErrInvalidInput
	}
	var repository string
	switch input.SourceType {
	case "github", "gitee":
		provider, name, err := ParseRepositoryURL(input.SourceRef)
		if err != nil || provider != input.SourceType || len(input.Archive) != 0 || !validRef(input.Revision) {
			return Result{}, ErrInvalidInput
		}
		repository = name
	case "upload":
		if len(input.Archive) == 0 || len(input.Archive) > 20*1024*1024 || input.SourceRef == "" ||
			path.Base(input.SourceRef) != input.SourceRef || strings.ContainsAny(input.SourceRef, `\/`) ||
			!strings.HasSuffix(input.SourceRef, ".tar.gz") {
			return Result{}, ErrInvalidInput
		}
		hash := sha256.Sum256(input.Archive)
		input.Revision = hex.EncodeToString(hash[:])
	default:
		return Result{}, ErrInvalidInput
	}
	// 先落库再返回；下载和构建均不占用 HTTP 请求生命周期。
	select {
	case s.buildSlots <- struct{}{}:
	default:
		return Result{}, ErrBusy
	}
	var result Result
	var err error
	if existing {
		result, err = s.repository.AddVersion(ctx, input)
	} else {
		result, err = s.repository.Create(ctx, input)
	}
	if err != nil {
		<-s.buildSlots
		return Result{}, err
	}
	go s.runBuild(input, repository, result)
	return result, nil
}

func (s *Service) runBuild(input Input, repository string, result Result) {
	defer func() { <-s.buildSlots }()
	buildCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	archive := input.Archive
	var err error
	stage := "读取源码"
	if input.SourceType != "upload" {
		stage = "解析仓库版本"
		var sha string
		sha, err = s.source.Resolve(buildCtx, input.SourceType, repository, input.Revision)
		if err == nil && !revisionPattern.MatchString(sha) {
			err = fmt.Errorf("平台未返回有效的 Git 提交号")
		}
		if err == nil {
			input.Revision = strings.ToLower(sha)
			stage = "下载仓库代码"
			archive, err = s.source.Fetch(buildCtx, input.SourceType, repository, input.Revision)
		}
	}
	if err == nil {
		stage = "读取 README.md"
		input.Description, err = readmeFromArchive(archive)
	}
	if err == nil && input.RuntimeType == "node" && input.EntryFile == "" {
		stage = "识别 Node.js 运行入口"
		input.EntryFile, err = nodeEntryFromArchive(archive)
	}
	if err == nil {
		stage = "保存源码信息"
		err = s.repository.UpdateBuildSource(buildCtx, input.Actor, result.Service.ID, result.Version.ID,
			input.Revision, input.EntryFile, input.Description)
	}
	var imageRef, buildLog string
	if err == nil {
		stage = "构建容器镜像"
		imageRef, buildLog, err = s.runtime.Build(buildCtx, result.Service.ID, result.Version.ID, input.RuntimeType, input.EntryFile, archive)
	}
	message := ""
	if err != nil {
		log.Printf("MCP 后台构建失败 service=%s version=%s stage=%s: %v", result.Service.ID, result.Version.ID, stage, err)
		message = stage + "失败，请检查构建日志或服务器日志"
		if stage == "识别 Node.js 运行入口" {
			message = "未识别到 Node.js 运行入口，请手动指定入口文件后重新构建"
		} else if errors.Is(err, ErrDependencyNetwork) {
			message = ErrDependencyNetwork.Error()
		} else if errors.Is(err, context.DeadlineExceeded) {
			message = "构建超过 10 分钟，请检查依赖下载网络和 build 脚本"
		}
		if buildLog == "" {
			buildLog = message
		}
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()
	_, writeErr := s.repository.CompleteBuild(writeCtx, input.Actor, result.Service.ID, result.Version.ID, imageRef, buildLog, message)
	if writeErr != nil {
		// 启动恢复会把残留 building 记录标为失败。
		log.Printf("保存 MCP 构建结果失败 service=%s version=%s: %v", result.Service.ID, result.Version.ID, writeErr)
	}
}

func (s *Service) Start(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.ServiceRecord, error) {
	if !s.enabled {
		return mcpadmin.ServiceRecord{}, ErrDisabled
	}
	return s.withLock(ctx, id, func() (mcpadmin.ServiceRecord, error) { return s.startUnlocked(ctx, actor, id) })
}

func (s *Service) startUnlocked(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.ServiceRecord, error) {
	item, err := s.repository.Get(ctx, actor.OrganizationID, id)
	if err != nil {
		return item, err
	}
	if item.SourceType == "remote" || item.ImageRef == "" || item.DeploymentStatus == "building" {
		return item, ErrNotReady
	}
	if err := s.runtime.Start(ctx, item); err != nil {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if _, recordErr := s.repository.SetRuntimeStatus(writeCtx, actor, id, "failed", safeError(err)); recordErr != nil {
			return item, errors.Join(err, fmt.Errorf("记录 MCP 启动失败状态: %w", recordErr))
		}
		return item, err
	}
	return s.repository.SetRuntimeStatus(ctx, actor, id, "running", "")
}

func (s *Service) Stop(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.ServiceRecord, error) {
	if !s.enabled {
		return mcpadmin.ServiceRecord{}, ErrDisabled
	}
	return s.withLock(ctx, id, func() (mcpadmin.ServiceRecord, error) { return s.stopUnlocked(ctx, actor, id) })
}

func (s *Service) stopUnlocked(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.ServiceRecord, error) {
	item, err := s.repository.Get(ctx, actor.OrganizationID, id)
	if err != nil {
		return item, err
	}
	if item.SourceType == "remote" {
		return item, ErrInvalidInput
	}
	if item.DeploymentStatus == "building" {
		return item, ErrBusy
	}
	if err := s.runtime.Stop(ctx, item); err != nil {
		return item, err
	}
	return s.repository.SetRuntimeStatus(ctx, actor, id, "stopped", "")
}

func (s *Service) Restart(ctx context.Context, actor mcpadmin.Actor, id string) (mcpadmin.ServiceRecord, error) {
	if !s.enabled {
		return mcpadmin.ServiceRecord{}, ErrDisabled
	}
	return s.withLock(ctx, id, func() (mcpadmin.ServiceRecord, error) {
		if _, err := s.stopUnlocked(ctx, actor, id); err != nil {
			return mcpadmin.ServiceRecord{}, err
		}
		return s.startUnlocked(ctx, actor, id)
	})
}

func (s *Service) Rollback(ctx context.Context, actor mcpadmin.Actor, id, versionID string) (mcpadmin.ServiceRecord, error) {
	if !s.enabled {
		return mcpadmin.ServiceRecord{}, ErrDisabled
	}
	if _, err := uuid.Parse(versionID); err != nil {
		return mcpadmin.ServiceRecord{}, ErrInvalidInput
	}
	return s.withLock(ctx, id, func() (mcpadmin.ServiceRecord, error) {
		versions, err := s.repository.ListVersions(ctx, actor.OrganizationID, id)
		if err != nil {
			return mcpadmin.ServiceRecord{}, err
		}
		found := false
		for _, version := range versions {
			if version.ID == versionID && version.BuildStatus == "succeeded" {
				found = true
				break
			}
		}
		if !found {
			return mcpadmin.ServiceRecord{}, ErrNotFound
		}
		if _, err := s.stopUnlocked(ctx, actor, id); err != nil {
			return mcpadmin.ServiceRecord{}, err
		}
		item, err := s.repository.ActivateVersion(ctx, actor, id, versionID)
		if err != nil {
			return item, err
		}
		return s.startUnlocked(ctx, actor, id)
	})
}

func (s *Service) withLock(ctx context.Context, id string, operation func() (mcpadmin.ServiceRecord, error)) (mcpadmin.ServiceRecord, error) {
	if _, err := uuid.Parse(id); err != nil {
		return mcpadmin.ServiceRecord{}, ErrInvalidInput
	}
	release, err := s.repository.AcquireLock(ctx, id)
	if err != nil {
		return mcpadmin.ServiceRecord{}, err
	}
	item, operationErr := operation()
	if releaseErr := release(); releaseErr != nil {
		operationErr = errors.Join(operationErr, fmt.Errorf("释放 MCP 部署锁失败: %w", releaseErr))
	}
	return item, operationErr
}

func (s *Service) Inspect(ctx context.Context, organizationID, id string) (RuntimeStatus, error) {
	if !s.enabled {
		return RuntimeStatus{}, ErrDisabled
	}
	item, err := s.repository.Get(ctx, organizationID, id)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if item.SourceType == "remote" {
		return RuntimeStatus{}, ErrInvalidInput
	}
	if item.ImageRef == "" {
		return RuntimeStatus{Status: item.DeploymentStatus}, nil
	}
	status, err := s.runtime.Inspect(ctx, item)
	if err != nil || !status.Running {
		return status, err
	}
	if s.probe == nil {
		return status, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := s.probe.ListTools(probeCtx, item, mcpadmin.UpstreamCredentials{}); err != nil {
		status.ProtocolError = "MCP 协议握手或工具发现失败"
		return status, nil
	}
	status.ProtocolHealthy = true
	return status, nil
}

func (s *Service) ListVersions(ctx context.Context, organizationID, id string) ([]Version, error) {
	if !s.enabled {
		return nil, ErrDisabled
	}
	return s.repository.ListVersions(ctx, organizationID, id)
}

func (s *Service) UpdateMetadata(ctx context.Context, actor mcpadmin.Actor, id, name, _ string) (mcpadmin.ServiceRecord, error) {
	if !s.enabled {
		return mcpadmin.ServiceRecord{}, ErrDisabled
	}
	name = strings.TrimSpace(name)
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" || name == "" ||
		len([]rune(name)) > 120 {
		return mcpadmin.ServiceRecord{}, ErrInvalidInput
	}
	return s.repository.UpdateMetadata(ctx, actor, id, name, "")
}

func validEntry(value, runtime string) bool {
	if value == "" || len(value) > 240 || strings.HasPrefix(value, "/") || !entryFilePattern.MatchString(value) ||
		strings.HasPrefix(value, "-") || path.Clean(value) != value || strings.HasPrefix(value, "../") || value == ".." {
		return false
	}
	if runtime == "node" {
		return strings.HasSuffix(value, ".js") || strings.HasSuffix(value, ".mjs") || strings.HasSuffix(value, ".cjs")
	}
	return strings.HasSuffix(value, ".py")
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	// Docker/下载错误可能包含本地路径或 URL；API 只暴露固定、无敏感值的分类。
	return "构建或运行失败，请查看管理员日志与构建输出"
}
