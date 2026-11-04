package modelgateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const gatewayKeyPrefix = "harness_sk_"

var gatewayModelCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)
var usageMonthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

type Service struct {
	repository Repository
	policy     PolicyRepository
	limiter    RequestLimiter
	now        func() time.Time
	newSecret  func() (string, error)
}

func NewService(repository Repository, limiter RequestLimiter, policy PolicyRepository) *Service {
	return &Service{repository: repository, limiter: limiter, policy: policy, now: time.Now, newSecret: generateSecret}
}

func (s *Service) ListAPIKeys(ctx context.Context, organizationID string) ([]APIKey, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListAPIKeys(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询模型网关调用密钥失败: %w", err)
	}
	return items, nil
}

func (s *Service) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (CreatedAPIKey, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.AllowedModelCodes = normalizeModelCodes(input.AllowedModelCodes)
	now := s.now().UTC()
	if !validAPIKeySettings(input.OrganizationID, input.ActorID, input.Name, input.ExpiresAt, input.RequestsPerMinute, input.MonthlyTokenLimit, input.AllowedModelCodes, now) {
		return CreatedAPIKey{}, ErrInvalidInput
	}
	secret, err := s.newSecret()
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("生成模型网关调用密钥失败: %w", err)
	}
	digest := sha256.Sum256([]byte(secret))
	input.KeyPrefix = visiblePrefix(secret)
	input.SecretHash = digest[:]
	input.Secret = secret
	created, err := s.repository.CreateAPIKey(ctx, input)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("创建模型网关调用密钥失败: %w", err)
	}
	return CreatedAPIKey{APIKey: created, Secret: secret}, nil
}

func (s *Service) RevealAPIKeySecret(ctx context.Context, input RevealAPIKeySecretInput) (string, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.APIKeyID == "" {
		return "", ErrInvalidInput
	}
	value, err := s.repository.RevealAPIKeySecret(ctx, input)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) || errors.Is(err, ErrAPIKeySecretUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("查看模型网关调用密钥失败: %w", err)
	}
	return value, nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, input UpdateAPIKeyInput) (APIKey, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.AllowedModelCodes = normalizeModelCodes(input.AllowedModelCodes)
	if input.APIKeyID == "" || !validAPIKeySettings(input.OrganizationID, input.ActorID, input.Name, input.ExpiresAt, input.RequestsPerMinute, input.MonthlyTokenLimit, input.AllowedModelCodes, s.now().UTC()) {
		return APIKey{}, ErrInvalidInput
	}
	item, err := s.repository.UpdateAPIKey(ctx, input)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return APIKey{}, err
		}
		return APIKey{}, fmt.Errorf("修改模型网关调用密钥失败: %w", err)
	}
	return item, nil
}

func (s *Service) RevokeAPIKey(ctx context.Context, input RevokeAPIKeyInput) (APIKey, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.APIKeyID == "" {
		return APIKey{}, ErrInvalidInput
	}
	item, err := s.repository.RevokeAPIKey(ctx, input)
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return APIKey{}, err
		}
		return APIKey{}, fmt.Errorf("吊销模型网关调用密钥失败: %w", err)
	}
	return item, nil
}

func (s *Service) Prepare(ctx context.Context, credential string, body []byte) (PreparedRequest, error) {
	return s.PrepareOperation(ctx, credential, body, "chat")
}

