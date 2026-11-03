package modelprovider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DownloadImage 复用模型网关的 DNS 固定及私网检查，不跟随跳转。
func (f *GatewayForwarder) DownloadImage(ctx context.Context, raw string) ([]byte, error) {
	return f.downloadMedia(ctx, raw, "图片", 20)
}

func (f *GatewayForwarder) DownloadVideo(ctx context.Context, raw string) ([]byte, error) {
	return f.downloadMedia(ctx, raw, "视频", 100)
}

func (f *GatewayForwarder) downloadMedia(ctx context.Context, raw, kind string, maxMB int) ([]byte, error) {
	limit := maxMB * 1024 * 1024
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("模型返回的%s地址无效", kind)
	}
	if u.Scheme == "http" {
		if _, ok := f.prober.allowPrivateHosts[strings.ToLower(u.Hostname())]; !ok {
			return nil, fmt.Errorf("%s地址必须使用 HTTPS", kind)
		}
	}
	client, err := f.prober.clientFor(ctx, u.Hostname(), 90*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载模型生成的%s失败: %w", kind, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s下载返回状态 %d", kind, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("生成的%s超过 %d MB", kind, maxMB)
	}
	return data, nil
}
