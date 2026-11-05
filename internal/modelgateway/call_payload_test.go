package modelgateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCallPayloadPreservesUsefulJSONAfterRedaction(t *testing.T) {
	raw := []byte(`{"model":"video","Authorization":"Bearer abcdefghijk","data":[{"b64_json":"fixture-bytes"}],"audio":{"data":"fixture-audio","format":"wav"},"input":{"prompt":"保留创作提示","media":["data:image/png;base64,fixture-image","image-job:existing"]},"output":{"task_id":"wan-123","task_status":"SUCCEEDED","video_url":"https://name:secret@example.invalid/out.mp4?download=1&Signature=fixture-signature&x-oss-security-token=fixture-token"}}`)
	got, truncated := sanitizeCallPayload(raw)
	if truncated || !json.Valid(got) {
		t.Fatalf("脱敏结果应保留完整 JSON，truncated=%v，内容=%s", truncated, got)
	}
	for _, fragment := range []string{"wan-123", "SUCCEEDED", "https://example.invalid/out.mp4?download=1", "保留创作提示", "image-job:existing", "wav"} {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("诊断信息被误删除：%q", fragment)
		}
	}
	for _, fragment := range []string{"abcdefghijk", "fixture-bytes", "fixture-image", "fixture-audio", "fixture-signature", "fixture-token", "name:secret"} {
		if strings.Contains(string(got), fragment) || !strings.Contains(string(raw), fragment) {
			t.Errorf("日志未脱敏或误改原始内容：%q", fragment)
		}
	}
}

func TestCallPayloadPreservesSSEEventsAndOmitsBrokenStructuredTail(t *testing.T) {
	raw := []byte("event: chunk\r\ndata: {\"choices\":[{\"delta\":\r\ndata: {\"content\":\"可读正文 data:image/png;base64,sensitive-bytes\"}}]}\r\n\r\ndata: [DONE]\r\n\r\ndata: {\"authorization\":\"not-a-complete-secret")
	got, _ := sanitizeCallPayload(raw)
	for _, fragment := range []string{"event: chunk", "可读正文", "data: [DONE]", "logNote"} {
		if !strings.Contains(string(got), fragment) {
			t.Errorf("事件缺少诊断信息 %q", fragment)
		}
	}
	if strings.Contains(string(got), "sensitive-bytes") || strings.Contains(string(got), "not-a-complete-secret") {
		t.Fatal("SSE 中不应保留媒体二进制或截断的凭据")
	}
}

func TestCallPayloadBoundsAndLeavesSafePayloadUnchanged(t *testing.T) {
	raw := []byte("  {\"choices\":[{\"text\":\"正文\"}]}  ")
	if got, cut := sanitizeCallPayload(raw); cut || !bytes.Equal(got, raw) {
		t.Fatalf("无敏感字段的小响应应原样保留：%q", got)
	}
	got, cut := sanitizeCallPayload(bytes.Repeat([]byte("一"), maxCallLogPayload))
	if !cut || len(got) > maxCallLogPayload || !utf8.Valid(got) || !bytes.HasSuffix(got, []byte(logTail)) {
		t.Fatalf("日志截断不合规：长度 %d，截断 %v", len(got), cut)
	}
}
