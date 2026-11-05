package modelgateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxCallLogPayload = 1024 * 1024

const (
	omittedMedia = "【内嵌媒体已省略】"
	hiddenSecret = "【敏感值已隐藏】"
	logTail      = "\n【调用日志正文已截断】"
)

var (
	logMediaOrURL = regexp.MustCompile(`(?i)https?://[^\s<>"']+|data:[^,\s<>"']{0,160},[^\s<>"']+`)
	logBearer     = regexp.MustCompile(`(?i)\bBearer[ \t]+[-._~+/=A-Za-z0-9]{8,}`)
)

// sanitizeCallPayload 只处理入库副本。媒体和签名信息脱敏不算截断，保留视频任务状态的完整结构。
func sanitizeCallPayload(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return raw, false
	}
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return []byte("【无法以文本记录的正文已省略】"), true
	}
	trimmed := bytes.TrimSpace(raw)
	if isCallLogStream(trimmed) {
		return sanitizeCallLogStream(raw)
	}
	return limitCallLogPayload(sanitizeCallLogData(raw))
}

func sanitizeCallLogData(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw
	}
	switch trimmed[0] {
	case '{', '[', '"':
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil || decoder.Decode(new(any)) != io.EOF {
			// 截断或损坏的 JSON 可能在媒体、凭据中间断开，不能当普通文本落库。
			return []byte(`{"logNote":"JSON 不完整或无法解析，正文已省略"}`)
		}
		value, changed := sanitizeCallLogValue(value, "")
		if !changed {
			return raw
		}
		out, err := json.Marshal(value)
		if err != nil {
			return []byte(`{"logNote":"正文无法安全编码，已省略"}`)
		}
		return out
	default:
		return []byte(sanitizeCallLogText(string(raw)))
	}
}

func sanitizeCallLogValue(value any, key string) (any, bool) {
	changed := false
	switch item := value.(type) {
	case map[string]any:
		for name, member := range item {
			normalized := normalizedCallLogKey(name)
			if sensitiveCallLogKey(normalized) {
				item[name], changed = hiddenSecret, true
				continue
			}
			if _, isText := member.(string); isText && mediaCallLogKey(normalized, key) {
				item[name], changed = omittedMedia, true
				continue
			}
			clean, replaced := sanitizeCallLogValue(member, normalized)
			item[name], changed = clean, changed || replaced
		}
	case []any:
		for i, member := range item {
			clean, replaced := sanitizeCallLogValue(member, key)
			item[i], changed = clean, changed || replaced
		}
	case string:
		if mediaCallLogKey(key, "") {
			return omittedMedia, true
		}
		clean := sanitizeCallLogText(item)
		return clean, clean != item
	}
	return value, changed
}

func normalizedCallLogKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
}

func sensitiveCallLogKey(key string) bool {
	switch key {
	case "authorization", "proxyauthorization", "apikey", "xapikey", "accesstoken", "refreshtoken",
		"securitytoken", "secretkey", "clientsecret", "password", "privatekey", "cookie", "setcookie":
		return true
	default:
		return false
	}
}

func mediaCallLogKey(key, parent string) bool {
	switch key {
	case "b64json", "base64", "imagebase64", "audiobase64", "videobase64", "filebase64", "imagebytes",
		"audiobytes", "videobytes", "filebytes":
		return true
	case "data":
		switch parent {
		case "inlinedata", "audio", "inputaudio", "video", "image", "file":
			return true
		}
	}
	return false
}

func sanitizeCallLogText(value string) string {
	value = logMediaOrURL.ReplaceAllStringFunc(value, func(match string) string {
		if strings.EqualFold(match[:5], "data:") {
			header, _, _ := strings.Cut(match, ",")
			return header + "," + omittedMedia
		}
		return sanitizeCallLogURL(match)
	})
	return logBearer.ReplaceAllString(value, "Bearer "+hiddenSecret)
}

func sanitizeCallLogURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	changed := parsed.User != nil
	parsed.User = nil
	query := strings.Split(parsed.RawQuery, "&")
	kept := query[:0]
	for _, param := range query {
		key, _, _ := strings.Cut(param, "=")
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		normalized := normalizedCallLogKey(key)
		switch {
		case sensitiveCallLogKey(normalized), normalized == "signature", normalized == "sig",
			normalized == "token", normalized == "ossaccesskeyid", normalized == "awsaccesskeyid",
			normalized == "googleaccessid", normalized == "keypairid",
			strings.HasPrefix(normalized, "xosssignature"), strings.HasPrefix(normalized, "xosscredential"),
			strings.HasPrefix(normalized, "xosssecuritytoken"), strings.HasPrefix(normalized, "xamz"),
			strings.HasPrefix(normalized, "xgoog"):
			changed = true
		default:
			kept = append(kept, param)
		}
	}
	if !changed {
		return value
	}
	parsed.RawQuery = strings.Join(kept, "&")
	parsed.ForceQuery = false
	return parsed.String()
}

func isCallLogStream(trimmed []byte) bool {
	line, _, _ := bytes.Cut(trimmed, []byte("\n"))
	for _, prefix := range []string{"data:", "event:", "id:", "retry:", ":"} {
		if bytes.HasPrefix(line, []byte(prefix)) {
			return true
		}
	}
	return false
}

func sanitizeCallLogStream(raw []byte) ([]byte, bool) {
	var out bytes.Buffer
	frameStart := 0
	for offset := 0; offset < len(raw); {
		line, next, ended := bytes.Cut(raw[offset:], []byte("\n"))
		offset = len(raw) - len(next)
		if ended && len(bytes.TrimSpace(line)) != 0 {
			continue
		}
		frame := sanitizeCallLogFrame(raw[frameStart:offset])
		if out.Len()+len(frame) > maxCallLogPayload-len(logTail)-4 {
			out.WriteString("\n\n: 【调用日志正文已截断】\n\n")
			return out.Bytes(), true
		}
		out.Write(frame)
		frameStart = offset
	}
	// 结尾可能只有一个换行，仍然记录已收到的最后一条事件。
	if frameStart < len(raw) {
		frame := sanitizeCallLogFrame(raw[frameStart:])
		if out.Len()+len(frame) > maxCallLogPayload-len(logTail)-4 {
			out.WriteString("\n\n: 【调用日志正文已截断】\n\n")
			return out.Bytes(), true
		}
		out.Write(frame)
	}
	return out.Bytes(), false
}

func sanitizeCallLogFrame(frame []byte) []byte {
	lines := bytes.Split(frame, []byte("\n"))
	var parts [][]byte
	first := -1
	for i, line := range lines {
		if payload, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			if first < 0 {
				first = i
			}
			payload, _ = bytes.CutPrefix(payload, []byte(" "))
			parts = append(parts, bytes.TrimSuffix(payload, []byte("\r")))
		}
	}
	if first < 0 {
		return []byte(sanitizeCallLogText(string(frame)))
	}
	raw := bytes.Join(parts, []byte("\n"))
	if bytes.Equal(bytes.TrimSpace(raw), []byte("[DONE]")) {
		return frame
	}
	safe := sanitizeCallLogData(raw)
	if bytes.Equal(raw, safe) {
		return frame
	}
	var out [][]byte
	for i, line := range lines {
		if i == first {
			for _, part := range bytes.Split(safe, []byte("\n")) {
				out = append(out, append([]byte("data: "), part...))
			}
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			out = append(out, line)
		}
	}
	return bytes.Join(out, []byte("\n"))
}

func limitCallLogPayload(raw []byte) ([]byte, bool) {
	if len(raw) <= maxCallLogPayload {
		return raw, false
	}
	end := maxCallLogPayload - len(logTail)
	for !utf8.RuneStart(raw[end]) {
		end--
	}
	preview := make([]byte, 0, end+len(logTail))
	preview = append(preview, raw[:end]...)
	return append(preview, logTail...), true
}
