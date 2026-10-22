package mcpsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// GitHub 是受限的 GitHub/Gitee 公开仓库读取器；不接受用户给出的任意请求地址。
type GitHub struct{ client *http.Client }

func NewGitHub() *GitHub {
	allowed := map[string]bool{"api.github.com": true, "codeload.github.com": true, "gitee.com": true}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !allowed[host] || port != "443" {
			return nil, errors.New("仓库平台目标地址不正确")
		}
		addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		for _, item := range addresses {
			if !publicIP(item.IP) {
				continue
			}
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, errors.New("仓库平台不可达或指向受保护网络")
	}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	return &GitHub{client: &http.Client{Timeout: 45 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

func (g *GitHub) Resolve(ctx context.Context, provider, repository, ref string) (string, error) {
	if !repositoryPattern.MatchString(repository) || (provider != "github" && provider != "gitee") {
		return "", errors.New("仓库来源不正确")
	}
	base := "https://api.github.com/repos/" + repository
	if provider == "gitee" {
		base = "https://gitee.com/api/v5/repos/" + repository
	}
	if ref == "" {
		var metadata struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := g.readJSON(ctx, base, &metadata); err != nil {
			return "", fmt.Errorf("读取默认分支失败: %w", err)
		}
		if metadata.DefaultBranch == "" {
			return "", errors.New("仓库未返回默认分支")
		}
		ref = metadata.DefaultBranch
	}
	var sha string
	if provider == "gitee" {
		var commits []struct {
			SHA string `json:"sha"`
		}
		if err := g.readJSON(ctx, base+"/commits?sha="+url.QueryEscape(ref)+"&per_page=1", &commits); err != nil {
			return "", err
		}
		if len(commits) > 0 {
			sha = commits[0].SHA
		}
	} else {
		var commit struct {
			SHA string `json:"sha"`
		}
		if err := g.readJSON(ctx, base+"/commits/"+url.PathEscape(ref), &commit); err != nil {
			return "", err
		}
		sha = commit.SHA
	}
	if !shaPattern.MatchString(sha) {
		return "", errors.New("平台未返回有效提交号")
	}
	return strings.ToLower(sha), nil
}

func (g *GitHub) readJSON(ctx context.Context, endpoint string, target any) error {
	response, err := g.get(ctx, endpoint)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("仓库接口返回 %d（请检查仓库公开状态、版本和网络）", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(target)
}

func (g *GitHub) Fetch(ctx context.Context, provider, repository, sha string) ([]byte, error) {
	if !repositoryPattern.MatchString(repository) || !shaPattern.MatchString(sha) {
		return nil, errors.New("仓库或固定提交号不正确")
	}
	var endpoint string
	switch provider {
	case "github":
		endpoint = "https://codeload.github.com/" + repository + "/tar.gz/" + sha
	case "gitee":
		endpoint = "https://gitee.com/api/v5/repos/" + repository + "/tarball?ref=" + sha
	default:
		return nil, errors.New("不支持的仓库平台")
	}
	response, err := g.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("代码包接口返回 %d（请检查仓库公开状态和服务器网络）", response.StatusCode)
	}
	const maximum = 20 * 1024 * 1024
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maximum {
		return nil, errors.New("代码包超过 20 MiB")
	}
	return contents, nil
}

func (g *GitHub) get(ctx context.Context, endpoint string) (*http.Response, error) {
	for redirects := 0; redirects < 3; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Harness-Admin-MCP-Importer")
		response, err := g.client.Do(req)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 300 || response.StatusCode > 399 {
			return response, nil
		}
		location, err := url.Parse(response.Header.Get("Location"))
		response.Body.Close()
		if err != nil || location == nil || location.Scheme != "https" || location.Host != "gitee.com" ||
			!strings.HasPrefix(endpoint, "https://gitee.com/") || location.User != nil {
			return nil, errors.New("仓库平台返回了不受信任的下载跳转")
		}
		endpoint = location.String()
	}
	return nil, errors.New("仓库平台下载跳转次数过多")
}
