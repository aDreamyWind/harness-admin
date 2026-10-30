package httpserver

import (
	"context"
	"encoding/json"
	"harness-admin/internal/drama"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type styleServiceStub struct{}

func (styleServiceStub) Styles(context.Context) ([]drama.StyleOption, error) {
	return []drama.StyleOption{{ID: "dark-concept", Name: "黑暗原画概念风", Category: "2D", PreviewURL: "/drama-style-sheet.png", PreviewX: 1, PreviewY: 1}}, nil
}
func TestDramaStylesRequireSessionAndExposeOnlyPublicMetadata(t *testing.T) {
	session := accountTestSession()
	router := New(Dependencies{Environment: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &csrfCheckingAuthStub{&accountAuthStub{session: session}}, DramaStyles: styleServiceStub{}, SessionCookie: "session"})
	for _, auth := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/api/v1/ai-drama/styles", nil)
		if auth {
			req.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if !auth {
			if res.Code != 401 {
				t.Fatal("缺少会话未拒绝")
			}
			continue
		}
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		var reply struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Data) != 1 || len(reply.Data[0]) != 6 || reply.Data[0]["id"] != "dark-concept" {
			t.Fatal("公开字段契约错误")
		}
		for _, key := range []string{"imagePrompt", "videoPrompt", "prompt"} {
			if _, ok := reply.Data[0][key]; ok {
				t.Fatal("泄露私有提示词")
			}
		}
	}
}
