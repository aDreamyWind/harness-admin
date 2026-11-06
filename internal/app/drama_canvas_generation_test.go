package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"

	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type canvasGenerationCatalogStub []modelgateway.AvailableModel

func (s canvasGenerationCatalogStub) ListModelsForUser(context.Context, string, string) ([]modelgateway.AvailableModel, error) {
	return s, nil
}

func TestDramaCanvasGenerationSendsImageReferencesAndLogsNodeIDs(t *testing.T) {
	gateway := &visualGatewayStub{}
	forwarder := &visualForwarderStub{body: `{"data":[{"b64_json":"` + visualPNG + `"}]}`}
	engine := &dramaCanvasGenerationEngine{
		catalog: canvasGenerationCatalogStub{{Code: "canvas-painter", ModelType: "image", ProviderKind: "aliyun_bailian", UpstreamModelID: "qwen-image-3.0-pro"}},
		image:   &dramaImageEngine{gateway: gateway, forwarder: forwarder}, referenceDownload: forwarder,
	}
	localImage := "data:image/jpeg;base64,YWJj"
	data, mime, err := engine.GenerateCanvasMedia(context.Background(), drama.CanvasGenerationRequest{
		OrganizationID: "org", UserID: "user", JobID: "canvas-job", ModelCode: "canvas-painter", Kind: "image",
		Prompt: "在原图中添加一盏灯", AspectRatio: "9:16", ReferenceImages: []string{localImage, "https://example.com/second.png"},
		ReferenceIDs: []string{"first-node", "second-node"},
	})
	if err != nil || mime != "image/png" || len(data) == 0 {
		t.Fatalf("生成失败：%s，%v", mime, err)
	}
	var sent, logged struct {
		Prompt, Size string
		Image        []string
	}
	if err := json.Unmarshal(forwarder.payload, &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(gateway.record.RequestBody, &logged); err != nil {
		t.Fatal(err)
	}
	if sent.Prompt != "在原图中添加一盏灯" || sent.Size != "864x1536" || !slices.Equal(sent.Image, []string{localImage, "data:image/png;base64," + visualPNG}) {
		t.Fatalf("提示词、比例或参考图顺序不正确：%+v", sent)
	}
	if !slices.Equal(logged.Image, []string{"canvas-node:first-node", "canvas-node:second-node"}) ||
		gateway.record.Source != "ai_drama" || gateway.record.Operation != "image" || gateway.record.RequestID != "canvas-job" {
		t.Fatalf("调用记录未正确关联任务或脱敏：%+v", gateway.record)
	}
}

func TestDramaCanvasGenerationUsesModelSupportedImageResolution(t *testing.T) {
	t.Parallel()
	gateway := &visualGatewayStub{}
	forwarder := &visualForwarderStub{body: `{"data":[{"b64_json":"` + visualPNG + `"}]}`}
	engine := &dramaCanvasGenerationEngine{
		catalog: canvasGenerationCatalogStub{
			{Code: "qwen", ModelType: "image", ProviderKind: "aliyun_bailian", UpstreamModelID: "qwen-image-3.0-pro"},
			{Code: "standard", ModelType: "image", ProviderKind: "openai_compatible", UpstreamModelID: "qwen-image-3.0-pro"},
		},
		image: &dramaImageEngine{gateway: gateway, forwarder: forwarder},
	}
	ctx := context.Background()
	models, err := engine.CanvasGenerationModels(ctx, "org", "user")
	if err != nil || len(models) != 2 || !slices.Equal(models[0].Resolutions, []string{"1K", "2K"}) || !slices.Equal(models[1].Resolutions, []string{"1K"}) {
		t.Fatalf("模型目录应按供应商及模型报告已确认的分辨率：%+v，%v", models, err)
	}
	in := drama.CanvasGenerationRequest{OrganizationID: "org", UserID: "user", JobID: "canvas-job", ModelCode: "standard", Kind: "image", Prompt: "月光下的山谷", Resolution: "2K", AspectRatio: "9:16"}
	if _, _, err := engine.GenerateCanvasMedia(ctx, in); !errors.Is(err, drama.ErrInvalidInput) || len(forwarder.payload) > 0 {
		t.Fatalf("不支持的分辨率不应发起模型调用：%v", err)
	}
	in.ModelCode = "qwen"
	for ratio, want := range map[string]string{"16:9": "2560x1440", "9:16": "1440x2560", "1:1": "2048x2048"} {
		in.AspectRatio = ratio
		if _, _, err := engine.GenerateCanvasMedia(ctx, in); err != nil {
			t.Fatalf("生成 %s 图片失败：%v", ratio, err)
		}
		var sent, logged struct{ Size string }
		if err := json.Unmarshal(forwarder.payload, &sent); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(gateway.record.RequestBody, &logged); err != nil {
			t.Fatal(err)
		}
		if sent.Size != want || logged.Size != want {
			t.Fatalf("%s 的模型请求和调用日志应保留尺寸 %s，实际为 %s 与 %s", ratio, want, sent.Size, logged.Size)
		}
	}
}

func TestDramaCanvasGenerationUsesVideoQueryAndAllowsTextOnlyWan3(t *testing.T) {
	t.Parallel()
	gateway, forwarder := &dramaVideoGatewayStub{}, &dramaVideoForwarderStub{}
	engine := &dramaCanvasGenerationEngine{
		catalog: canvasGenerationCatalogStub{
			{Code: "wan3", ModelType: "video", ProviderKind: "aliyun_bailian", UpstreamModelID: "wan3.0-video"},
			{Code: "wan27", ModelType: "video", ProviderKind: "aliyun_bailian", UpstreamModelID: "wan2.7-r2v"},
			{Code: "unknown", ModelType: "video", ProviderKind: "aliyun_bailian", UpstreamModelID: "unknown"},
		},
		video: &dramaVideoEngine{gateway: gateway, forwarder: forwarder},
	}
	in := drama.CanvasGenerationRequest{
		OrganizationID: "org", UserID: "user", JobID: "canvas-job", ModelCode: "wan27", Kind: "video",
		Prompt: "风吹起灯笼", AspectRatio: "16:9", Duration: 12,
	}
	if _, _, err := engine.GenerateCanvasMedia(context.Background(), in); !errors.Is(err, drama.ErrInvalidInput) || len(gateway.records) != 0 {
		t.Fatalf("万相 2.7 缺少参考图时应拒绝：%v", err)
	}
	models, err := engine.CanvasGenerationModels(context.Background(), "org", "user")
	if err != nil || len(models) != 2 || models[0].MaxReferences != 10 || models[0].MaxDuration != 30 || models[1].MaxReferences != 5 {
		t.Fatalf("视频能力不正确：%+v，%v", models, err)
	}
	in.ModelCode = "wan3"
	data, mime, err := engine.GenerateCanvasMedia(context.Background(), in)
	if err != nil || mime != "video/mp4" || len(data) == 0 {
		t.Fatalf("视频生成失败：%s，%v", mime, err)
	}
	if strings.Contains(forwarder.payload, `"media"`) || !strings.Contains(forwarder.payload, `"prompt":"风吹起灯笼"`) {
		t.Fatalf("万相 3.0 文生视频请求不正确：%s", forwarder.payload)
	}
	if len(gateway.records) != 2 || gateway.records[0].Operation != "video" || gateway.records[1].Operation != "video_task_query" {
		t.Fatalf("视频提交和查询应分别记录：%+v", gateway.records)
	}
	for _, call := range gateway.records {
		if call.Source != "ai_drama" || call.SegmentVideoID != "" || call.RequestID != "canvas-job" {
			t.Fatalf("画布调用记录归属不正确：%+v", call)
		}
	}
}

func TestDramaCanvasGenerationWan27TextVideo(t *testing.T) {
	t.Parallel()
	const modelID = "wan2.7-t2v"
	gateway, forwarder := &dramaVideoGatewayStub{upstreamModelID: modelID}, &dramaVideoForwarderStub{}
	video := &dramaVideoEngine{gateway: gateway, forwarder: forwarder}
	engine := &dramaCanvasGenerationEngine{
		catalog: canvasGenerationCatalogStub{{Code: "wan-text", ModelType: "video", ProviderKind: "aliyun_bailian", UpstreamModelID: modelID}},
		video:   video,
	}
	ctx := context.Background()
	models, err := engine.CanvasGenerationModels(ctx, "org", "user")
	if err != nil || len(models) != 1 || models[0].MaxReferences != 0 || models[0].MinDuration != 2 || models[0].MaxDuration != 15 {
		t.Fatalf("文生视频能力不正确：%+v，%v", models, err)
	}
	in := drama.CanvasGenerationRequest{
		OrganizationID: "org", UserID: "user", JobID: "canvas-job", ModelCode: "wan-text", Kind: "video",
		Prompt: "风吹起灯笼", AspectRatio: "9:16", Duration: 16,
	}
	if _, _, err := engine.GenerateCanvasMedia(ctx, in); !errors.Is(err, drama.ErrInvalidInput) {
		t.Fatalf("超过时长的请求应拒绝：%v", err)
	}
	in.Duration = 15
	in.ReferenceImages = []string{"data:image/png;base64," + visualPNG}
	if _, _, err := engine.GenerateCanvasMedia(ctx, in); !errors.Is(err, drama.ErrInvalidInput) {
		t.Fatalf("有参考图的文生视频请求应拒绝：%v", err)
	}
	in.ReferenceImages = nil
	if _, err := video.GenerateSegmentVideo(ctx, drama.SegmentVideoRequest{
		OrganizationID: in.OrganizationID, UserID: in.UserID, VideoID: "segment-video",
		ModelCode: in.ModelCode, Prompt: in.Prompt, AspectRatio: in.AspectRatio, Duration: in.Duration,
	}); !errors.Is(err, drama.ErrModelUnavailable) || len(gateway.records) != 0 {
		t.Fatalf("分镜不应选用文生视频：%v", err)
	}
	data, mime, err := engine.GenerateCanvasMedia(ctx, in)
	if err != nil || mime != "video/mp4" || len(data) == 0 {
		t.Fatalf("文生视频生成失败：%s，%v", mime, err)
	}
	var payload struct {
		Model      string
		Input      map[string]any
		Parameters map[string]any
	}
	if err := json.Unmarshal([]byte(forwarder.payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != modelID || len(payload.Input) != 1 || payload.Input["prompt"] != in.Prompt ||
		payload.Parameters["duration"] != float64(in.Duration) || payload.Parameters["ratio"] != in.AspectRatio ||
		payload.Parameters["resolution"] != "720P" || payload.Parameters["prompt_extend"] != false {
		t.Fatalf("文生视频请求错误：%+v", payload)
	}
	if _, ok := payload.Parameters["audio"]; ok || len(gateway.records) != 2 {
		t.Fatalf("不应发送音频开关，且应分别记录提交和轮询：%+v", gateway.records)
	}
}

func TestDramaCanvasReferenceDownloadFailureHidesSignedURL(t *testing.T) {
	const link = "https://example.com/image.png?signature=secret"
	engine := &dramaCanvasGenerationEngine{
		referenceDownload: &visualForwarderStub{downloadErr: &url.Error{Op: "Get", URL: link, Err: errors.New("超时")}},
	}
	_, _, err := engine.prepareReference(context.Background(), link, "image")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("下载错误应保留原因并省略签名地址：%v", err)
	}
}
