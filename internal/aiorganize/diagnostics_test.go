package aiorganize

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type diagnosticTransport func(*http.Request) (*http.Response, error)

func (f diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type diagnosticBody struct{}

func (diagnosticBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (diagnosticBody) Close() error             { return nil }

func TestDiagnosticTransportKinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind string
		stop bool
	}{
		{"dns", &net.DNSError{Err: "no such host", Name: "example.invalid"}, "dns", false},
		{"tls", tls.RecordHeaderError{Msg: "bad TLS record"}, "tls", false},
		{"refused", syscall.ECONNREFUSED, "connection_refused", false},
		{"timeout", &net.DNSError{Err: "timeout", IsTimeout: true}, "timeout", true},
		{"cancel", context.Canceled, "canceled", true},
		{"deadline", context.DeadlineExceeded, "deadline_exceeded", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelErrorKind(context.Background(), tc.err); got != tc.kind {
				t.Fatalf("kind=%s", got)
			}
			if got := stopModelRetry(context.Background(), tc.err); got != tc.stop {
				t.Fatalf("stop=%v", got)
			}
		})
	}
}

func TestDiagnosticTimeoutAndCancellationDoNotRetry(t *testing.T) {
	for _, failure := range []error{&net.DNSError{Err: "timeout", IsTimeout: true}, context.Canceled, context.DeadlineExceeded} {
		calls := 0
		s := New(nil)
		s.http = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) { calls++; return nil, failure })}
		_, err := s.chat(context.Background(), Config{BaseURL: "https://example.invalid/v1", Model: "test"}, []chatMessage{{Role: "user", Content: "private prompt"}})
		if err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
		if errors.Is(failure, context.Canceled) && !errors.Is(err, context.Canceled) {
			t.Fatal("lost cancellation identity")
		}
		if errors.Is(failure, context.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("lost deadline identity")
		}
	}
}

func TestDiagnosticBodyReadAndLogRedaction(t *testing.T) {
	var logs bytes.Buffer
	calls := 0
	s := New(nil)
	s.log = slog.New(slog.NewJSONHandler(&logs, nil))
	s.http = &http.Client{Transport: diagnosticTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"request-123"}}, Body: diagnosticBody{}, Request: r}, nil
	})}
	endpoint := "https://username:password@example.invalid/v1/responses?secret=querycredential"
	status, _, err := s.executeModelRequest(context.Background(), endpoint, []byte(`{"model":"test-model","input":"private prompt"}`), map[string]string{"Authorization": "Bearer supplied-secret"})
	if err == nil || status != 200 || calls != 2 {
		t.Fatalf("status=%d calls=%d err=%v", status, calls, err)
	}
	for _, want := range []string{"读取模型响应失败", "unexpected EOF", "request-123"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %s in %v", want, err)
		}
	}
	for _, want := range []string{`"protocol":"openai-responses"`, `"model":"test-model"`, `"duration_ms":`, `"status":200`, `"attempt":2`} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing %s in logs", want)
		}
	}
	for _, secret := range []string{"username", "password", "querycredential", "supplied-secret", "private prompt", "Authorization"} {
		if strings.Contains(logs.String()+err.Error(), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
}

func TestDiagnosticHTTPProviderErrorIsBoundedAndRedacted(t *testing.T) {
	endpoint := "https://user:pass@example.invalid/v1/responses?token=query-secret"
	headers := map[string]string{"Authorization": "Bearer api-secret", "X-Api-Key": "other-secret"}
	resp := &http.Response{Header: http.Header{"X-Request-Id": []string{"trace-42"}}}
	data := []byte(`{"error":{"message":"invalid api-secret other-secret https://user:pass@example.invalid/v1/responses?token=query-secret Authorization: Bearer hidden-token"},"input":"private prompt"}`)
	err := modelHTTPDiagnostic(401, data, resp, endpoint, headers)
	for _, secret := range []string{"api-secret", "other-secret", "query-secret", "hidden-token", "private prompt", "user:pass"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked %s: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "trace-42") || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatal(err)
	}
	err = modelHTTPDiagnostic(503, []byte(`<html>private prompt</html>`), resp, endpoint, headers)
	if strings.Contains(err.Error(), "private prompt") {
		t.Fatal(err)
	}
	if got := safeDiagnostic(strings.Repeat("界", 1000), endpoint, nil, 512); len([]rune(got)) != 513 {
		t.Fatalf("unbounded message: %d", len([]rune(got)))
	}
}
