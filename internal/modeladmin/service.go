package modeladmin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)

var allowedCapabilities = map[string]struct{}{
	"text": {}, "vision": {}, "tools": {}, "streaming": {}, "json": {},
}

var allowedProviderKinds = map[string]struct{}{
	"openai_compatible": {},
	"deepseek":          {},
	"aliyun_bailian":    {},
	"kimi":              {},
	"ollama":            {},
}

type Service struct {
	repository Repository
	prober     ProviderProber
}

func NewService(repository Repository, prober ProviderProber) *Service {
	return &Service{repository: repository, prober: prober}
}

func (s *Service) ListProviders(ctx context.Context, organizationID string) ([]Provider, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListProviders(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询模型供应商连接失败: %w", err)
	}
	return items, nil
}

func (s *Service) CreateProvider(ctx context.Context, input CreateProviderInput) (Provider, error) {
	normalizeProviderCreate(&input)
	if input.ProviderKind == "" {
		input.ProviderKind = "openai_compatible"
	}
	if input.RequestTimeoutMS == 0 {
		input.RequestTimeoutMS = 15000
	}
	if !validProvider(input.OrganizationID, input.ActorID, input.Code, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS) || input.APIKey == "" || utf8.RuneCountInString(input.APIKey) > 4096 {
		return Provider{}, ErrInvalidInput
	}
	provider, err := s.repository.CreateProvider(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderConflict) {
			return Provider{}, err
		}
		return Provider{}, fmt.Errorf("创建模型供应商连接失败: %w", err)
	}
	return provider, nil
}

func (s *Service) UpdateProvider(ctx context.Context, input UpdateProviderInput) (Provider, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ProviderKind = strings.ToLower(strings.TrimSpace(input.ProviderKind))
	input.BaseURL = normalizeBaseURL(input.BaseURL)
	if input.APIKey != nil {
		value := strings.TrimSpace(*input.APIKey)
		input.APIKey = &value
	}
	if !validProviderSettings(input.OrganizationID, input.ActorID, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS) || input.ProviderID == "" || (input.APIKey != nil && (*input.APIKey == "" || utf8.RuneCountInString(*input.APIKey) > 4096)) {
		return Provider{}, ErrInvalidInput
	}
	provider, err := s.repository.UpdateProvider(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) {
			return Provider{}, err
		}
		return Provider{}, fmt.Errorf("修改模型供应商连接失败: %w", err)
	}
	return provider, nil
}

func (s *Service) ChangeProviderStatus(ctx context.Context, input ChangeProviderStatusInput) (Provider, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ProviderID == "" || !validStatus(input.Status) {
		return Provider{}, ErrInvalidInput
	}
	provider, err := s.repository.ChangeProviderStatus(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) {
			return Provider{}, err
		}
		return Provider{}, fmt.Errorf("修改模型供应商连接状态失败: %w", err)
	}
	return provider, nil
}

func (s *Service) ProbeProvider(ctx context.Context, input ProbeInput) (ProbeResult, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ProviderID == "" || s.prober == nil {
		return ProbeResult{}, ErrInvalidInput
	}
	credential, err := s.repository.GetProviderCredential(ctx, input.OrganizationID, input.ProviderID)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) {
			return ProbeResult{}, err
		}
		return ProbeResult{}, fmt.Errorf("读取模型供应商连接失败: %w", err)
	}
	if credential.Provider.Status != "active" {
		return ProbeResult{}, ErrProviderDisabled
	}
	result, probeErr := s.prober.Probe(ctx, credential)
	detailCode := ""
	if probeErr != nil {
		result.Status = "failed"
		detailCode = "provider_unreachable"
	}
	if _, err := s.repository.RecordProviderProbe(ctx, input, result, detailCode); err != nil {
		return ProbeResult{}, fmt.Errorf("记录模型供应商连接检测结果失败: %w", err)
	}
	if probeErr != nil {
		return result, fmt.Errorf("%w: %v", ErrProbeFailed, probeErr)
	}
	return result, nil
}

func (s *Service) RevealProviderCredential(ctx context.Context, input RevealProviderCredentialInput) (string, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ProviderID == "" {
		return "", ErrInvalidInput
	}
	value, err := s.repository.RevealProviderCredential(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) {
			return "", err
		}
		return "", fmt.Errorf("查看模型供应商凭据失败: %w", err)
	}
	return value, nil
}

func (s *Service) ListModels(ctx context.Context, organizationID string) ([]CatalogModel, error) {
	if organizationID == "" {
		return nil, ErrInvalidInput
	}
	items, err := s.repository.ListModels(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("查询模型目录失败: %w", err)
	}
	return items, nil
}

