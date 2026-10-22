package mcpadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("MCP 服务参数不正确")
	ErrNotFound     = errors.New("MCP 服务不存在")
	ErrConflict     = errors.New("MCP 服务编码已存在")
	ErrDisabled     = errors.New("MCP 服务已停用")
	ErrUpstream     = errors.New("上游 MCP 服务异常")
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)
var headerNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
var queryNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

type UpstreamCredentials struct {
	Authorization string           `json:"authorization,omitempty"`
	HeaderName    string           `json:"headerName,omitempty"`
	HeaderValue   string           `json:"headerValue,omitempty"`
	QueryName     string           `json:"queryName,omitempty"`
	QueryValue    string           `json:"queryValue,omitempty"`
	Parameters    []ParameterValue `json:"parameters"`
}

type ParameterDefinition struct {
	Location    string `json:"location"`
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

type ParameterValue struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	Value    string `json:"value"`
}

// MergeCredentials 以连接专用值覆盖服务默认值；只返回一份无重复键的有效参数。
func MergeCredentials(defaults, overrides UpstreamCredentials) UpstreamCredentials {
	items := make([]ParameterValue, 0, len(defaults.AllParameters())+len(overrides.AllParameters()))
	positions := make(map[string]int)
	for _, item := range append(defaults.AllParameters(), overrides.AllParameters()...) {
		key := parameterKey(item.Location, item.Name)
		if index, exists := positions[key]; exists {
			items[index] = item
			continue
		}
		positions[key] = len(items)
		items = append(items, item)
	}
	return UpstreamCredentials{Parameters: items}
}

func (value UpstreamCredentials) AllParameters() []ParameterValue {
	items := make([]ParameterValue, 0, len(value.Parameters)+3)
	items = append(items, value.Parameters...)
	if value.Authorization != "" {
		items = append(items, ParameterValue{Location: "header", Name: "Authorization", Value: value.Authorization})
	}
	if value.HeaderName != "" {
		items = append(items, ParameterValue{Location: "header", Name: value.HeaderName, Value: value.HeaderValue})
	}
	if value.QueryName != "" {
		items = append(items, ParameterValue{Location: "query", Name: value.QueryName, Value: value.QueryValue})
	}
	return items
}

func ValidParameterDefinitions(items []ParameterDefinition, upstreamURL string) bool {
	if len(items) > 32 {
		return false
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if !validParameterName(item.Location, item.Name) || len([]rune(item.Label)) > 120 || len([]rune(item.Description)) > 500 {
			return false
		}
		key := parameterKey(item.Location, item.Name)
		if seen[key] {
			return false
		}
		seen[key] = true
		if item.Location == "path" && !strings.Contains(upstreamURL, "{"+item.Name+"}") {
			return false
		}
	}
	for _, found := range pathPlaceholderPattern.FindAllStringSubmatch(upstreamURL, -1) {
		if !seen[parameterKey("path", found[1])] {
			return false
		}
	}
	if strings.ContainsAny(pathPlaceholderPattern.ReplaceAllString(upstreamURL, ""), "{}") {
		return false
	}
	return true
}

