package quark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"litepan/internal/driver"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestQuarkSharePrepareSaveAndFailedRefresh(t *testing.T) {
	var saveBody map[string]any
	d := &Driver{client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != "sid=test" {
			t.Fatalf("夸克请求未携带账号 Cookie")
		}
		switch req.URL.Path {
		case "/1/clouddrive/share/sharepage/token":
			var tokenBody map[string]any
			if err := json.NewDecoder(req.Body).Decode(&tokenBody); err != nil {
				t.Fatal(err)
			}
			if tokenBody["pwd_id"] != "abc123" || tokenBody["passcode"] != "7788" {
				t.Fatalf("夸克 token 参数不正确: %+v", tokenBody)
			}
			return jsonResponse(`{"status":200,"code":0,"data":{"stoken":"secret-stoken"}}`), nil
		case "/1/clouddrive/share/sharepage/detail":
			if req.URL.Query().Get("pwd_id") != "abc123" || req.URL.Query().Get("stoken") != "secret-stoken" {
				t.Fatalf("分享详情查询参数不正确: %s", req.URL.RawQuery)
			}
			return jsonResponse(`{"status":200,"code":0,"data":{"list":[{"fid":"file-1","file_name":"a.mkv","size":100,"file_type":1,"share_fid_token":"token-1"},{"fid":"dir-1","file_name":"folder","size":0,"file_type":0,"share_fid_token":"token-2"}]}}`), nil
		case "/1/clouddrive/share/sharepage/save":
			if err := json.NewDecoder(req.Body).Decode(&saveBody); err != nil {
				t.Fatal(err)
			}
			return jsonResponse(`{"status":200,"code":0,"data":{"task_id":"task-1"}}`), nil
		case "/1/clouddrive/task":
			return jsonResponse(`{"status":200,"code":0,"data":{"status":3}}`), nil
		default:
			t.Fatalf("unexpected quark request: %s", req.URL.String())
			return nil, nil
		}
	})}}
	d.cookie = "sid=test"

	preparation, err := d.PrepareOfflineShare(context.Background(), driver.OfflineSharePrepareRequest{
		Link: "https://pan.quark.cn/s/abc123", Passcode: "7788",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preparation.Files) != 2 || preparation.Files[0].IsDir || !preparation.Files[1].IsDir {
		t.Fatalf("夸克分享列表解析不正确: %+v", preparation.Files)
	}
	result, err := d.SaveOfflineShare(context.Background(), driver.OfflineShareSaveRequest{
		Preparation: *preparation, FileIDs: []string{"dir-1"}, ParentID: "target-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProviderTaskID != "task-1" || result.Completed {
		t.Fatalf("异步转存结果不正确: %+v", result)
	}
	assertStringSlice(t, saveBody["fid_list"], []string{"dir-1"})
	assertStringSlice(t, saveBody["fid_token_list"], []string{"token-2"})
	if saveBody["to_pdir_fid"] != "target-1" || saveBody["stoken"] != "secret-stoken" {
		t.Fatalf("夸克转存参数不正确: %+v", saveBody)
	}
	updates, err := d.RefreshOfflineTasks(context.Background(), []driver.OfflineTaskRef{{ProviderTaskID: "task-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Status != driver.OfflineStatusFailed || updates[0].Error == "" {
		t.Fatalf("夸克失败任务映射不正确: %+v", updates)
	}
}

func assertStringSlice(t *testing.T, value any, want []string) {
	t.Helper()
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		t.Fatalf("got %#v, want %v", value, want)
	}
	for index := range want {
		if items[index] != want[index] {
			t.Fatalf("got %#v, want %v", value, want)
		}
	}
}

func TestParseQuarkShareLink(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		override string
		wantID   string
		wantCode string
	}{
		{name: "plain url", raw: "https://pan.quark.cn/s/AbC123", wantID: "AbC123"},
		{name: "share text", raw: "资源 https://pan.quark.cn/s/abc123 提取码：9xY2", wantID: "abc123", wantCode: "9xY2"},
		{name: "query", raw: "https://pan.quark.cn/s/abc123?pwd=7788", wantID: "abc123", wantCode: "7788"},
		{name: "override", raw: "https://pan.quark.cn/s/abc123 提取码: 1111", override: "2222", wantID: "abc123", wantCode: "2222"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, code, err := parseQuarkShareLink(tt.raw, tt.override)
			if err != nil {
				t.Fatal(err)
			}
			if id != tt.wantID || code != tt.wantCode {
				t.Fatalf("got (%q, %q), want (%q, %q)", id, code, tt.wantID, tt.wantCode)
			}
		})
	}
}

func TestParseQuarkShareLinkRejectsOtherHost(t *testing.T) {
	if _, _, err := parseQuarkShareLink("https://example.com/s/abc", ""); err == nil {
		t.Fatal("非夸克链接应返回错误")
	}
}
