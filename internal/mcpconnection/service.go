package mcpconnection

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"harness-admin/internal/mcpadmin"
)

var (
	ErrInvalidInput      = errors.New("MCP 连接参数不正确")
	ErrNotFound          = errors.New("MCP 连接不存在")
	ErrConflict          = errors.New("MCP 连接编码已存在")
	ErrUnauthorized      = errors.New("MCP 连接凭据无效")
	ErrForbidden         = errors.New("当前员工无权使用此 MCP 连接")
	ErrInactive          = errors.New("MCP 连接已停用、过期或所属服务不可用")
	ErrRateLimited       = errors.New("MCP 连接请求过于频繁")
	ErrSecretUnavailable = errors.New("此连接没有可查看的入口密钥")
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)

type Connection struct {
	ID                       string     `json:"id"`
	ServiceID                string     `json:"serviceId"`
	ServiceName              string     `json:"serviceName"`
	Code                     string     `json:"code"`
	GatewayURL               string     `json:"gatewayUrl,omitempty"`
	Name                     string     `json:"name"`
	Purpose                  string     `json:"purpose"`
	AuthMode                 string     `json:"authMode"`
	ApplicationName          string     `json:"applicationName,omitempty"`
	Status                   string     `json:"status"`
	ExpiresAt                *time.Time `json:"expiresAt,omitempty"`
	KeyPrefix                string     `json:"keyPrefix,omitempty"`
	UpstreamAuthConfigured   bool       `json:"upstreamAuthConfigured"`
	UpstreamHeaderConfigured bool       `json:"upstreamHeaderConfigured"`
	UpstreamQueryConfigured  bool       `json:"upstreamQueryConfigured"`
	UpstreamConfigured       bool       `json:"upstreamConfigured"`
	RequestsPerMinute        int        `json:"requestsPerMinute"`
	DepartmentIDs            []string   `json:"departmentIds"`
	UserIDs                  []string   `json:"userIds"`
	CreatedAt                time.Time  `json:"createdAt"`
	UpdatedAt                time.Time  `json:"updatedAt"`
}

type CreatedConnection struct {
	Connection
	Secret string `json:"secret,omitempty"`
}

type Actor struct {
	OrganizationID string
	UserID         string
	RequestID      string
	SourceIP       string
}

type CreateInput struct {
	Actor
	ServiceID             string
	Code                  string
	Name                  string
	Purpose               string
	AuthMode              string
	ApplicationName       string
	ExpiresAt             *time.Time
	UpstreamAuthorization string
	UpstreamHeaderName    string
	UpstreamHeaderValue   string
	UpstreamQueryName     string
	UpstreamQueryValue    string
	UpstreamParameters    []mcpadmin.ParameterValue
	RequestsPerMinute     int
	DepartmentIDs         []string
	UserIDs               []string
	Secret                string
	SecretHash            []byte
	KeyPrefix             string
}

type GrantsInput struct {
	Actor
	ConnectionID  string
	DepartmentIDs []string
	UserIDs       []string
}

type GatewayConnection struct {
	Connection
	ServiceParameters     []mcpadmin.ParameterValue `json:"-"`
	OrganizationID        string
	ServiceStatus         string
	UpstreamURL           string
	Transport             string
	SourceType            string
	RuntimeType           string
	EntryFile             string
	ContainerName         string
	DeploymentStatus      string
	SecretHash            []byte
	UpstreamAuthorization string
	UpstreamHeaderName    string
	UpstreamHeaderValue   string
	UpstreamQueryName     string
	UpstreamQueryValue    string
	UpstreamParameters    []mcpadmin.ParameterValue
	Tools                 []mcpadmin.Tool
}

type Caller struct {
	UserID       string
	DepartmentID string
}

type CallRecord struct {
	ConnectionID   string
	OrganizationID string
	ServiceID      string
	ToolName       string
	UserID         string
	DepartmentID   string
	Result         string
	LatencyMS      int64
}

type Usage struct {
	ConnectionID   string `json:"connectionId"`
	ToolName       string `json:"toolName"`
	UserID         string `json:"userId,omitempty"`
	UserName       string `json:"userName,omitempty"`
	DepartmentID   string `json:"departmentId,omitempty"`
	DepartmentName string `json:"departmentName,omitempty"`
	Calls          int64  `json:"calls"`
	Failures       int64  `json:"failures"`
	AverageMS      int64  `json:"averageMs"`
}

