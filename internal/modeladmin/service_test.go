package modeladmin

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repositoryStub struct {
	createdProvider CreateProviderInput
	updatedProvider UpdateProviderInput
	createdModel    CreateModelInput
	credential      ProviderCredential
	probeResult     ProbeResult
	err             error
	importInput     ImportModelsInput
	importModels    []DiscoveredModel
}

func (r *repositoryStub) ListProviders(context.Context, string) ([]Provider, error) {
	return nil, r.err
}
func (r *repositoryStub) CreateProvider(_ context.Context, input CreateProviderInput) (Provider, error) {
	r.createdProvider = input
	return Provider{ID: "provider-1", Code: input.Code, Name: input.Name}, r.err
}
func (r *repositoryStub) UpdateProvider(_ context.Context, input UpdateProviderInput) (Provider, error) {
	r.updatedProvider = input
	return Provider{}, r.err
}
func (r *repositoryStub) ChangeProviderStatus(context.Context, ChangeProviderStatusInput) (Provider, error) {
	return Provider{}, r.err
}
func (r *repositoryStub) GetProviderCredential(context.Context, string, string) (ProviderCredential, error) {
	return r.credential, r.err
}
func (r *repositoryStub) RevealProviderCredential(context.Context, RevealProviderCredentialInput) (string, error) {
	return r.credential.APIKey, r.err
}
func (r *repositoryStub) RecordProviderProbe(_ context.Context, _ ProbeInput, result ProbeResult, _ string) (Provider, error) {
	r.probeResult = result
	return Provider{}, r.err
}
func (r *repositoryStub) ListModels(context.Context, string) ([]CatalogModel, error) {
	return nil, r.err
}
func (r *repositoryStub) CreateModel(_ context.Context, input CreateModelInput) (CatalogModel, error) {
	r.createdModel = input
	return CatalogModel{ID: "model-1", Code: input.Code}, r.err
}
func (r *repositoryStub) UpdateModel(context.Context, UpdateModelInput) (CatalogModel, error) {
	return CatalogModel{}, r.err
}
func (r *repositoryStub) ChangeModelStatus(context.Context, ChangeModelStatusInput) (CatalogModel, error) {
	return CatalogModel{}, r.err
}
func (r *repositoryStub) ImportModels(_ context.Context, input ImportModelsInput, models []DiscoveredModel) (ImportModelsResult, error) {
	r.importInput, r.importModels = input, models
	return ImportModelsResult{Imported: len(models)}, r.err
}

type proberStub struct{ err error }

func (p proberStub) Probe(context.Context, ProviderCredential) (ProbeResult, error) {
	return ProbeResult{Status: "success", LatencyMS: 12, CheckedAt: time.Now()}, p.err
}
func (p proberStub) Discover(context.Context, ProviderCredential) ([]DiscoveredModel, error) {
	return []DiscoveredModel{{UpstreamModelID: "qwen-image", Name: "Qwen Image", ModelType: "image"}}, p.err
}

func TestCreateProviderNormalizesInput(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, proberStub{})
	_, err := service.CreateProvider(context.Background(), CreateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", Code: " Primary_AI ", Name: " 主模型连接 ",
		BaseURL: "https://models.example.com/v1/", APIKey: " secret ", StreamUsageEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.createdProvider.Code != "primary_ai" || repository.createdProvider.BaseURL != "https://models.example.com/v1" || repository.createdProvider.RequestTimeoutMS != 15000 || !repository.createdProvider.StreamUsageEnabled {
		t.Fatalf("供应商参数未正确规范化: %#v", repository.createdProvider)
	}
}

