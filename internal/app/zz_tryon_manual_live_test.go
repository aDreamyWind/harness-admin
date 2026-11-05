package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/image/vector"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"harness-admin/internal/cache"
	"harness-admin/internal/config"
	"harness-admin/internal/httpserver"
	"harness-admin/internal/identity"
	"harness-admin/internal/modelgateway"
	"harness-admin/internal/modelprovider"
	"harness-admin/internal/secret"
	pgstore "harness-admin/internal/store/postgres"
	"harness-admin/internal/tryon"
)

type manualTryOnAuth struct {
	httpserver.AuthService
	session identity.Session
}

func (a manualTryOnAuth) Authenticate(_ context.Context, token string) (identity.Session, error) {
	if token != "manual-local-session" {
		return identity.Session{}, identity.ErrUnauthorized
	}
	return a.session, nil
}
func (a manualTryOnAuth) ValidateCSRF(_ identity.Session, token string) error {
	if token != "manual-local-csrf" {
		return identity.ErrInvalidCSRF
	}
	return nil
}

func TestManualLiveTryOn(t *testing.T) {
	out := os.Getenv("HARNESS_TRYON_LIVE_OUT")
	if out == "" {
		t.Skip("仅用于显式发起的真实供应商联调")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	redis, err := cache.Open(ctx, cfg.RedisAddress, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		t.Fatal(err)
	}
	defer redis.Close()
	cipher, err := secret.NewCipher(cfg.ModelCredentialKey)
	if err != nil {
		t.Fatal(err)
	}
	gateway := modelgateway.NewService(pgstore.NewModelGatewayRepository(db, cipher), cache.NewGatewayRequestLimiter(redis), pgstore.NewModelPolicyRepository(db))
	forwarder := modelprovider.NewGatewayForwarder(cfg.ModelPrivateHosts)
	engine := &tryOnEngine{gateway: gateway, forwarder: forwarder, image: &dramaImageEngine{gateway: gateway, forwarder: forwarder, source: "ai_tryon"}}
	var owner tryon.Owner
	err = db.QueryRow(ctx, `SELECT o.id,u.id FROM organizations o JOIN users u ON u.organization_id=o.id WHERE o.code='default' AND u.status='active' ORDER BY u.created_at LIMIT 1`).Scan(&owner.OrganizationID, &owner.UserID)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := manualTryOnAuth{session: identity.Session{User: identity.User{ID: owner.UserID, OrganizationID: owner.OrganizationID}}}
	handler := httpserver.New(httpserver.Dependencies{Environment: "test", Logger: logger, Auth: auth, SessionCookie: "manual-test-session", TryOn: tryon.NewService(pgstore.NewTryOnRepository(db), engine, logger)})
	prefix := "tryon-live-" + uuid.NewString()[:8]
	var assetIDs, jobIDs []string
	defer func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if len(jobIDs) > 0 {
			if _, e := db.Exec(cleanCtx, `DELETE FROM tryon_jobs WHERE organization_id=$1 AND owner_id=$2 AND id::text=ANY($3::text[])`, owner.OrganizationID, owner.UserID, jobIDs); e != nil {
				t.Error(e)
			}
		}
		if len(assetIDs) > 0 {
			if _, e := db.Exec(cleanCtx, `DELETE FROM tryon_assets WHERE organization_id=$1 AND owner_id=$2 AND id::text=ANY($3::text[])`, owner.OrganizationID, owner.UserID, assetIDs); e != nil {
				t.Error(e)
			}
		}
	}()
	defer func() {
		logs, e := db.Query(ctx, `SELECT l.operation,c.upstream_model_id,c.result,c.http_status,c.latency_ms,l.error_message,convert_from(l.response_body,'UTF8') FROM model_gateway_calls c JOIN model_call_logs l ON l.call_id=c.id WHERE c.organization_id=$1 AND c.request_id LIKE $2 ORDER BY l.created_at`, owner.OrganizationID, prefix+"-%")
		if e != nil {
			t.Error(e)
			return
		}
		defer logs.Close()
		for logs.Next() {
			var op, model, result, message, body string
			var status, ms int
			if e := logs.Scan(&op, &model, &result, &status, &ms, &message, &body); e != nil {
				t.Error(e)
				break
			}
			t.Logf("网关 %s %s %s HTTP %d %.1fs", op, model, result, status, float64(ms)/1000)
			if result == "failed" {
				var upstream struct {
					Code    string
					Message string
					Error   struct {
						Code    any
						Message string
					}
				}
				if json.Unmarshal([]byte(body), &upstream) == nil {
					t.Logf("上游错误 %s %s %v %s", upstream.Code, shortLiveText(upstream.Message), upstream.Error.Code, shortLiveText(upstream.Error.Message))
				}
				t.Logf("记录错误：%s", shortLiveText(message))
			}
		}
		if e := logs.Err(); e != nil {
			t.Error(e)
		}
	}()
	send := func(method, path, contentType, tag string, body io.Reader, want int) []byte {
		t.Helper()
		req := httptest.NewRequestWithContext(ctx, method, "/api/v1/ai-tryon"+path, body)
		req.AddCookie(&http.Cookie{Name: "manual-test-session", Value: "manual-local-session"})
		req.Header.Set("X-CSRF-Token", "manual-local-csrf")
		req.Header.Set("X-Request-ID", prefix+"-"+tag)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		res := httptest.NewRecorder()
		start := time.Now()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s 响应 %d（期望 %d，%.1fs）: %s", method, path, res.Code, want, time.Since(start).Seconds(), shortLiveText(res.Body.String()))
		}
		return res.Body.Bytes()
	}
	upload := func(path, tag string, data []byte, fields map[string]string, want int) []byte {
		t.Helper()
		var buf bytes.Buffer
		form := multipart.NewWriter(&buf)
		file, e := form.CreateFormFile("file", "fixture.png")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.Write(data); e != nil {
			t.Fatal(e)
		}
		for k, v := range fields {
			if e = form.WriteField(k, v); e != nil {
				t.Fatal(e)
			}
		}
		if e = form.Close(); e != nil {
			t.Fatal(e)
		}
		return send("POST", path, form.FormDataContentType(), tag, &buf, want)
	}
	post := func(path, tag string, data any, want int) []byte {
		t.Helper()
		payload, e := json.Marshal(data)
		if e != nil {
			t.Fatal(e)
		}
		return send("POST", path, "application/json", tag, bytes.NewReader(payload), want)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(out, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	models := decodeTryOnLive[tryon.Models](t, send("GET", "/models", "", "models", nil, 200))
	find := func(items []tryon.Model, needle string) string {
		t.Helper()
		for _, item := range items {
			if strings.Contains(item.Code, needle) {
				return item.Code
			}
		}
		t.Fatalf("当前用户没有 %s 权限，可选列表：%+v", needle, items)
		return ""
	}
	detection := find(models.Detection, "qwen3-vl-plus")
	painter := find(models.Image, "qwen-image-3.0-pro")
	find(models.Person, "qwen-image-3.0-pro")
	t.Log("当前账号可使用 Qwen3-VL Plus 和 Qwen-Image 3.0 Pro")
	raw, err := syntheticTryOnGarments()
	if err != nil {
		t.Fatal(err)
	}
	write("source-outfit.png", raw)
	candidates := decodeTryOnLive[[]tryon.Candidate](t, upload("/garments/detect", "detect", raw, map[string]string{"modelCode": detection}, 200))
	candidateJSON, _ := json.MarshalIndent(candidates, "", "  ")
	write("detection.json", candidateJSON)
	var chosen *tryon.Candidate
	for i := range candidates {
		t.Logf("识别 %s（%s） %v", candidates[i].Name, candidates[i].Category, candidates[i].Box)
		if candidates[i].Category == "外套" && chosen == nil {
			chosen = &candidates[i]
		}
	}
	if chosen == nil {
		t.Fatal("样例图的外套未识别出来")
	}
	coords, _ := json.Marshal(chosen.Box)
	garment := decodeTryOnLive[tryon.Asset](t, upload("/assets", "save-garment", raw, map[string]string{"kind": tryon.Garment, "name": chosen.Name, "category": chosen.Category, "box": string(coords)}, 201))
	assetIDs = append(assetIDs, garment.ID)
	write("garment.png", send("GET", "/assets/"+garment.ID+"/content", "", "garment", nil, 200))
	for _, sample := range []struct{ key, kind, name, description string }{
		{"2d", tryon.Person2D, "测试插画人物", "原创成年女性时装角色，平涂动画赛璐璐风格，清晰深色描边，黑色齐下巴短发，圆框黑眼镜，穿青绿色短袖圆领T恤、酒红色及踝直筒裙和黑色平底鞋。浅蓝色纯色背景，正面全身，自然站姿，双手垂在身体两侧。"},
		{"real", tryon.PersonReal, "测试写实人物", "虚构成年女性模特，黑色齐下巴短发，淡妆，穿深青绿色短袖圆领T恤、酒红色及踝直筒裙和黑色平底鞋。浅蓝色摄影棚背景，正面全身，双手自然垂在身体两侧。"},
	} {
		t.Logf("开始验证 %s", sample.key)
		person := decodeTryOnLive[tryon.Asset](t, post("/people/generate", sample.key+"-person", tryon.PersonInput{Name: sample.name, Kind: sample.kind, Description: sample.description, ModelCode: painter}, 201))
		assetIDs = append(assetIDs, person.ID)
		write(sample.key+"-before.png", send("GET", "/assets/"+person.ID+"/content", "", sample.key+"-reference", nil, 200))
		job := decodeTryOnLive[tryon.Job](t, post("/jobs", sample.key+"-tryon", tryon.JobInput{PersonID: person.ID, GarmentID: garment.ID, ModelCode: painter}, 202))
		jobIDs = append(jobIDs, job.ID)
		deadline := time.Now().Add(7 * time.Minute)
		for {
			jobs := decodeTryOnLive[[]tryon.Job](t, send("GET", "/jobs", "", sample.key+"-status", nil, 200))
			found := false
			for _, current := range jobs {
				if current.ID == job.ID {
					job = current
					found = true
					break
				}
			}
			if !found {
				t.Fatal("试穿记录不存在")
			}
			if job.Status != "running" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("试穿等待超时")
			}
			time.Sleep(3 * time.Second)
		}
		if job.Status != "succeeded" {
			t.Fatalf("%s 试穿失败：%s", sample.key, job.Error)
		}
		write(sample.key+"-after.png", send("GET", "/jobs/"+job.ID+"/result", "", sample.key+"-result", nil, 200))
		t.Logf("%s 试穿结果已导出", sample.key)
	}
	t.Log("联调批次 " + prefix)
}