// Health describes a single, on-demand upstream MCP protocol check. It does not execute a tool.
type Health struct {
	ConnectionID string    `json:"connectionId"`
	Status       string    `json:"status"`
	Message      string    `json:"message"`
	ToolCount    int       `json:"toolCount"`
	CheckedAt    time.Time `json:"checkedAt"`
}

type HealthProber interface {
	ListTools(context.Context, mcpadmin.ServiceRecord, mcpadmin.UpstreamCredentials) ([]mcpadmin.Tool, error)
}

type Repository interface {
	List(context.Context, string) ([]Connection, error)
	Create(context.Context, CreateInput) (Connection, error)
	ReplaceGrants(context.Context, GrantsInput) (Connection, error)
	GetByCode(context.Context, string) (GatewayConnection, error)
	GetByID(context.Context, string, string) (GatewayConnection, error)
	ChangeStatus(context.Context, Actor, string, string) (Connection, error)
	RevealSecret(context.Context, Actor, string) (string, error)
	RevealUpstreamAuthorization(context.Context, Actor, string) (string, error)
	RevealUpstreamCredentials(context.Context, Actor, string) (mcpadmin.UpstreamCredentials, error)
	RotateSecret(context.Context, Actor, string, string, []byte, string) error
	UpdateUpstreamCredentials(context.Context, Actor, string, mcpadmin.UpstreamCredentials) (Connection, error)
	AllowedUser(context.Context, string, string, string) (string, bool, error)
	ListForUser(context.Context, string, string) ([]Connection, error)
	RecordCall(context.Context, CallRecord) error
	ListUsage(context.Context, string) ([]Usage, error)
}

type Limiter interface {
	Allow(context.Context, string, int) (bool, error)
}

type Service struct {
	repository Repository
	limiter    Limiter
	now        func() time.Time
}

func NewService(repository Repository, limiter Limiter) *Service {
	return &Service{repository: repository, limiter: limiter, now: time.Now}
}

func (s *Service) List(ctx context.Context, organizationID string) ([]Connection, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	return s.repository.List(ctx, organizationID)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (CreatedConnection, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	if input.Code == "" {
		input.Code = "mcp_c_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	input.Name = strings.TrimSpace(input.Name)
	input.ApplicationName = strings.TrimSpace(input.ApplicationName)
	if input.OrganizationID == "" || input.UserID == "" || input.ServiceID == "" ||
		!codePattern.MatchString(input.Code) || len([]rune(input.Name)) == 0 || len([]rune(input.Name)) > 120 ||
		len([]rune(input.ApplicationName)) > 120 || !mcpadmin.ValidUpstreamCredentials(mcpadmin.UpstreamCredentials{
		Authorization: input.UpstreamAuthorization, HeaderName: input.UpstreamHeaderName,
		HeaderValue: input.UpstreamHeaderValue, QueryName: input.UpstreamQueryName, QueryValue: input.UpstreamQueryValue,
		Parameters: input.UpstreamParameters,
	}) ||
		(input.Purpose != "harness" && input.Purpose != "developer") {
		return CreatedConnection{}, ErrInvalidInput
	}
	if input.Purpose == "harness" {
		if input.AuthMode != "oidc" {
			return CreatedConnection{}, ErrInvalidInput
		}
	} else if input.AuthMode != "bearer" && input.AuthMode != "none" {
		return CreatedConnection{}, ErrInvalidInput
	}
	if input.Purpose == "developer" && len(input.DepartmentIDs)+len(input.UserIDs) > 0 {
		return CreatedConnection{}, ErrInvalidInput
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return CreatedConnection{}, ErrInvalidInput
	}
	if input.RequestsPerMinute == 0 {
		input.RequestsPerMinute = 60
	}
	if input.RequestsPerMinute < 1 || input.RequestsPerMinute > 100000 {
		return CreatedConnection{}, ErrInvalidInput
	}
	for _, id := range append(append([]string{}, input.DepartmentIDs...), input.UserIDs...) {
		if _, err := uuid.Parse(id); err != nil {
			return CreatedConnection{}, ErrInvalidInput
		}
	}
	if input.AuthMode == "bearer" {
		var err error
		input.Secret, input.SecretHash, input.KeyPrefix, err = newSecret()
		if err != nil {
			return CreatedConnection{}, err
		}
	}
	created, err := s.repository.Create(ctx, input)
	if err != nil {
		return CreatedConnection{}, err
	}
	return CreatedConnection{Connection: created, Secret: input.Secret}, nil
}

func newSecret() (string, []byte, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", nil, "", err
	}
	secret := "harness_mcp_" + base64.RawURLEncoding.EncodeToString(value)
	digest := sha256.Sum256([]byte(secret))
	return secret, digest[:], secret[:24], nil
}

func (s *Service) RotateSecret(ctx context.Context, actor Actor, id string) (string, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" {
		return "", ErrInvalidInput
	}
	value, digest, prefix, err := newSecret()
	if err != nil {
		return "", err
	}
	if err := s.repository.RotateSecret(ctx, actor, id, value, digest, prefix); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Service) UpdateUpstreamCredentials(ctx context.Context, actor Actor, id string, credentials mcpadmin.UpstreamCredentials) (Connection, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" || !mcpadmin.ValidUpstreamCredentials(credentials) {
		return Connection{}, ErrInvalidInput
	}
	return s.repository.UpdateUpstreamCredentials(ctx, actor, id, credentials)
}