func (s *Service) PrepareOperation(ctx context.Context, credential string, body []byte, operation string) (PreparedRequest, error) {
	if operation != "chat" && operation != "image" && operation != "video" {
		return PreparedRequest{}, ErrInvalidInput
	}
	credential = strings.TrimSpace(credential)
	if !strings.HasPrefix(credential, gatewayKeyPrefix) || len(credential) < len(gatewayKeyPrefix)+20 {
		return PreparedRequest{}, ErrInvalidCredential
	}
	digest := sha256.Sum256([]byte(credential))
	apiKey, err := s.repository.FindAPIKeyByHash(ctx, digest[:])
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return PreparedRequest{}, ErrInvalidCredential
		}
		return PreparedRequest{}, fmt.Errorf("校验模型网关调用密钥失败: %w", err)
	}
	if apiKey.Status != "active" {
		return PreparedRequest{}, ErrAPIKeyRevoked
	}
	if apiKey.ExpiresAt != nil && !apiKey.ExpiresAt.After(s.now()) {
		return PreparedRequest{}, ErrAPIKeyExpired
	}

	if operation == "chat" && apiKey.MonthlyTokenLimit > 0 && apiKey.MonthlyTokenUsed >= apiKey.MonthlyTokenLimit {
		return PreparedRequest{}, ErrMonthlyQuota
	}
	if apiKey.RequestsPerMinute > 0 {
		if s.limiter == nil {
			return PreparedRequest{}, errors.New("模型网关限流器未配置")
		}
		allowed, err := s.limiter.Allow(ctx, apiKey.ID, apiKey.RequestsPerMinute)
		if err != nil {
			return PreparedRequest{}, fmt.Errorf("检查模型网关调用频率失败: %w", err)
		}
		if !allowed {
			return PreparedRequest{}, ErrRateLimited
		}
	}
	prepared, err := s.prepareForCaller(ctx, Caller{OrganizationID: apiKey.OrganizationID, APIKeyID: apiKey.ID, AuthType: "api_key"}, body, apiKey.AllowedModelCodes, operation)
	if err != nil {
		return PreparedRequest{}, err
	}
	if err := s.repository.TouchAPIKey(ctx, apiKey.ID); err != nil {
		return PreparedRequest{}, fmt.Errorf("更新模型网关调用密钥使用时间失败: %w", err)
	}
	return prepared, nil
}

func (s *Service) PrepareForUser(ctx context.Context, organizationID, userID string, body []byte) (PreparedRequest, error) {
	return s.PrepareForUserOperation(ctx, organizationID, userID, body, "chat")
}

func (s *Service) PrepareForUserOperation(ctx context.Context, organizationID, userID string, body []byte, operation string) (PreparedRequest, error) {
	if operation != "chat" && operation != "image" && operation != "video" {
		return PreparedRequest{}, ErrInvalidInput
	}
	if organizationID == "" || userID == "" {
		return PreparedRequest{}, ErrInvalidCredential
	}
	if s.policy == nil {
		return PreparedRequest{}, errors.New("模型策略仓储未配置")
	}
	caller := Caller{OrganizationID: organizationID, UserID: userID, AuthType: "user"}
	snapshot, err := s.policy.FindUserPolicy(ctx, organizationID, userID)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("查询用户模型策略失败: %w", err)
	}
	caller.DepartmentID = snapshot.DepartmentID
	allowed := effectiveUserModels(snapshot)
	prepared, err := s.prepareForCaller(ctx, caller, body, allowed, operation)
	if err != nil {
		return PreparedRequest{}, err
	}
	policies := []*SubjectPolicy{snapshot.UserPolicy, snapshot.DepartmentPolicy}
	for _, policy := range policies {
		if operation == "chat" && policy != nil && policy.MonthlyTokenLimit > 0 && policy.MonthlyTokenUsed >= policy.MonthlyTokenLimit {
			if policy.SubjectType == "department" {
				if policy.MonthlyTokenMode == "per_user" {
					return PreparedRequest{}, fmt.Errorf("部门策略的个人额度已用尽: %w", ErrMonthlyQuota)
				}
				return PreparedRequest{}, fmt.Errorf("部门共享额度已用尽: %w", ErrMonthlyQuota)
			}
			return PreparedRequest{}, fmt.Errorf("个人额度已用尽: %w", ErrMonthlyQuota)
		}
	}
	for _, policy := range policies {
		if policy == nil || policy.RequestsPerMinute == 0 {
			continue
		}
		if s.limiter == nil {
			return PreparedRequest{}, errors.New("模型网关限流器未配置")
		}
		key := "policy:" + organizationID + ":" + policy.SubjectType + ":" + policy.SubjectID
		if policy.SubjectType == "department" {
			key += ":user:" + userID
		}
		ok, err := s.limiter.Allow(ctx, key, policy.RequestsPerMinute)
		if err != nil {
			return PreparedRequest{}, fmt.Errorf("检查用户模型调用频率失败: %w", err)
		}
		if !ok {
			if policy.SubjectType == "department" {
				return PreparedRequest{}, fmt.Errorf("部门策略的个人每分钟请求次数已达上限: %w", ErrRateLimited)
			}
			return PreparedRequest{}, fmt.Errorf("个人每分钟请求次数已达上限: %w", ErrRateLimited)
		}
	}
	return prepared, nil
}

