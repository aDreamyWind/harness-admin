package modelgateway

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type CallRecorder interface {
	RecordCall(context.Context, RecordCallInput) error
}

type CallLogOptions struct {
	Source, Operation, SegmentVideoID, RequestID, SourceIP string
	RequestBody                                            []byte
	Timing                                                 *CallTiming
}

type CallOutcome struct {
	Result, ErrorCode, ErrorMessage             string
	HTTPStatus                                  int
	ResponseBody                                []byte
	ResponseTruncated                           bool
	PromptTokens, CompletionTokens, TotalTokens int
	UsageReported                               bool
}

type callRequestIDKey struct{}

func WithCallRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callRequestIDKey{}, id)
}

// ExecuteRecordedCall 在供应商响应读完后统一记录一次调用及用量。
// 调用方负责解释响应，记录失败由调用方按业务语义处理。
func ExecuteRecordedCall(ctx context.Context, recorder CallRecorder, prepared PreparedRequest, options CallLogOptions, execute func() CallOutcome) error {
	started := time.Now()
	outcome := execute()
	requestID := options.RequestID
	if requestID == "" {
		requestID, _ = ctx.Value(callRequestIDKey{}).(string)
	}
	if requestID == "" || len(requestID) > 120 {
		requestID = uuid.NewString()
	}
	if outcome.HTTPStatus == 0 {
		outcome.HTTPStatus = http.StatusBadGateway
	}
	if outcome.Result == "" {
		outcome.Result = "success"
		if outcome.HTTPStatus < 200 || outcome.HTTPStatus >= 400 || outcome.ErrorCode != "" || outcome.ErrorMessage != "" {
			outcome.Result = "failed"
		}
	}
	request := options.RequestBody
	if request == nil {
		request = prepared.Body
	}
	caller, route := prepared.Caller, prepared.Route
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return recorder.RecordCall(recordCtx, RecordCallInput{
		Timing: options.Timing, SegmentVideoID: options.SegmentVideoID,
		OrganizationID: caller.OrganizationID, UserID: caller.UserID, APIKeyID: caller.APIKeyID,
		DepartmentID: caller.DepartmentID, AuthType: caller.AuthType,
		ModelID: route.ModelID, ProviderID: route.ProviderID, ModelCode: route.ModelCode,
		ModelType: route.ModelType, UpstreamModelID: route.UpstreamModelID, Streaming: prepared.Streaming,
		RequestID: requestID, SourceIP: options.SourceIP, Source: options.Source, Operation: options.Operation,
		Result: outcome.Result, HTTPStatus: outcome.HTTPStatus, LatencyMS: time.Since(started).Milliseconds(),
		PromptTokens: outcome.PromptTokens, CompletionTokens: outcome.CompletionTokens, TotalTokens: outcome.TotalTokens,
		UsageReported: outcome.UsageReported, ErrorCode: outcome.ErrorCode, ErrorMessage: outcome.ErrorMessage,
		RequestBody: request, ResponseBody: outcome.ResponseBody, ResponseTruncated: outcome.ResponseTruncated,
	})
}
