package modelgateway

import (
	"context"
	"strings"
	"testing"
)

type callRecorderFunc func(context.Context, RecordCallInput) error

func (record callRecorderFunc) RecordCall(ctx context.Context, input RecordCallInput) error {
	return record(ctx, input)
}

func TestRecordedCallStoresCancelledAttemptOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(WithCallRequestID(context.Background(), "business-request"))
	defer cancel()
	repository := &repositoryStub{}
	service := NewService(repository, nil, nil)
	count := 0
	recorder := callRecorderFunc(func(ctx context.Context, input RecordCallInput) error {
		count++
		if ctx.Err() != nil {
			t.Fatal("客户端取消后仍应尝试记录实际发出的调用")
		}
		return service.RecordCall(ctx, input)
	})
	prepared := PreparedRequest{
		Caller: Caller{OrganizationID: "org-1", UserID: "user-1", AuthType: "user"},
		Route:  Route{ModelID: "model-1", ProviderID: "provider-1", ModelType: "video"},
		Body:   []byte(`{"prompt":"排查用文本","api_key":"private-value"}`),
	}
	err := ExecuteRecordedCall(ctx, recorder, prepared, CallLogOptions{
		Source: "ai_new_feature", Operation: "video", SegmentVideoID: "video-1",
	}, func() CallOutcome {
		cancel()
		return CallOutcome{HTTPStatus: 502, ErrorMessage: "供应商连接中断"}
	})
	if err != nil || count != 1 {
		t.Fatalf("每次调用应只记录一次，实际 %d 次，错误 %v", count, err)
	}
	got := repository.recorded
	if got.RequestID != "business-request" || got.Result != "failed" || got.SegmentVideoID != "video-1" ||
		got.Source != "ai_new_feature" || got.ErrorMessage != "供应商连接中断" {
		t.Fatalf("排查日志缺少业务关联或失败信息: %+v", got)
	}
	if strings.Contains(string(got.RequestBody), "private-value") ||
		!strings.Contains(string(got.RequestBody), "排查用文本") ||
		!strings.Contains(string(prepared.Body), "private-value") {
		t.Fatal("应只脱敏保存副本，同时保留可排查的文本")
	}
}
