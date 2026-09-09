package aiorganize

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"litepan/internal/httpx"
)

// TestLiveResponsesDiagnostic 是针对真实 OpenAI Responses 网关的临时诊断测试，
// 仅在显式设置 LITEPAN_DIAGNOSTIC_KEY 时运行，用于人工复现「测试连接」失败场景：
//
//	LITEPAN_DIAGNOSTIC_URL=https://.../v1/responses \
//	LITEPAN_DIAGNOSTIC_MODEL=gpt-5.5 \
//	LITEPAN_DIAGNOSTIC_KEY=sk-... \
//	go test ./internal/aiorganize -run TestLiveResponsesDiagnostic -v
func TestLiveResponsesDiagnostic(t *testing.T) {
	key := os.Getenv("LITEPAN_DIAGNOSTIC_KEY")
	if key == "" {
		t.Skip("manual diagnostic: set LITEPAN_DIAGNOSTIC_KEY")
	}
	endpoint := os.Getenv("LITEPAN_DIAGNOSTIC_URL")
	if endpoint == "" {
		endpoint = "https://chunfengfast.mentalout.top/v1/responses"
	}
	model := os.Getenv("LITEPAN_DIAGNOSTIC_MODEL")
	if model == "" {
		model = "gpt-5.5"
	}
	s := New(nil)
	base := s.http.Transport
	s.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if err != nil {
			t.Logf("transport error=%v", err)
		}
		if resp != nil && resp.StatusCode >= 400 {
			data, readErr := httpx.ReadLimited(resp.Body, 4096)
			resp.Body.Close()
			if readErr != nil {
				t.Logf("read body error=%v", readErr)
			}
			t.Logf("HTTP %d: %s", resp.StatusCode, data)
			return rawHTTPResponse(resp.StatusCode, string(data)), nil
		}
		return resp, err
	})
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	start := time.Now()
	err := s.Test(ctx, UpdateRequest{BaseURL: endpoint, Model: model, APIKey: key})
	t.Logf("elapsed=%s error=%v", time.Since(start), err)
	if err != nil {
		t.Fail()
	}
}