var pathPlaceholderPattern = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_.-]{0,63})\}`)

func parameterKey(location, name string) string {
	if location == "header" {
		name = strings.ToLower(name)
	}
	return location + ":" + name
}

func validParameterName(location, name string) bool {
	switch location {
	case "header":
		if !headerNamePattern.MatchString(name) || strings.HasPrefix(strings.ToLower(name), "mcp-") {
			return false
		}
		switch strings.ToLower(name) {
		case "proxy-authorization", "host", "content-length", "transfer-encoding", "connection", "cookie", "accept", "content-type", "upgrade", "trailer", "te", "forwarded", "x-forwarded-host", "x-forwarded-proto", "x-original-url":
			return false
		}
		return true
	case "query", "path":
		return queryNamePattern.MatchString(name)
	default:
		return false
	}
}

func ValidUpstreamCredentials(value UpstreamCredentials) bool {
	if (value.HeaderName == "") != (value.HeaderValue == "") ||
		(value.QueryName == "") != (value.QueryValue == "") {
		return false
	}
	items := value.AllParameters()
	if len(items) > 32 {
		return false
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if !validParameterName(item.Location, item.Name) || item.Value == "" || len(item.Value) > 4096 ||
			strings.ContainsAny(item.Value, "\r\n") {
			return false
		}
		key := parameterKey(item.Location, item.Name)
		if seen[key] {
			return false
		}
		seen[key] = true
		if item.Location == "path" && (item.Value == "." || item.Value == "..") {
			return false
		}
	}
	return true
}

func HasRequiredParameters(definitions []ParameterDefinition, value UpstreamCredentials) bool {
	provided := make(map[string]bool)
	for _, item := range value.AllParameters() {
		provided[parameterKey(item.Location, item.Name)] = true
	}
	for _, item := range definitions {
		if item.Required && !provided[parameterKey(item.Location, item.Name)] {
			return false
		}
	}
	return true
}

type ServiceRecord struct {
	ID                    string                `json:"id"`
	Code                  string                `json:"code"`
	Name                  string                `json:"name"`
	Description           string                `json:"description"`
	Transport             string                `json:"transport"`
	UpstreamURL           string                `json:"upstreamUrl"`
	Parameters            []ParameterDefinition `json:"parameters"`
	CredentialsConfigured bool                  `json:"credentialsConfigured"`
	Status                string                `json:"status"`
	SourceType            string                `json:"sourceType"`
	RuntimeType           string                `json:"runtimeType,omitempty"`
	EntryFile             string                `json:"entryFile,omitempty"`
	ImageRef              string                `json:"-"`
	ContainerName         string                `json:"-"`
	DeploymentStatus      string                `json:"deploymentStatus,omitempty"`
	DeploymentError       string                `json:"deploymentError,omitempty"`
	CurrentRevision       string                `json:"currentRevision,omitempty"`
	CreatedAt             time.Time             `json:"createdAt"`
	UpdatedAt             time.Time             `json:"updatedAt"`
}

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	DiscoveredAt time.Time       `json:"discoveredAt"`
}

type Actor struct {
	OrganizationID string
	UserID         string
	RequestID      string
	SourceIP       string
}

type CreateInput struct {
	Actor
	Code        string
	Name        string
	Description string
	Transport   string
	UpstreamURL string
	Parameters  []ParameterDefinition
	Credentials UpstreamCredentials
}

type UpdateInput struct {
	Actor
	ServiceID   string
	Name        string
	Description string
	Transport   string
	UpstreamURL string
	Parameters  []ParameterDefinition
}

type UpdateCredentialsInput struct {
	Actor
	ServiceID   string
	Credentials UpstreamCredentials
	Definitions *[]ParameterDefinition
}

type ChangeStatusInput struct {
	Actor
	ServiceID string
	Status    string
}

type DiscoverInput struct {
	Actor
	ServiceID    string
	ConnectionID string
	Credentials  UpstreamCredentials
}

type TestInput struct {
	Actor
	ServiceID    string
	ConnectionID string
	Name         string
	Arguments    json.RawMessage
	Credentials  UpstreamCredentials
}

type Repository interface {
	List(context.Context, string) ([]ServiceRecord, error)
	Create(context.Context, CreateInput) (ServiceRecord, error)
	Update(context.Context, UpdateInput) (ServiceRecord, error)
	Get(context.Context, string, string) (ServiceRecord, error)
	GetCredentials(context.Context, string, string) (UpstreamCredentials, error)
	GetConnectionCredentials(context.Context, string, string, string) (UpstreamCredentials, error)
	RevealCredentials(context.Context, Actor, string) (UpstreamCredentials, error)
	UpdateCredentials(context.Context, UpdateCredentialsInput) (ServiceRecord, error)
	ChangeStatus(context.Context, ChangeStatusInput) (ServiceRecord, error)
	ReplaceTools(context.Context, DiscoverInput, []Tool) error
	ListTools(context.Context, string, string) ([]Tool, error)
	RecordTest(context.Context, TestInput, bool) error
}

type UpstreamClient interface {
	ListTools(context.Context, ServiceRecord, UpstreamCredentials) ([]Tool, error)
	CallTool(context.Context, ServiceRecord, UpstreamCredentials, string, json.RawMessage) (json.RawMessage, error)
}

type Service struct {
	repository Repository
	upstream   UpstreamClient
}

func NewService(repository Repository, upstream UpstreamClient) *Service {
	return &Service{repository: repository, upstream: upstream}
}

func (s *Service) List(ctx context.Context, organizationID string) ([]ServiceRecord, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	return s.repository.List(ctx, organizationID)
}

func (s *Service) Create(ctx context.Context, input CreateInput) (ServiceRecord, error) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.UpstreamURL = strings.TrimSpace(input.UpstreamURL)
	if input.Code == "" {
		input.Code = "mcp_s_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if input.OrganizationID == "" || input.UserID == "" || !codePattern.MatchString(input.Code) ||
		len([]rune(input.Name)) == 0 || len([]rune(input.Name)) > 120 ||
		len([]rune(input.Description)) > 10000 || !ValidUpstreamCredentials(input.Credentials) ||
		(input.Transport != "streamable_http" && input.Transport != "sse") || !validURL(input.UpstreamURL) ||
		!ValidParameterDefinitions(input.Parameters, input.UpstreamURL) {
		return ServiceRecord{}, ErrInvalidInput
	}
	return s.repository.Create(ctx, input)
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (ServiceRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.UpstreamURL = strings.TrimSpace(input.UpstreamURL)
	if input.OrganizationID == "" || input.UserID == "" || input.ServiceID == "" ||
		len([]rune(input.Name)) == 0 || len([]rune(input.Name)) > 120 ||
		len([]rune(input.Description)) > 10000 ||
		(input.Transport != "streamable_http" && input.Transport != "sse") || !validURL(input.UpstreamURL) ||
		!ValidParameterDefinitions(input.Parameters, input.UpstreamURL) {
		return ServiceRecord{}, ErrInvalidInput
	}
	return s.repository.Update(ctx, input)
}

func (s *Service) RevealCredentials(ctx context.Context, actor Actor, serviceID string) (UpstreamCredentials, error) {
	if actor.OrganizationID == "" || actor.UserID == "" || serviceID == "" {
		return UpstreamCredentials{}, ErrInvalidInput
	}
	return s.repository.RevealCredentials(ctx, actor, serviceID)
}

func (s *Service) UpdateCredentials(ctx context.Context, input UpdateCredentialsInput) (ServiceRecord, error) {
	if input.OrganizationID == "" || input.UserID == "" || input.ServiceID == "" ||
		!ValidUpstreamCredentials(input.Credentials) {
		return ServiceRecord{}, ErrInvalidInput
	}
	service, err := s.repository.Get(ctx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return ServiceRecord{}, err
	}
	definitions := service.Parameters
	if input.Definitions != nil {
		definitions = *input.Definitions
		if !ValidParameterDefinitions(definitions, service.UpstreamURL) {
			return ServiceRecord{}, ErrInvalidInput
		}
	}
	if !HasRequiredParameters(definitions, input.Credentials) {
		return ServiceRecord{}, ErrInvalidInput
	}
	return s.repository.UpdateCredentials(ctx, input)
}

func (s *Service) ChangeStatus(ctx context.Context, input ChangeStatusInput) (ServiceRecord, error) {
	if input.OrganizationID == "" || input.UserID == "" || input.ServiceID == "" ||
		(input.Status != "active" && input.Status != "disabled") {
		return ServiceRecord{}, ErrInvalidInput
	}
	return s.repository.ChangeStatus(ctx, input)
}

func (s *Service) Discover(ctx context.Context, input DiscoverInput) ([]Tool, error) {
	service, err := s.activeService(ctx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return nil, err
	}
	if input.UserID == "" || !ValidUpstreamCredentials(input.Credentials) {
		return nil, ErrInvalidInput
	}
	defaults, err := s.credentialsForCall(ctx, input.OrganizationID, input.ServiceID, input.ConnectionID)
	if err != nil {
		return nil, err
	}
	credentials := MergeCredentials(defaults, input.Credentials)
	if !HasRequiredParameters(service.Parameters, credentials) {
		return nil, ErrInvalidInput
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tools, err := s.upstream.ListTools(probeCtx, service, credentials)
	if err != nil {
		return nil, fmt.Errorf("%w: 发现 MCP 工具失败: %v", ErrUpstream, err)
	}
	if len(tools) > 500 {
		return nil, fmt.Errorf("%w: 上游工具数量超过 500 个", ErrUpstream)
	}
	if err := s.repository.ReplaceTools(ctx, input, tools); err != nil {
		return nil, fmt.Errorf("保存 MCP 工具快照失败: %w", err)
	}
	return tools, nil
}

func (s *Service) ListTools(ctx context.Context, organizationID, serviceID string) ([]Tool, error) {
	if _, err := s.repository.Get(ctx, organizationID, serviceID); err != nil {
		return nil, err
	}
	return s.repository.ListTools(ctx, organizationID, serviceID)
}

func (s *Service) Test(ctx context.Context, input TestInput) (json.RawMessage, error) {
	service, err := s.activeService(ctx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return nil, err
	}
	if input.UserID == "" || input.Name == "" || len(input.Name) > 256 ||
		len(input.Arguments) > 64*1024 || !ValidUpstreamCredentials(input.Credentials) ||
		!json.Valid(input.Arguments) || input.Arguments[0] != '{' {
		return nil, ErrInvalidInput
	}
	defaults, err := s.credentialsForCall(ctx, input.OrganizationID, input.ServiceID, input.ConnectionID)
	if err != nil {
		return nil, err
	}
	credentials := MergeCredentials(defaults, input.Credentials)
	if !HasRequiredParameters(service.Parameters, credentials) {
		return nil, ErrInvalidInput
	}
	knownTools, err := s.repository.ListTools(ctx, input.OrganizationID, input.ServiceID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, tool := range knownTools {
		if tool.Name == input.Name {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrInvalidInput
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, callErr := s.upstream.CallTool(callCtx, service, credentials, input.Name, input.Arguments)
	if err := s.repository.RecordTest(ctx, input, callErr == nil); err != nil {
		return nil, fmt.Errorf("记录 MCP 测试审计失败: %w", err)
	}
	if callErr != nil {
		return nil, fmt.Errorf("%w: MCP 工具测试失败: %v", ErrUpstream, callErr)
	}
	return result, nil
}

func (s *Service) activeService(ctx context.Context, organizationID, serviceID string) (ServiceRecord, error) {
	if organizationID == "" || serviceID == "" {
		return ServiceRecord{}, ErrInvalidInput
	}
	service, err := s.repository.Get(ctx, organizationID, serviceID)
	if err != nil {
		return ServiceRecord{}, err
	}
	if service.Status != "active" {
		return ServiceRecord{}, ErrDisabled
	}
	return service, nil
}

func (s *Service) credentialsForCall(ctx context.Context, organizationID, serviceID, connectionID string) (UpstreamCredentials, error) {
	if connectionID != "" {
		if _, err := uuid.Parse(connectionID); err != nil {
			return UpstreamCredentials{}, ErrInvalidInput
		}
		return s.repository.GetConnectionCredentials(ctx, organizationID, serviceID, connectionID)
	}
	return s.repository.GetCredentials(ctx, organizationID, serviceID)
}

func validURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawQuery != "" || len(raw) > 2048 {
		return false
	}
	return true
}