func decodeTryOnLive[T any](t *testing.T, body []byte) T {
	t.Helper()
	var result struct{ Data T }
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Data
}
func shortLiveText(s string) string {
	if len(s) > 300 {
		return fmt.Sprintf("%s…", s[:300])
	}
	return s
}

// 仅用几何图形现场绘制平铺衣物。测试图不读取用户素材或人物照片。
func syntheticTryOnGarments() ([]byte, error) {
	const width, height = 960, 720
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{246, 245, 241, 255}), image.Point{}, draw.Src)
	poly := func(fill color.RGBA, coords ...float32) {
		r := vector.NewRasterizer(width, height)
		r.MoveTo(coords[0], coords[1])
		for i := 2; i < len(coords); i += 2 {
			r.LineTo(coords[i], coords[i+1])
		}
		r.ClosePath()
		r.Draw(canvas, canvas.Bounds(), image.NewUniform(fill), image.Point{})
	}
	ink := color.RGBA{71, 51, 36, 255}
	khaki := color.RGBA{188, 137, 82, 255}
	light := color.RGBA{220, 179, 126, 255}
	trim := color.RGBA{89, 62, 45, 255}
	poly(ink, 193, 139, 259, 105, 323, 105, 388, 139, 451, 493, 381, 515, 363, 443, 363, 584, 215, 584, 215, 443, 196, 515, 127, 493)
	poly(khaki, 196, 144, 258, 113, 324, 113, 383, 144, 445, 488, 386, 507, 357, 373, 357, 578, 221, 578, 221, 373, 191, 507, 133, 488)
	poly(trim, 257, 113, 290, 181, 323, 113, 318, 254, 290, 312, 260, 253)
	poly(light, 252, 118, 290, 197, 290, 330, 238, 217, 252, 195, 238, 175)
	poly(light, 325, 118, 290, 197, 290, 330, 341, 217, 327, 195, 341, 175)
	poly(trim, 222, 407, 274, 407, 274, 433, 222, 433)
	poly(trim, 306, 407, 357, 407, 357, 433, 306, 433)
	poly(light, 223, 408, 273, 408, 273, 421, 223, 421)
	poly(light, 307, 408, 356, 408, 356, 421, 307, 421)
	for _, y := range []float32{351, 397, 443} {
		poly(ink, 286, y, 294, y, 298, y+5, 295, y+11, 286, y+11, 282, y+5)
	}
	poly(color.RGBA{74, 35, 57, 255}, 614, 194, 790, 194, 863, 576, 542, 576)
	poly(color.RGBA{123, 54, 86, 255}, 620, 216, 785, 216, 856, 571, 549, 571)
	poly(color.RGBA{153, 76, 110, 255}, 642, 223, 669, 223, 648, 568, 607, 568)
	poly(color.RGBA{101, 41, 72, 255}, 738, 223, 755, 223, 806, 568, 763, 568)
	var buf bytes.Buffer
	err := png.Encode(&buf, canvas)
	return buf.Bytes(), err
}