func (s *Service) ReplaceGrants(ctx context.Context, input GrantsInput) (Connection, error) {
	if input.OrganizationID == "" || input.UserID == "" || input.ConnectionID == "" {
		return Connection{}, ErrInvalidInput
	}
	for _, id := range append(append([]string{}, input.DepartmentIDs...), input.UserIDs...) {
		if _, err := uuid.Parse(id); err != nil {
			return Connection{}, ErrInvalidInput
		}
	}
	return s.repository.ReplaceGrants(ctx, input)
}

func (s *Service) ChangeStatus(ctx context.Context, actor Actor, id, status string) (Connection, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" || (status != "active" && status != "disabled") {
		return Connection{}, ErrInvalidInput
	}
	return s.repository.ChangeStatus(ctx, actor, id, status)
}

func (s *Service) RevealSecret(ctx context.Context, actor Actor, id string) (string, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" {
		return "", ErrInvalidInput
	}
	return s.repository.RevealSecret(ctx, actor, id)
}

func (s *Service) RevealUpstreamAuthorization(ctx context.Context, actor Actor, id string) (string, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" {
		return "", ErrInvalidInput
	}
	return s.repository.RevealUpstreamAuthorization(ctx, actor, id)
}

func (s *Service) RevealUpstreamCredentials(ctx context.Context, actor Actor, id string) (mcpadmin.UpstreamCredentials, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || id == "" {
		return mcpadmin.UpstreamCredentials{}, ErrInvalidInput
	}
	return s.repository.RevealUpstreamCredentials(ctx, actor, id)
}

func (s *Service) Lookup(ctx context.Context, code string) (GatewayConnection, error) {
	if !codePattern.MatchString(code) {
		return GatewayConnection{}, ErrNotFound
	}
	item, err := s.repository.GetByCode(ctx, code)
	if err != nil {
		return GatewayConnection{}, err
	}
	if item.Status != "active" || item.ServiceStatus != "active" ||
		(item.Transport == "stdio" && item.DeploymentStatus != "running") ||
		(item.ExpiresAt != nil && !item.ExpiresAt.After(s.now())) {
		return GatewayConnection{}, ErrInactive
	}
	return item, nil
}

