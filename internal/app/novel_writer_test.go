package app

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"testing"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/novel"
)

func TestNovelWriterAssemblesStreamAndRecordsCall(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaStreamForwarderStub{payload: `data: {"choices":[{"delta":{"content":"{\"review\":"}}]}

data: {"choices":[{"delta":{"content":"[\"核对线索\"]}"},"finish_reason":"stop"}],"usage":{"total_tokens":20}}

data: [DONE]

`}
	writer := &novelWriter{completion: dramaWriter{
		gateway: gateway, forwarder: forwarder, source: "ai_novel", systemPrompt: "小说审稿",
	}}
	result, err := writer.Complete(context.Background(), novel.Owner{OrganizationID: "org", UserID: "user"}, "writer", "待审正文")
	if err != nil || result != `{"review":["核对线索"]}` {
		t.Fatalf("流式小说输出未完整组装: %q，%v", result, err)
	}
	if !forwarder.streaming || gateway.record.Source != "ai_novel" || gateway.record.TotalTokens != 20 ||
		!strings.Contains(string(gateway.record.RequestBody), "待审正文") || string(gateway.record.ResponseBody) != forwarder.payload {
		t.Fatalf("未记录小说调用参数、流式响应或用量: %+v", gateway.record)
	}
}

func TestNovelStoryMemoryWriterRecordsJobAndOperation(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaStreamForwarderStub{payload: "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"facts\\\":[]}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"}
	writer := &novelWriter{completion: dramaWriter{gateway: gateway, forwarder: forwarder, source: "ai_novel"}}
	const jobID = "30941f8f-02d0-4f93-8c0a-0b58869a09e3"
	got, err := writer.CompleteStoryMemory(context.Background(), novel.Owner{}, "writer", "提取定稿事实", jobID)
	if err != nil || got != `{"facts":[]}` || gateway.record.RequestID != jobID || gateway.record.Source != "ai_novel" ||
		gateway.record.Operation != "story_memory" || gateway.record.Timing == nil || gateway.record.Timing.Stage != "故事档案" {
		t.Fatalf("故事档案调用未按任务关联记录: %q，%v", got, err)
	}
}

