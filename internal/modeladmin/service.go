package modeladmin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,79}$`)
var importedCodeCharacters = regexp.MustCompile(`[^a-z0-9._-]+`)

var allowedCapabilities = map[string]struct{}{
	"text": {}, "vision": {}, "tools": {}, "streaming": {}, "json": {},
}

var allowedProviderKinds = map[string]struct{}{
	"openai_compatible": {},
	"deepseek":          {},
	"aliyun_bailian":    {},
	"kimi":              {},
	"ollama":            {},
	"siliconflow":       {},
	"claude":            {},
	"gemini":            {},
}

var allowedModelTypes = map[string]struct{}{"chat": {}, "image": {}, "video": {}, "audio": {}, "embedding": {}, "rerank": {}, "unknown": {}}

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
	if !validProvider(input.OrganizationID, input.ActorID, input.Code, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS) || !validOptionalModelListURL(input.ModelListURL) || input.APIKey == "" || utf8.RuneCountInString(input.APIKey) > 4096 {
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
	if input.ModelListURL != nil {
		value := strings.TrimSpace(*input.ModelListURL)
		input.ModelListURL = &value
	}
	if input.APIKey != nil {
		value := strings.TrimSpace(*input.APIKey)
		input.APIKey = &value
	}
	if !validProviderSettings(input.OrganizationID, input.ActorID, input.Name, input.ProviderKind, input.BaseURL, input.RequestTimeoutMS) || input.ProviderID == "" || (input.ModelListURL != nil && !validOptionalModelListURL(*input.ModelListURL)) || (input.APIKey != nil && (*input.APIKey == "" || utf8.RuneCountInString(*input.APIKey) > 4096)) {
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

func (s *Service) DiscoverModels(ctx context.Context, input DiscoverModelsInput) ([]DiscoveredModel, error) {
	if input.OrganizationID == "" || input.ActorID == "" || input.ProviderID == "" || s.prober == nil {
		return nil, ErrInvalidInput
	}
	credential, err := s.repository.GetProviderCredential(ctx, input.OrganizationID, input.ProviderID)
	if err != nil {
		return nil, err
	}
	if credential.Provider.Status != "active" {
		return nil, ErrProviderDisabled
	}
	models, err := s.prober.Discover(ctx, credential)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDiscoveryFailed, err)
	}
	return models, nil
}

func (s *Service) ImportModels(ctx context.Context, input ImportModelsInput) (ImportModelsResult, error) {
	if len(input.UpstreamModelIDs) == 0 || len(input.UpstreamModelIDs) > 500 {
		return ImportModelsResult{}, ErrInvalidInput
	}
	selected := make(map[string]struct{}, len(input.UpstreamModelIDs))
	for _, id := range input.UpstreamModelIDs {
		if !validLength(id, 1, 200) {
			return ImportModelsResult{}, ErrInvalidInput
		}
		selected[id] = struct{}{}
	}
	models, err := s.DiscoverModels(ctx, DiscoverModelsInput{input.OrganizationID, input.ActorID, input.ProviderID})
	if err != nil {
		return ImportModelsResult{}, err
	}
	chosen := make([]DiscoveredModel, 0, len(selected))
	for _, model := range models {
		if _, ok := selected[model.UpstreamModelID]; !ok {
			continue
		}
		model.Name = strings.TrimSpace(model.Name)
		if model.Name == "" {
			model.Name = model.UpstreamModelID
		}
		if !validModelSettings(input.OrganizationID, input.ActorID, input.ProviderID, model.UpstreamModelID, model.Name, model.ModelType, model.Capabilities, model.ContextWindowTokens, model.MaxOutputTokens) {
			return ImportModelsResult{}, ErrInvalidInput
		}
		chosen = append(chosen, model)
		delete(selected, model.UpstreamModelID)
	}
	if len(selected) != 0 {
		return ImportModelsResult{}, ErrInvalidInput
	}
	return s.repository.ImportModels(ctx, input, chosen)
}

func ImportedModelCode(providerCode, upstreamID string) string {
	sum := sha256.Sum256([]byte(providerCode + "\x00" + upstreamID))
	stem := strings.ToLower(providerCode + "_" + upstreamID)
	stem = importedCodeCharacters.ReplaceAllString(stem, "_")
	stem = strings.TrimRight(stem, "._-")
	if len(stem) > 62 {
		stem = strings.TrimRight(stem[:62], "._-")
	}
	return stem + "_" + hex.EncodeToString(sum[:])[:12]
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

func (s *Service) BatchChangeModelStatus(ctx context.Context, input BatchChangeModelStatusInput) (BatchChangeModelStatusResult, error) {
	if input.OrganizationID == "" || input.ActorID == "" || !validStatus(input.Status) || len(input.ModelIDs) == 0 || len(input.ModelIDs) > 500 {
		return BatchChangeModelStatusResult{}, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(input.ModelIDs))
	for _, id := range input.ModelIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return BatchChangeModelStatusResult{}, ErrInvalidInput
		}
		normalized := parsed.String()
		if _, exists := seen[normalized]; exists {
			return BatchChangeModelStatusResult{}, ErrInvalidInput
		}
		seen[normalized] = struct{}{}
	}
	result, err := s.repository.BatchChangeModelStatus(ctx, input)
	if err != nil {
		if errors.Is(err, ErrModelNotFound) {
			return BatchChangeModelStatusResult{}, err
		}
		return BatchChangeModelStatusResult{}, fmt.Errorf("批量修改模型状态失败: %w", err)
	}
	return result, nil
}

func normalizeProviderCreate(input *CreateProviderInput) {
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.ProviderKind = strings.ToLower(strings.TrimSpace(input.ProviderKind))
	input.BaseURL = normalizeBaseURL(input.BaseURL)
	input.ModelListURL = strings.TrimSpace(input.ModelListURL)
	input.APIKey = strings.TrimSpace(input.APIKey)
}

func normalizeBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validOptionalModelListURL(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
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
	if organizationID == "" || actorID == "" || providerID == "" || !validLength(upstreamID, 1, 200) || !validLength(name, 1, 120) || contextWindow < 0 || maxOutput < 0 || (contextWindow > 0 && maxOutput > contextWindow) {
		return false
	}
	if _, ok := allowedModelTypes[modelType]; !ok {
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
