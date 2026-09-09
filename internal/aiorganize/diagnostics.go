package aiorganize

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"litepan/internal/domain"
)

// 仅匹配 ASCII URL 字符，避免把紧随其后的中文标点/文案也吞进 safeEndpoint 而被重新转义。
var diagnosticURL = regexp.MustCompile(`https?://[A-Za-z0-9\-._~:/?#\[\]@!$&'()*+,;=%]+`)
var diagnosticBearer = regexp.MustCompile(`(?i)bearer\s+[^\s,;]+`)
var diagnosticCredential = regexp.MustCompile(`(?i)(bearer\s+|(?:authorization|proxy-authorization|x-api-key|api[_-]?key|token|password)\s*[:=]\s*)[^\s,;]+`)

func diagnosticSecrets(endpoint string, headers map[string]string) []string {
	var secrets []string
	for key, value := range headers {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "auth") || strings.Contains(lower, "key") || strings.Contains(lower, "token") || strings.Contains(lower, "cookie") {
			secrets = append(secrets, value)
			if parts := strings.Fields(value); len(parts) > 1 {
				secrets = append(secrets, parts[1:]...)
			}
		}
	}
	if u, err := url.Parse(endpoint); err == nil {
		if u.User != nil {
			secrets = append(secrets, u.User.Username())
			if p, ok := u.User.Password(); ok {
				secrets = append(secrets, p)
			}
		}
		for _, values := range u.Query() {
			secrets = append(secrets, values...)
		}
	}
	return secrets
}

func safeEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[invalid endpoint]"
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

func safeDiagnostic(text, endpoint string, secrets []string, limit int) string {
	if endpoint != "" {
		text = strings.ReplaceAll(text, endpoint, safeEndpoint(endpoint))
	}
	text = diagnosticURL.ReplaceAllStringFunc(text, safeEndpoint)
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
			text = strings.ReplaceAll(text, url.QueryEscape(secret), "[REDACTED]")
		}
	}
	text = diagnosticBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	text = diagnosticCredential.ReplaceAllString(text, "${1}[REDACTED]")
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		text = string(runes[:limit]) + "…"
	}
	return text
}

func stopModelRetry(ctx context.Context, err error) bool {
	var network net.Error
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout())
}

func modelErrorKind(ctx context.Context, err error) string {
	var dns *net.DNSError
	var network net.Error
	var certificate *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var unknown x509.UnknownAuthorityError
	switch {
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &certificate), errors.As(err, &record), errors.As(err, &unknown):
		return "tls"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	default:
		return "network"
	}
}

// Keep only a sanitized cause in the error chain, while retaining cancellation identity.
func modelRequestError(ctx context.Context, endpoint string, headers map[string]string, resp *http.Response, err error) error {
	stage := "连接模型服务失败"
	if resp != nil {
		stage = "读取模型响应失败"
	}
	secrets := diagnosticSecrets(endpoint, headers)
	detail := safeDiagnostic(err.Error(), endpoint, secrets, 512)
	cause := errors.New(detail)
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		cause = fmt.Errorf("%s: %w", detail, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		cause = fmt.Errorf("%s: %w", detail, context.DeadlineExceeded)
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	kind := modelErrorKind(ctx, err)
	label := map[string]string{
		"canceled": "请求已取消", "deadline_exceeded": "请求超时",
		"timeout": "网络超时", "dns": "DNS 解析失败",
		"tls": "TLS 握手或证书错误", "connection_refused": "连接被拒绝", "network": "网络错误",
	}[kind]
	metadata := "endpoint=" + safeDiagnostic(endpoint, endpoint, secrets, 512)
	if status != 0 {
		metadata += fmt.Sprintf("，HTTP %d", status)
	}
	if id := modelRequestID(resp, endpoint, secrets); id != "" {
		metadata += "，request_id=" + id
	}
	return &domain.AppError{Code: domain.CodeDriverError, Message: fmt.Sprintf("%s：%s（%s）：%s", stage, label, metadata, detail), Err: cause}
}

func modelRequestID(resp *http.Response, endpoint string, secrets []string) string {
	if resp != nil {
		for _, key := range []string{"x-request-id", "request-id", "x-amzn-requestid", "cf-ray"} {
			if value := resp.Header.Get(key); value != "" {
				return safeDiagnostic(value, endpoint, secrets, 128)
			}
		}
	}
	return ""
}

func modelHTTPDiagnostic(status int, data []byte, resp *http.Response, endpoint string, headers map[string]string) error {
	// Only extract the provider's explicit error message; never dump HTML or the response.
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	var message string
	if json.Unmarshal(data, &envelope) == nil {
		var provider struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(envelope.Error, &provider) == nil {
			message = provider.Message
		}
		if message == "" {
			_ = json.Unmarshal(envelope.Error, &message)
		}
		if message == "" {
			message = envelope.Message
		}
	}
	secrets := diagnosticSecrets(endpoint, headers)
	base, _ := domain.AsAppError(modelHTTPError(status))
	metadata := "HTTP " + strconv.Itoa(status) + "，endpoint=" + safeDiagnostic(endpoint, endpoint, secrets, 512)
	if id := modelRequestID(resp, endpoint, secrets); id != "" {
		metadata += "，request_id=" + id
	}
	return domain.Errorf(base.Code, "%s（%s）：%s", base.Message, metadata, safeDiagnostic(message, endpoint, secrets, 512))
}

func (s *Service) logModelAttempt(ctx context.Context, endpoint string, body []byte, headers map[string]string, resp *http.Response, attempt int, started time.Time, err error) {
	var payload struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &payload)
	secrets := diagnosticSecrets(endpoint, headers)
	protocol := "openai"
	path := safeEndpoint(endpoint)
	if strings.HasSuffix(path, "/responses") {
		protocol = "openai-responses"
	} else if strings.HasSuffix(path, "/messages") {
		protocol = "anthropic"
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	attrs := []any{"attempt", attempt + 1, "duration_ms", time.Since(started).Milliseconds(), "status", status, "request_id", modelRequestID(resp, endpoint, secrets), "protocol", protocol, "endpoint", safeDiagnostic(endpoint, endpoint, secrets, 512), "model", safeDiagnostic(payload.Model, endpoint, secrets, 128)}
	logger := s.log
	if logger == nil {
		logger = slog.Default().With("module", "system")
	}
	if err != nil {
		attrs = append(attrs, "error", safeDiagnostic(err.Error(), endpoint, secrets, 1024))
		logger.WarnContext(ctx, "AI 模型请求失败", attrs...)
	} else {
		logger.InfoContext(ctx, "AI 模型请求完成", attrs...)
	}
}
