package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"harness-admin/internal/drama"
	"harness-admin/internal/modelgateway"
)

type dramaVideoForwarder interface {
	dramaForwarder
	QueryVideoTask(context.Context, modelgateway.Route, string) (*http.Response, error)
	DownloadVideo(context.Context, string) ([]byte, error)
}

type dramaVideoEngine struct {
	gateway   dramaImageGateway
	forwarder dramaVideoForwarder
}

type dramaVideoOutput struct {
	Output struct {
		ID      string `json:"task_id"`
		Status  string `json:"task_status"`
		URL     string `json:"video_url"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"output"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (w *dramaVideoEngine) GenerateSegmentVideo(ctx context.Context, in drama.SegmentVideoRequest) ([]byte, error) {
	if len(in.ImageIDs) != len(in.ReferenceImages) {
		return nil, errors.New("视频参考图与版本记录数量不一致")
	}
	media := make([]map[string]string, 0, len(in.ReferenceImages))
	for _, image := range in.ReferenceImages {
		media = append(media, map[string]string{"type": "reference_image", "url": image})
	}
	input := map[string]any{"prompt": in.Prompt}
	if len(media) > 0 {
		input["media"] = media
	}
	payload := map[string]any{"model": in.ModelCode, "input": input, "parameters": map[string]any{
		"ratio": in.AspectRatio, "duration": in.Duration, "resolution": "720P", "prompt_extend": false, "audio": true,
	}}
	// 先按同一用户和模型校验网关权限，再编码完整媒体请求。
	body, err := json.Marshal(map[string]any{"model": in.ModelCode, "input": map[string]string{"prompt": in.Prompt}})
	if err != nil {
		return nil, err
	}
	prepared, err := w.gateway.PrepareForUserOperation(ctx, in.OrganizationID, in.UserID, body, "video")
	if err != nil {
		return nil, &drama.SegmentVideoFailure{Message: "视频模型不可用，请检查漫剧设置、模型权限或调用限制", Cause: err}
	}
	route := prepared.Route
	older := route.UpstreamModelID == "wan2.7-r2v" || route.UpstreamModelID == "wan2.7-r2v-2026-06-12"
	if route.ProviderKind != "aliyun_bailian" || (!older && route.UpstreamModelID != "wan3.0-video" && route.UpstreamModelID != "wan3.0-video-prime") {
		return nil, &drama.SegmentVideoFailure{Message: "请在漫剧设置中选择万相 2.7 参考生视频（wan2.7-r2v）或万相 3.0", Cause: drama.ErrModelUnavailable}
	}
	if older {
		if in.Duration > 15 || len(media) < 1 || len(media) > 5 || utf8.RuneCountInString(in.Prompt) > 5000 {
			return nil, &drama.SegmentVideoFailure{Message: "万相 2.7 参考生视频要求 2–15 秒、1–5 张参考图、提示词最多 5000 字，请调整片段或切换模型", Cause: drama.ErrInvalidInput}
		}
		delete(payload["parameters"].(map[string]any), "audio")
	}
	payload["model"] = route.UpstreamModelID
	providerBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// 调用记录保留参数和参考图版本，避免重复存放图片二进制。
	for i := range media {
		media[i]["url"] = "image-job:" + in.ImageIDs[i]
	}
	loggedBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	task, err := w.videoCall(ctx, prepared, in.VideoID, "video", loggedBody, func() (*http.Response, error) {
		return w.forwarder.Forward(ctx, route, providerBody, false)
	})
	if err != nil {
		return nil, err
	}
	if task.Output.ID == "" {
		return nil, errors.New("视频供应商未返回任务编号")
	}
	taskID := task.Output.ID
	queryBody, err := json.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		switch task.Output.Status {
		case "SUCCEEDED":
			if task.Output.URL == "" {
				return nil, errors.New("视频供应商未返回视频地址")
			}
			return w.forwarder.DownloadVideo(ctx, task.Output.URL)
		case "PENDING", "RUNNING":
		default:
			return nil, fmt.Errorf("视频任务状态 %s，供应商错误码 %s", task.Output.Status, task.Output.Code)
		}
		select {
		case <-ctx.Done():
			return nil, &drama.SegmentVideoFailure{Message: "等待视频模型超时，请先查看调用记录再重试", Cause: ctx.Err()}
		case <-timer.C:
		}
		task, err = w.videoCall(ctx, prepared, in.VideoID, "video_task_query", queryBody, func() (*http.Response, error) {
			return w.forwarder.QueryVideoTask(ctx, route, taskID)
		})
		if err != nil {
			return nil, err
		}
	}
}

func (w *dramaVideoEngine) videoCall(ctx context.Context, prepared modelgateway.PreparedRequest, videoID, operation string, body []byte, call func() (*http.Response, error)) (dramaVideoOutput, error) {
	start := time.Now()
	response, err := call()
	status, result, message := http.StatusBadGateway, "success", ""
	var raw []byte
	truncated := false
	var task dramaVideoOutput
	if response != nil {
		defer response.Body.Close()
		status = response.StatusCode
		const limit = 1024 * 1024
		var readErr error
		raw, readErr = io.ReadAll(io.LimitReader(response.Body, limit+1))
		err = errors.Join(err, readErr)
		if len(raw) > limit {
			raw, truncated, err = raw[:limit], true, errors.New("视频模型响应超过限制")
		}
	}
	if err == nil {
		if status < 200 || status >= 300 {
			err = fmt.Errorf("视频供应商返回 HTTP %d", status)
			// HTTP 错误也可能带有可识别的供应商错误码。
			if json.Unmarshal(raw, &task) == nil {
				err = videoProviderFailure(task, err)
			}
		} else if err = json.Unmarshal(raw, &task); err == nil &&
			(task.Code != "" || task.Output.Status == "FAILED" || task.Output.Status == "CANCELED" || task.Output.Status == "UNKNOWN") {
			cause := fmt.Errorf("视频供应商返回错误 %s %s %s", task.Code, task.Output.Status, task.Output.Code)
			err = videoProviderFailure(task, cause)
		}
	}
	if err == nil && operation == "video" && task.Output.ID == "" {
		err = errors.New("视频供应商未返回任务编号")
	}
	if err != nil {
		result, message = "failed", err.Error()
	}
	route, caller := prepared.Route, prepared.Caller
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	logErr := w.gateway.RecordCall(logCtx, modelgateway.RecordCallInput{
		OrganizationID: caller.OrganizationID, UserID: caller.UserID, DepartmentID: caller.DepartmentID, AuthType: "user",
		ModelID: route.ModelID, ProviderID: route.ProviderID, ModelCode: route.ModelCode, ModelType: "video", UpstreamModelID: route.UpstreamModelID,
		RequestID: uuid.NewString(), Result: result, HTTPStatus: status, LatencyMS: time.Since(start).Milliseconds(),
		Source: "ai_drama", Operation: operation, SegmentVideoID: videoID, RequestBody: body, ResponseBody: raw, ResponseTruncated: truncated, ErrorMessage: message,
	})
	return task, errors.Join(err, logErr)
}

func videoProviderFailure(task dramaVideoOutput, cause error) error {
	code, detail := task.Output.Code, task.Output.Message
	if code == "" {
		code, detail = task.Code, task.Message
	}
	if !strings.EqualFold(code, "DataInspectionFailed") && !strings.EqualFold(code, "data_inspection_failed") {
		return cause
	}
	message := "百炼内容审核未通过，请检查当前片段的分镜内容和参考图"
	detail = strings.ToLower(detail)
	switch {
	case strings.Contains(detail, "rejected image (input)"), strings.HasPrefix(detail, "input image"):
		message = "输入参考图审核未通过（百炼），请检查或更换当前片段的参考图"
	case strings.HasPrefix(detail, "output data"):
		message = "生成的视频未通过百炼输出审核，请检查当前片段的分镜内容"
	}
	return &drama.SegmentVideoFailure{Message: message, Cause: cause}
}

func (f *dramaMediaFiles) PutVideo(ctx context.Context, id string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := uuid.Parse(id); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 100*1024*1024 || http.DetectContentType(data) != "video/mp4" {
		return errors.New("视频文件为空、超过 100 MB 或不是 MP4 格式")
	}
	if err := os.MkdirAll(f.directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(f.directory, ".video-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err = errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(f.directory, id+".mp4"))
}

func (f *dramaMediaFiles) OpenVideo(ctx context.Context, id string) (io.ReadSeekCloser, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, time.Time{}, drama.ErrNotFound
	}
	file, err := os.Open(filepath.Join(f.directory, id+".mp4"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, drama.ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, time.Time{}, err
	}
	return file, info.ModTime(), nil
}