func TestCreateProviderRejectsEmbeddedCredentials(t *testing.T) {
	service := NewService(&repositoryStub{}, proberStub{})
	_, err := service.CreateProvider(context.Background(), CreateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", Code: "primary", Name: "主连接",
		BaseURL: "https://user:pass@example.com/v1", APIKey: "secret",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestUpdateProviderAcceptsKnownProviderKind(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, proberStub{})
	_, err := service.UpdateProvider(context.Background(), UpdateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1",
		Name: " DeepSeek 生产连接 ", ProviderKind: " DeepSeek ", BaseURL: "https://api.deepseek.com/", RequestTimeoutMS: 15000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.updatedProvider.ProviderKind != "deepseek" || repository.updatedProvider.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("供应商修改参数未正确规范化: %#v", repository.updatedProvider)
	}
}

func TestUpdateProviderModelListURL(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, proberStub{})
	endpoint := " https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/models "
	_, err := service.UpdateProvider(context.Background(), UpdateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1",
		Name: "百炼", ProviderKind: "aliyun_bailian", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		ModelListURL: &endpoint, RequestTimeoutMS: 15000,
	})
	if err != nil || repository.updatedProvider.ModelListURL == nil || *repository.updatedProvider.ModelListURL != "https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/models" {
		t.Fatalf("模型目录地址未正确传递: input=%#v err=%v", repository.updatedProvider, err)
	}
	bad := "https://user:secret@example.com/api/v1/models"
	_, err = service.UpdateProvider(context.Background(), UpdateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1",
		Name: "百炼", ProviderKind: "aliyun_bailian", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		ModelListURL: &bad, RequestTimeoutMS: 15000,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("含账号信息的目录地址应被拒绝: %v", err)
	}
}

func TestCreateProviderRejectsUnknownProviderKind(t *testing.T) {
	service := NewService(&repositoryStub{}, proberStub{})
	_, err := service.CreateProvider(context.Background(), CreateProviderInput{
		OrganizationID: "org-1", ActorID: "user-1", Code: "unknown", Name: "未知供应商",
		ProviderKind: "unknown", BaseURL: "https://models.example.com/v1", APIKey: "secret",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("错误 = %v，期望 ErrInvalidInput", err)
	}
}

func TestCreateModelNormalizesCapabilities(t *testing.T) {
	repository := &repositoryStub{}
	service := NewService(repository, proberStub{})
	_, err := service.CreateModel(context.Background(), CreateModelInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderConnectionID: "provider-1",
		Code: " Main_Model ", UpstreamModelID: "gpt-compatible", Name: "主模型",
		Capabilities: []string{"Tools", "text", "tools"}, ContextWindowTokens: 128000, MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.createdModel.Code != "main_model" || len(repository.createdModel.Capabilities) != 2 || repository.createdModel.ModelType != "chat" {
		t.Fatalf("模型参数未正确规范化: %#v", repository.createdModel)
	}
}

func TestProbeDisabledProvider(t *testing.T) {
	repository := &repositoryStub{credential: ProviderCredential{Provider: Provider{Status: "disabled"}}}
	service := NewService(repository, proberStub{})
	_, err := service.ProbeProvider(context.Background(), ProbeInput{OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1"})
	if !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("错误 = %v，期望 ErrProviderDisabled", err)
	}
}

func TestImportModelsUsesFreshDiscoveryAndKeepsImageType(t *testing.T) {
	repository := &repositoryStub{credential: ProviderCredential{Provider: Provider{Status: "active", Code: "bailian"}}}
	service := NewService(repository, proberStub{})
	result, err := service.ImportModels(context.Background(), ImportModelsInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1",
		UpstreamModelIDs: []string{"qwen-image"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || len(repository.importModels) != 1 || repository.importModels[0].ModelType != "image" {
		t.Fatalf("导入结果不正确: result=%#v models=%#v", result, repository.importModels)
	}
	if _, err := service.ImportModels(context.Background(), ImportModelsInput{
		OrganizationID: "org-1", ActorID: "user-1", ProviderID: "provider-1",
		UpstreamModelIDs: []string{"client-forged-model"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("不在上游列表中的模型应被拒绝: %v", err)
	}
}

func TestImportedModelCodeStableAndDistinct(t *testing.T) {
	first := ImportedModelCode("bailian", "qwen/image-v1")
	if first != ImportedModelCode("bailian", "qwen/image-v1") || first == ImportedModelCode("bailian", "qwen_image-v1") || len(first) > 80 {
		t.Fatalf("导入编码不稳定或发生碰撞: %q", first)
	}
	if ImportedModelCode("long-provider-code-with-shared-prefix-aaaaaaaaaaaaaaaaaaaaaa-left", "same-model") == ImportedModelCode("long-provider-code-with-shared-prefix-aaaaaaaaaaaaaaaaaaaaaa-right", "same-model") {
		t.Fatal("不同供应商不能生成相同编码")
	}
}