func (s *Service) CheckHealth(ctx context.Context, organizationID, id string, prober HealthProber) (Health, error) {
	if organizationID == "" || prober == nil {
		return Health{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(id); err != nil {
		return Health{}, ErrInvalidInput
	}
	item, err := s.repository.GetByID(ctx, organizationID, id)
	if err != nil {
		return Health{}, err
	}
	result := Health{ConnectionID: id, CheckedAt: s.now().UTC()}
	if item.Status != "active" {
		result.Status, result.Message = "inactive", "连接已停用"
		return result, nil
	}
	if item.ExpiresAt != nil && !item.ExpiresAt.After(s.now()) {
		result.Status, result.Message = "inactive", "连接已过期"
		return result, nil
	}
	if item.ServiceStatus != "active" {
		result.Status, result.Message = "inactive", "MCP 服务已停用"
		return result, nil
	}
	if item.Transport == "stdio" && item.DeploymentStatus != "running" {
		result.Status, result.Message = "unavailable", "托管服务未运行"
		return result, nil
	}
	service := mcpadmin.ServiceRecord{ID: item.ServiceID, UpstreamURL: item.UpstreamURL, Transport: item.Transport,
		SourceType: item.SourceType, RuntimeType: item.RuntimeType, EntryFile: item.EntryFile,
		ContainerName: item.ContainerName, DeploymentStatus: item.DeploymentStatus}
	credentials := mcpadmin.MergeCredentials(mcpadmin.UpstreamCredentials{Parameters: item.ServiceParameters},
		mcpadmin.UpstreamCredentials{Authorization: item.UpstreamAuthorization,
			HeaderName: item.UpstreamHeaderName, HeaderValue: item.UpstreamHeaderValue,
			QueryName: item.UpstreamQueryName, QueryValue: item.UpstreamQueryValue,
			Parameters: item.UpstreamParameters})
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tools, err := prober.ListTools(probeCtx, service, credentials)
	if err != nil {
		result.Status, result.Message = "unavailable", "上游握手或读取工具失败，请检查地址、参数和服务状态"
		return result, nil
	}
	result.ToolCount = len(tools)
	if len(tools) == 0 {
		result.Status, result.Message = "unavailable", "上游没有可用工具"
		return result, nil
	}
	if len(item.Tools) == 0 {
		result.Status, result.Message = "unavailable", "尚未发现工具，请刷新工具列表"
		return result, nil
	}
	actual := make(map[string]bool, len(tools))
	for _, tool := range tools {
		actual[tool.Name] = true
	}
	for _, tool := range item.Tools {
		if !actual[tool.Name] {
			result.Status, result.Message = "unavailable", "工具列表已变化，请重新发现工具"
			return result, nil
		}
	}
	result.Status, result.Message = "healthy", "MCP 握手和工具列表正常"
	return result, nil
}

func (s *Service) AuthorizeDeveloper(ctx context.Context, item GatewayConnection, credential string) (Caller, error) {
	if item.Purpose != "developer" {
		return Caller{}, ErrUnauthorized
	}
	switch item.AuthMode {
	case "none":
		// 匿名连接仍需按连接限流，不能作为用户身份。
	case "bearer":
		digest := sha256.Sum256([]byte(credential))
		if credential == "" || len(item.SecretHash) != len(digest) || subtle.ConstantTimeCompare(item.SecretHash, digest[:]) != 1 {
			return Caller{}, ErrUnauthorized
		}
	default:
		return Caller{}, ErrUnauthorized
	}
	return Caller{}, s.allow(ctx, item, "")
}

func (s *Service) AuthorizeUser(ctx context.Context, item GatewayConnection, organizationID, userID string) (Caller, error) {
	if item.Purpose != "harness" || item.AuthMode != "oidc" || item.OrganizationID != organizationID || userID == "" {
		return Caller{}, ErrUnauthorized
	}
	departmentID, allowed, err := s.repository.AllowedUser(ctx, item.ID, organizationID, userID)
	if err != nil {
		return Caller{}, err
	}
	if !allowed {
		return Caller{}, ErrForbidden
	}
	if err := s.allow(ctx, item, userID); err != nil {
		return Caller{}, err
	}
	return Caller{UserID: userID, DepartmentID: departmentID}, nil
}

func (s *Service) allow(ctx context.Context, item GatewayConnection, userID string) error {
	if s.limiter == nil {
		return errors.New("MCP 网关限流器未配置")
	}
	key := "mcp:" + item.ID
	if userID != "" {
		key += ":user:" + userID
	}
	allowed, err := s.limiter.Allow(ctx, key, item.RequestsPerMinute)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRateLimited
	}
	return nil
}

func (s *Service) ListForUser(ctx context.Context, organizationID, userID string) ([]Connection, error) {
	if organizationID == "" || userID == "" {
		return nil, ErrInvalidInput
	}
	return s.repository.ListForUser(ctx, organizationID, userID)
}

func (s *Service) RecordCall(ctx context.Context, record CallRecord) error {
	return s.repository.RecordCall(ctx, record)
}

func (s *Service) ListUsage(ctx context.Context, organizationID string) ([]Usage, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	return s.repository.ListUsage(ctx, organizationID)
}