func (s *Service) prepareForCaller(ctx context.Context, caller Caller, body []byte, allowedModelCodes []string, operation string) (PreparedRequest, error) {
	var payload map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil || payload == nil {
		return PreparedRequest{}, ErrInvalidInput
	}
	var modelCode string
	if raw, ok := payload["model"]; !ok || json.Unmarshal(raw, &modelCode) != nil || strings.TrimSpace(modelCode) == "" {
		return PreparedRequest{}, ErrInvalidInput
	}
	modelCode = strings.ToLower(strings.TrimSpace(modelCode))
	if (caller.AuthType == "api_key" && !modelAllowed(allowedModelCodes, modelCode)) ||
		(caller.AuthType == "user" && !policyModelAllowed(allowedModelCodes, modelCode)) {
		return PreparedRequest{}, ErrModelNotAllowed
	}
	var streaming bool
	if raw, ok := payload["stream"]; ok {
		if operation != "chat" || json.Unmarshal(raw, &streaming) != nil {
			return PreparedRequest{}, ErrInvalidInput
		}
	}
	if operation == "image" {
		var prompt string
		if json.Unmarshal(payload["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
			return PreparedRequest{}, ErrInvalidInput
		}
	}
	if operation == "video" {
		var input map[string]json.RawMessage
		if json.Unmarshal(payload["input"], &input) != nil || len(input) == 0 {
			return PreparedRequest{}, ErrInvalidInput
		}
	}
	route, err := s.repository.FindRoute(ctx, caller.OrganizationID, modelCode)
	if err != nil {
		if errors.Is(err, ErrModelNotFound) {
			return PreparedRequest{}, err
		}
		return PreparedRequest{}, fmt.Errorf("解析模型网关路由失败: %w", err)
	}
	if route.ModelStatus != "active" {
		return PreparedRequest{}, ErrModelDisabled
	}
	if route.ProviderStatus != "active" {
		return PreparedRequest{}, ErrProviderDisabled
	}
	if route.ModelType != "" && route.ModelType != operation {
		return PreparedRequest{}, ErrModelTypeMismatch
	}
	if operation == "video" && route.ProviderKind != "aliyun_bailian" {
		return PreparedRequest{}, ErrModelTypeMismatch
	}
	encodedModel, _ := json.Marshal(route.UpstreamModelID)
	payload["model"] = encodedModel
	if streaming && route.ProviderStreamUsageEnabled {
		streamOptions := make(map[string]json.RawMessage)
		if raw, ok := payload["stream_options"]; ok {
			if err := json.Unmarshal(raw, &streamOptions); err != nil || streamOptions == nil {
				return PreparedRequest{}, ErrInvalidInput
			}
		}
		streamOptions["include_usage"] = json.RawMessage("true")
		encodedOptions, err := json.Marshal(streamOptions)
		if err != nil {
			return PreparedRequest{}, ErrInvalidInput
		}
		payload["stream_options"] = encodedOptions
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return PreparedRequest{}, ErrInvalidInput
	}
	return PreparedRequest{Caller: caller, Route: route, Body: rewritten, Streaming: streaming}, nil
}

func (s *Service) SaveVideoTask(ctx context.Context, input SaveVideoTaskInput) (VideoTask, error) {
	if input.Caller.OrganizationID == "" || input.Route.ModelID == "" || input.Route.ProviderID == "" ||
		input.Route.ModelType != "video" || input.UpstreamTaskID == "" || len(input.UpstreamTaskID) > 200 || input.RequestID == "" ||
		(input.Caller.AuthType != "api_key" && input.Caller.AuthType != "user") ||
		(input.Caller.AuthType == "api_key" && (input.Caller.APIKeyID == "" || input.Caller.UserID != "")) ||
		(input.Caller.AuthType == "user" && (input.Caller.UserID == "" || input.Caller.APIKeyID != "")) {
		return VideoTask{}, ErrInvalidInput
	}
	task, err := s.repository.SaveVideoTask(ctx, input)
	if err != nil {
		return VideoTask{}, fmt.Errorf("保存视频任务失败: %w", err)
	}
	return task, nil
}

func (s *Service) FindVideoTaskForAPIKey(ctx context.Context, credential, taskID string) (VideoTask, error) {
	if _, err := uuid.Parse(taskID); err != nil {
		return VideoTask{}, ErrInvalidInput
	}
	credential = strings.TrimSpace(credential)
	if !strings.HasPrefix(credential, gatewayKeyPrefix) || len(credential) < len(gatewayKeyPrefix)+20 {
		return VideoTask{}, ErrInvalidCredential
	}
	digest := sha256.Sum256([]byte(credential))
	key, err := s.repository.FindAPIKeyByHash(ctx, digest[:])
	if errors.Is(err, ErrAPIKeyNotFound) {
		return VideoTask{}, ErrInvalidCredential
	}
	if err != nil {
		return VideoTask{}, fmt.Errorf("校验模型网关调用密钥失败: %w", err)
	}
	if key.Status != "active" {
		return VideoTask{}, ErrAPIKeyRevoked
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(s.now()) {
		return VideoTask{}, ErrAPIKeyExpired
	}
	return s.findVideoTask(ctx, Caller{OrganizationID: key.OrganizationID, APIKeyID: key.ID, AuthType: "api_key"}, taskID)
}

func (s *Service) FindVideoTaskForUser(ctx context.Context, organizationID, userID, taskID string) (VideoTask, error) {
	if organizationID == "" || userID == "" || s.policy == nil {
		return VideoTask{}, ErrInvalidCredential
	}
	if _, err := uuid.Parse(taskID); err != nil {
		return VideoTask{}, ErrInvalidInput
	}
	if _, err := s.policy.FindUserPolicy(ctx, organizationID, userID); err != nil {
		return VideoTask{}, fmt.Errorf("校验用户身份失败: %w", err)
	}
	return s.findVideoTask(ctx, Caller{OrganizationID: organizationID, UserID: userID, AuthType: "user"}, taskID)
}

func (s *Service) findVideoTask(ctx context.Context, caller Caller, taskID string) (VideoTask, error) {
	task, err := s.repository.FindVideoTask(ctx, caller, taskID)
	if err != nil {
		if errors.Is(err, ErrVideoTaskNotFound) {
			return VideoTask{}, err
		}
		return VideoTask{}, fmt.Errorf("查询视频任务失败: %w", err)
	}
	return task, nil
}

func (s *Service) RecordCall(ctx context.Context, input RecordCallInput) error {
	if input.ModelType == "" {
		input.ModelType = "chat"
	}
	if input.AuthType == "" && input.APIKeyID != "" {
		input.AuthType = "api_key"
	}
	validCaller := (input.AuthType == "api_key" && input.APIKeyID != "" && input.UserID == "") ||
		(input.AuthType == "user" && input.UserID != "" && input.APIKeyID == "")
	if input.OrganizationID == "" || !validCaller || input.ModelID == "" || input.ProviderID == "" || input.RequestID == "" || input.HTTPStatus < 100 || input.HTTPStatus > 599 || input.LatencyMS < 0 {
		return ErrInvalidInput
	}
	if (input.Result != "success" && input.Result != "failed") ||
		(input.ModelType != "chat" && input.ModelType != "image" && input.ModelType != "video") {
		return ErrInvalidInput
	}
	if input.Source == "" {
		input.Source = "model_gateway"
	}
	if input.Operation == "" {
		input.Operation = input.ModelType
	}
	if input.Source != "model_gateway" && input.Source != "ai_drama" && input.Source != "ai_novel" {
		return ErrInvalidInput
	}
	if input.PromptTokens < 0 || input.CompletionTokens < 0 || input.TotalTokens < 0 {
		return ErrInvalidInput
	}
	if input.PromptTokens > 0 || input.CompletionTokens > 0 || input.TotalTokens > 0 {
		input.UsageReported = true
	}
	if err := s.repository.RecordCall(ctx, input); err != nil {
		return fmt.Errorf("记录模型网关调用失败: %w", err)
	}
	return nil
}

func (s *Service) ListCalls(ctx context.Context, query CallQuery) (CallPage, error) {
	query.ModelCode = strings.ToLower(strings.TrimSpace(query.ModelCode))
	query.Result = strings.ToLower(strings.TrimSpace(query.Result))
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.OrganizationID == "" || query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 || (query.Result != "" && query.Result != "success" && query.Result != "failed") || (query.StartedAt != nil && query.EndedAt != nil && query.StartedAt.After(*query.EndedAt)) {
		return CallPage{}, ErrInvalidInput
	}
	items, err := s.repository.ListCalls(ctx, query)
	if err != nil {
		return CallPage{}, fmt.Errorf("查询模型网关调用记录失败: %w", err)
	}
	return items, nil
}

func (s *Service) ListUsage(ctx context.Context, query UsageQuery) (UsagePage, error) {
	if query.Month == "" {
		query.Month = s.now().UTC().Format("2006-01")
	}
	if query.GroupBy == "" {
		query.GroupBy = "department"
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.OrganizationID == "" || !usageMonthPattern.MatchString(query.Month) || query.Month[:4] == "0000" ||
		(query.GroupBy != "department" && query.GroupBy != "user" && query.GroupBy != "model") ||
		query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return UsagePage{}, ErrInvalidInput
	}
	items, err := s.repository.ListUsage(ctx, query)
	if err != nil {
		return UsagePage{}, fmt.Errorf("查询模型用量统计失败: %w", err)
	}
	return items, nil
}

func (s *Service) ListModelsForUser(ctx context.Context, organizationID, userID string) ([]AvailableModel, error) {
	if organizationID == "" || userID == "" || s.policy == nil {
		return nil, ErrInvalidCredential
	}
	snapshot, err := s.policy.FindUserPolicy(ctx, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("查询用户模型策略失败: %w", err)
	}
	return s.listAvailableModels(ctx, organizationID, effectiveUserModels(snapshot))
}

func (s *Service) ListModelsForAPIKey(ctx context.Context, credential string) ([]AvailableModel, error) {
	if !strings.HasPrefix(credential, gatewayKeyPrefix) || len(credential) < len(gatewayKeyPrefix)+20 || s.policy == nil {
		return nil, ErrInvalidCredential
	}
	digest := sha256.Sum256([]byte(credential))
	key, err := s.repository.FindAPIKeyByHash(ctx, digest[:])
	if errors.Is(err, ErrAPIKeyNotFound) {
		return nil, ErrInvalidCredential
	}
	if err != nil {
		return nil, fmt.Errorf("校验模型网关调用密钥失败: %w", err)
	}
	if key.Status != "active" {
		return nil, ErrAPIKeyRevoked
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(s.now()) {
		return nil, ErrAPIKeyExpired
	}
	allowed := key.AllowedModelCodes
	if len(allowed) == 0 {
		allowed = nil
	}
	return s.listAvailableModels(ctx, key.OrganizationID, allowed)
}

func (s *Service) listAvailableModels(ctx context.Context, organizationID string, allowed []string) ([]AvailableModel, error) {
	items, err := s.policy.ListAvailableModels(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询可用模型目录失败: %w", err)
	}
	result := make([]AvailableModel, 0, len(items))
	for _, item := range items {
		if policyModelAllowed(allowed, strings.ToLower(item.Code)) {
			result = append(result, item)
		}
	}
	return result, nil
}

func normalizeModelCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func validAPIKeySettings(organizationID, actorID, name string, expiresAt *time.Time, requestsPerMinute int, monthlyTokenLimit int64, allowedModelCodes []string, now time.Time) bool {
	if organizationID == "" || actorID == "" || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 || requestsPerMinute < 0 || requestsPerMinute > 100000 || monthlyTokenLimit < 0 || len(allowedModelCodes) > 100 || (expiresAt != nil && !expiresAt.After(now)) {
		return false
	}
	for _, code := range allowedModelCodes {
		if !gatewayModelCodePattern.MatchString(code) {
			return false
		}
	}
	return true
}

func modelAllowed(allowed []string, modelCode string) bool {
	if len(allowed) == 0 {
		return true
	}
	index := sort.SearchStrings(allowed, modelCode)
	return index < len(allowed) && allowed[index] == modelCode
}

func generateSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return gatewayKeyPrefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func visiblePrefix(secret string) string {
	const visibleLength = 18
	if len(secret) <= visibleLength {
		return secret
	}
	return secret[:visibleLength]
}
