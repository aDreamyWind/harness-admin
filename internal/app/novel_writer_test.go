package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"harness-admin/internal/modelgateway"
	"harness-admin/internal/novel"
)

func TestNovelWriterAssemblesStreamWithoutLoggingManuscript(t *testing.T) {
	gateway := &dramaGatewayStub{}
	forwarder := &dramaStreamForwarderStub{payload: `data: {"choices":[{"delta":{"content":"{\"review\":"}}]}

data: {"choices":[{"delta":{"content":"[\"核对线索\"]}"},"finish_reason":"stop"}],"usage":{"total_tokens":20}}

data: [DONE]

`}
	writer := &novelWriter{completion: dramaWriter{
		gateway: gateway, forwarder: forwarder, source: "ai_novel", systemPrompt: "小说审稿", omitCallContent: true,
	}}
	result, err := writer.Complete(context.Background(), novel.Owner{OrganizationID: "org", UserID: "user"}, "writer", "待审正文")
	if err != nil || result != `{"review":["核对线索"]}` {
		t.Fatalf("流式小说输出未完整组装: %q，%v", result, err)
	}
	if !forwarder.streaming || gateway.record.Source != "ai_novel" || gateway.record.TotalTokens != 20 ||
		len(gateway.record.RequestBody) != 0 || len(gateway.record.ResponseBody) != 0 {
		t.Fatalf("调用未正确记录或泄露正文: %+v", gateway.record)
	}
}

func TestNovelCoverUsesPortraitImageAndPrivateModelLogs(t *testing.T) {
	const story = "用户故事内容"
	ctx := context.Background()
	owner := novel.Owner{OrganizationID: "org", UserID: "user"}
	gateway := &visualGatewayStub{}
	forwarder := &visualForwarderStub{body: `{"data":[{"b64_json":"` + visualPNG + `"}]}`}
	writer := &novelWriter{image: &dramaImageEngine{
		gateway: gateway, forwarder: forwarder, source: "ai_novel", omitCallContent: true,
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
		len(gateway.record.RequestBody) != 0 || len(gateway.record.ResponseBody) != 0 {
		t.Fatalf("封面调用未正确记录或泄露内容: %+v", gateway.record)
	}

	forwarder.status, forwarder.body = http.StatusBadRequest, `{"error":{"message":"`+story+`"}}`
	_, err = writer.GenerateCover(ctx, owner, "painter", story)
	if err == nil || strings.Contains(err.Error(), story) || gateway.record.Result != "failed" ||
		len(gateway.record.RequestBody) != 0 || len(gateway.record.ResponseBody) != 0 || strings.Contains(gateway.record.ErrorMessage, story) {
		t.Fatalf("封面调用失败未正确记录或泄露内容: %v，%+v", err, gateway.record)
	}

	gateway.prepareErr = modelgateway.ErrModelNotAllowed
	if _, err := writer.GenerateCover(ctx, owner, "painter", story); !errors.Is(err, novel.ErrCoverModel) {
		t.Fatalf("不可用的图片模型未识别: %v", err)
	}
}