func TestNovelIdeaRefinementClassifiesContentInspectionInStream(t *testing.T) {
	for _, tc := range []struct {
		name, errorFrame string
		wantCode         string
	}{
		{"百炼兼容接口", `{"error":{"code":"data_inspection_failed","type":"data_inspection_failed","message":"供应商内部信息"}}`, modelContentInspectionCode},
		{"百炼错误码别名", `{"error":{"type":"DataInspectionFailed","message":"供应商内部信息"}}`, modelContentInspectionCode},
		{"其他流式错误", `{"error":{"code":"internal_error","message":"data_inspection_failed"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := &dramaGatewayStub{}
			payload := "data: {\"choices\":[{\"delta\":{\"content\":\"部分正文\"}}]}\n\ndata: " + tc.errorFrame + "\n\n"
			writer := &novelWriter{completion: dramaWriter{
				gateway: gateway, forwarder: &dramaStreamForwarderStub{payload: payload}, source: "ai_novel",
			}}
			service := novel.NewService(nil, writer, nil, nil)
			result, err := service.RefineIdea(context.Background(), novel.Owner{}, novel.RefineIdeaInput{
				Brief: "旧城守钟人", Instruction: "指定主角姓氏", ModelCode: "writer",
			})
			if err == nil || result.Brief != "" || errors.Is(err, novel.ErrContentInspection) != (tc.wantCode != "") {
				t.Fatalf("流式错误分类或返回结果不正确：%+v，%v", result, err)
			}
			if gateway.record.Result != "failed" || gateway.record.HTTPStatus != http.StatusOK ||
				gateway.record.ErrorCode != tc.wantCode || string(gateway.record.ResponseBody) != payload ||
				gateway.record.Timing == nil || gateway.record.Timing.FirstContentAt == nil {
				t.Fatal("流式错误没有按实际状态和阶段记录")
			}
		})
	}
}

type ideaGatewayStub struct {
	dramaGatewayStub
	baseURL, upstreamID string
}

func (g *ideaGatewayStub) PrepareForUser(ctx context.Context, org, user string, body []byte) (modelgateway.PreparedRequest, error) {
	prepared, err := g.dramaGatewayStub.PrepareForUser(ctx, org, user, body)
	prepared.Route.ProviderBaseURL = g.baseURL
	prepared.Route.UpstreamModelID = g.upstreamID
	return prepared, err
}

func TestNovelIdeaRefinementTemperatureUsesVerifiedRoute(t *testing.T) {
	const model = "deepseek-v4-flash-0731"
	for _, tc := range []struct {
		name, baseURL, upstreamID string
		withTemperature           bool
	}{
		{"百炼通用接入", "https://dashscope.aliyuncs.com/compatible-mode/v1", model, true},
		{"百炼工作空间接入", "https://workspace.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", model, true},
		{"百炼其他模型", "https://dashscope.aliyuncs.com/compatible-mode/v1", "deepseek-v4-pro", false},
		{"其他供应商同名模型", "https://dashscope.aliyuncs.com.example.com/compatible-mode/v1", model, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			owner := novel.Owner{OrganizationID: "org", UserID: "user"}
			gateway := &ideaGatewayStub{baseURL: tc.baseURL, upstreamID: tc.upstreamID}
			forwarder := &dramaForwarderStub{}
			writer := &novelWriter{completion: dramaWriter{gateway: gateway, forwarder: forwarder, source: "ai_novel"}}
			if _, err := writer.Complete(ctx, owner, model, "大纲或正文"); err != nil {
				t.Fatal(err)
			}
			var ordinary map[string]any
			if err := json.Unmarshal(gateway.record.RequestBody, &ordinary); err != nil {
				t.Fatal(err)
			}
			if _, ok := ordinary["temperature"]; ok {
				t.Fatal("普通小说任务未保留供应商默认温度")
			}

			service := novel.NewService(nil, writer, nil, nil)
			forwarderIdea := &dramaStreamForwarderStub{payload: "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"brief\\\":\\\"改写后的灵感\\\"}\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"}
			writer.completion.forwarder = forwarderIdea
			result, err := service.RefineIdea(ctx, owner, novel.RefineIdeaInput{Brief: "旧城守钟人", ModelCode: model})
			if err != nil || result.Brief != "改写后的灵感" {
				t.Fatalf("灵感优化失败: %q，%v", result.Brief, err)
			}
			var refined map[string]any
			if err := json.Unmarshal(gateway.record.RequestBody, &refined); err != nil {
				t.Fatal(err)
			}
			temperature, exists := refined["temperature"]
			if tc.withTemperature {
				value, isNumber := temperature.(float64)
				if !isNumber || !(value < 2 && math.Nextafter32(float32(value), 2) == 2) {
					t.Fatalf("灵感优化温度没有取到小于 2 的上界附近: %v", temperature)
				}
			} else if exists {
				t.Fatalf("不匹配的上游路由不应传采样温度: %s", gateway.record.RequestBody)
			}
			if refined["enable_thinking"] != ordinary["enable_thinking"] || refined["reasoning_effort"] != ordinary["reasoning_effort"] {
				t.Fatalf("灵感优化改变了原有思考参数: %s", gateway.record.RequestBody)
			}
		})
	}
}

func TestNovelCoverUsesPortraitImageAndRecordsCall(t *testing.T) {
	const story = "用户故事内容"
	ctx := context.Background()
	owner := novel.Owner{OrganizationID: "org", UserID: "user"}
	gateway := &visualGatewayStub{}
	forwarder := &visualForwarderStub{body: `{"data":[{"b64_json":"` + visualPNG + `"}]}`}
	writer := &novelWriter{image: &dramaImageEngine{
		gateway: gateway, forwarder: forwarder, source: "ai_novel",
	}}
	data, err := writer.GenerateCover(ctx, owner, "painter", story)
	if err != nil || len(data) == 0 {
		t.Fatalf("封面未生成: %d 字节，%v", len(data), err)
	}
	var request map[string]any
	if err := json.Unmarshal(forwarder.payload, &request); err != nil || request["size"] != "1024x1536" || request["prompt"] != story || request["n"] != float64(1) {
		t.Fatalf("封面请求参数错误: %s，%v", forwarder.payload, err)
	}
	if gateway.record.Source != "ai_novel" || gateway.record.Operation != "image" || gateway.record.UserID != "user" ||
		string(gateway.record.RequestBody) != string(forwarder.payload) || string(gateway.record.ResponseBody) != forwarder.body {
		t.Fatalf("封面调用未记录请求和响应: %+v", gateway.record)
	}

	forwarder.status, forwarder.body = http.StatusBadRequest, `{"error":{"message":"`+story+`"}}`
	_, err = writer.GenerateCover(ctx, owner, "painter", story)
	if err == nil || strings.Contains(err.Error(), story) || gateway.record.Result != "failed" ||
		!strings.Contains(string(gateway.record.RequestBody), story) || string(gateway.record.ResponseBody) != forwarder.body ||
		!strings.Contains(gateway.record.ErrorMessage, story) {
		t.Fatalf("封面调用失败未正确记录或泄露内容: %v，%+v", err, gateway.record)
	}

	gateway.prepareErr = modelgateway.ErrModelNotAllowed
	if _, err := writer.GenerateCover(ctx, owner, "painter", story); !errors.Is(err, novel.ErrCoverModel) {
		t.Fatalf("不可用的图片模型未识别: %v", err)
	}
}
