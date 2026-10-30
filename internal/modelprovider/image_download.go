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
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("模型返回的图片地址无效")
	}
	if u.Scheme == "http" {
		if _, ok := f.prober.allowPrivateHosts[strings.ToLower(u.Hostname())]; !ok {
			return nil, fmt.Errorf("图片地址必须使用 HTTPS")
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
		return nil, fmt.Errorf("下载模型生成的图片失败: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("图片下载返回状态 %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 20*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 20*1024*1024 {
		return nil, fmt.Errorf("生成的图片超过 20 MB")
	}
	return data, nil
}
