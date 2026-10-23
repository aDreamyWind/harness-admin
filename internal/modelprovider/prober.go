package modelprovider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"harness-admin/internal/modeladmin"
)

type Prober struct {
	allowPrivateHosts map[string]struct{}
	resolver          *net.Resolver
}

func NewProber(allowPrivateHosts []string) *Prober {
	allowed := make(map[string]struct{}, len(allowPrivateHosts))
	for _, host := range allowPrivateHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			allowed[host] = struct{}{}
		}
	}
	return &Prober{allowPrivateHosts: allowed, resolver: net.DefaultResolver}
}

func (p *Prober) Probe(ctx context.Context, credential modeladmin.ProviderCredential) (modeladmin.ProbeResult, error) {
	checkedAt := time.Now().UTC()
	startedAt := time.Now()
	_, err := p.discover(ctx, credential, true)
	latency := time.Since(startedAt).Milliseconds()
	result := modeladmin.ProbeResult{Status: "failed", LatencyMS: latency, CheckedAt: checkedAt}
	if err != nil {
		return result, err
	}
	result.Status = "success"
	return result, nil
}

func (p *Prober) clientFor(ctx context.Context, hostname string, timeout time.Duration) (*http.Client, error) {
	hostname = strings.ToLower(hostname)
	addresses, err := p.resolver.LookupIPAddr(ctx, hostname)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("无法解析模型供应商地址")
	}
	_, allowPrivate := p.allowPrivateHosts[hostname]
	allowedIPs := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !allowPrivate && unsafeIP(address.IP) {
			continue
		}
		allowedIPs = append(allowedIPs, address.IP)
	}
	if len(allowedIPs) == 0 {
		return nil, errors.New("模型供应商地址指向受保护网络")
	}

	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			_, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil, errors.New("供应商网络地址格式不正确")
			}
			var lastErr error
			for _, ip := range allowedIPs {
				connection, dialErr := dialer.DialContext(dialCtx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

func failedResult(checkedAt time.Time) modeladmin.ProbeResult {
	return modeladmin.ProbeResult{Status: "failed", CheckedAt: checkedAt}
}

func unsafeIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		value := v4.String()
		if value == "169.254.169.254" {
			return true
		}
		first, _ := strconv.Atoi(strings.Split(value, ".")[0])
		return first == 0 || first >= 224
	}
	return false
}
