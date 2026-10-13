package modelgateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const gatewayKeyPrefix = "harness_sk_"

type Service struct {
	repository Repository
	now        func() time.Time
	newSecret  func() (string, error)
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now, newSecret: generateSecret}
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
	now := s.now().UTC()
	if input.OrganizationID == "" || input.ActorID == "" || utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 120 || (input.ExpiresAt != nil && !input.ExpiresAt.After(now)) {
		return CreatedAPIKey{}, ErrInvalidInput
	}
	secret, err := s.newSecret()
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("生成模型网关调用密钥失败: %w", err)
	}
	digest := sha256.Sum256([]byte(secret))
	input.KeyPrefix = visiblePrefix(secret)
	input.SecretHash = digest[:]
	created, err := s.repository.CreateAPIKey(ctx, input)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("创建模型网关调用密钥失败: %w", err)
	}
	return CreatedAPIKey{APIKey: created, Secret: secret}, nil
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

	var payload map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil || payload == nil {
		return PreparedRequest{}, ErrInvalidInput
	}
	var modelCode string
	if raw, ok := payload["model"]; !ok || json.Unmarshal(raw, &modelCode) != nil || strings.TrimSpace(modelCode) == "" {
		return PreparedRequest{}, ErrInvalidInput
	}
	modelCode = strings.ToLower(strings.TrimSpace(modelCode))
	var streaming bool
	if raw, ok := payload["stream"]; ok && json.Unmarshal(raw, &streaming) != nil {
		return PreparedRequest{}, ErrInvalidInput
	}
	route, err := s.repository.FindRoute(ctx, apiKey.OrganizationID, modelCode)
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
	encodedModel, _ := json.Marshal(route.UpstreamModelID)
	payload["model"] = encodedModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return PreparedRequest{}, ErrInvalidInput
	}
	if err := s.repository.TouchAPIKey(ctx, apiKey.ID); err != nil {
		return PreparedRequest{}, fmt.Errorf("更新模型网关调用密钥使用时间失败: %w", err)
	}
	return PreparedRequest{
		Caller: Caller{OrganizationID: apiKey.OrganizationID, APIKeyID: apiKey.ID},
		Route:  route, Body: rewritten, Streaming: streaming,
	}, nil
}

func (s *Service) RecordCall(ctx context.Context, input RecordCallInput) error {
	if input.OrganizationID == "" || input.APIKeyID == "" || input.ModelID == "" || input.ProviderID == "" || input.RequestID == "" || input.HTTPStatus < 100 || input.HTTPStatus > 599 || input.LatencyMS < 0 {
		return ErrInvalidInput
	}
	if input.Result != "success" && input.Result != "failed" {
		return ErrInvalidInput
	}
	if err := s.repository.RecordCall(ctx, input); err != nil {
		return fmt.Errorf("记录模型网关调用失败: %w", err)
	}
	return nil
}

func (s *Service) ListCalls(ctx context.Context, organizationID string, limit int) ([]Call, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 200 {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListCalls(ctx, organizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询模型网关调用记录失败: %w", err)
	}
	return items, nil
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