func (s *Service) CreateModel(ctx context.Context, input CreateModelInput) (CatalogModel, error) {
	normalizeModelCreate(&input)
	if input.ModelType == "" {
		input.ModelType = "chat"
	}
	if !validModel(input.OrganizationID, input.ActorID, input.ProviderConnectionID, input.Code, input.UpstreamModelID, input.Name, input.ModelType, input.Capabilities, input.ContextWindowTokens, input.MaxOutputTokens) {
		return CatalogModel{}, ErrInvalidInput
	}
	model, err := s.repository.CreateModel(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) || errors.Is(err, ErrModelConflict) {
			return CatalogModel{}, err
		}
		return CatalogModel{}, fmt.Errorf("创建模型失败: %w", err)
	}
	return model, nil
}

func (s *Service) UpdateModel(ctx context.Context, input UpdateModelInput) (CatalogModel, error) {
	normalizeModelUpdate(&input)
	if input.ModelType == "" {
		input.ModelType = "chat"
	}
	if !validModelSettings(input.OrganizationID, input.ActorID, input.ProviderConnectionID, input.UpstreamModelID, input.Name, input.ModelType, input.Capabilities, input.ContextWindowTokens, input.MaxOutputTokens) || input.ModelID == "" {
		return CatalogModel{}, ErrInvalidInput
	}
	model, err := s.repository.UpdateModel(ctx, input)
	if err != nil {
		if errors.Is(err, ErrProviderNotFound) || errors.Is(err, ErrModelNotFound) || errors.Is(err, ErrModelConflict) {
			return CatalogModel{}, err
		}
		return CatalogModel{}, fmt.Errorf("修改模型失败: %w", err)
	}
	return model, nil
}

func (s *Service) ChangeModelStatus(ctx context.Context, input ChangeModelStatusInput) (CatalogModel, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ModelID == "" || !validStatus(input.Status) {
		return CatalogModel{}, ErrInvalidInput
	}
	model, err := s.repository.ChangeModelStatus(ctx, input)
	if err != nil {
		if errors.Is(err, ErrModelNotFound) {
			return CatalogModel{}, err
		}
		return CatalogModel{}, fmt.Errorf("修改模型状态失败: %w", err)
	}
	return model, nil
}

func normalizeProviderCreate(input *CreateProviderInput) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.ProviderKind = strings.ToLower(strings.TrimSpace(input.ProviderKind))
	input.BaseURL = normalizeBaseURL(input.BaseURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
}

func normalizeBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validProvider(organizationID, actorID, code, name, kind, baseURL string, timeoutMS int) bool {
	return codePattern.MatchString(code) && validProviderSettings(organizationID, actorID, name, kind, baseURL, timeoutMS)
}

func validProviderSettings(organizationID, actorID, name, kind, baseURL string, timeoutMS int) bool {
	if organizationID == "" || actorID == "" || !validLength(name, 1, 120) || timeoutMS < 1000 || timeoutMS > 120000 {
		return false
	}
	if _, ok := allowedProviderKinds[kind]; !ok {
		return false
	}
	parsed, err := url.ParseRequestURI(baseURL)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func normalizeModelCreate(input *CreateModelInput) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.UpstreamModelID = strings.TrimSpace(input.UpstreamModelID)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = input.UpstreamModelID
	}
	input.ModelType = strings.ToLower(strings.TrimSpace(input.ModelType))
	input.Capabilities = normalizeCapabilities(input.Capabilities)
}

func normalizeModelUpdate(input *UpdateModelInput) {
	input.UpstreamModelID = strings.TrimSpace(input.UpstreamModelID)
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = input.UpstreamModelID
	}
	input.ModelType = strings.ToLower(strings.TrimSpace(input.ModelType))
	input.Capabilities = normalizeCapabilities(input.Capabilities)
}

func normalizeCapabilities(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validModel(organizationID, actorID, providerID, code, upstreamID, name, modelType string, capabilities []string, contextWindow, maxOutput int) bool {
	return codePattern.MatchString(code) && validModelSettings(organizationID, actorID, providerID, upstreamID, name, modelType, capabilities, contextWindow, maxOutput)
}

func validModelSettings(organizationID, actorID, providerID, upstreamID, name, modelType string, capabilities []string, contextWindow, maxOutput int) bool {
	if organizationID == "" || actorID == "" || providerID == "" || !validLength(upstreamID, 1, 200) || !validLength(name, 1, 120) || modelType != "chat" || contextWindow < 0 || maxOutput < 0 || (contextWindow > 0 && maxOutput > contextWindow) {
		return false
	}
	for _, capability := range capabilities {
		if _, ok := allowedCapabilities[capability]; !ok {
			return false
		}
	}
	return true
}

func validStatus(value string) bool { return value == "active" || value == "disabled" }

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}
