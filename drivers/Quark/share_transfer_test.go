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

func TestQuarkShareSaveDefersRenameWhenConfirmRequired(t *testing.T) {
	listCalls := 0
	renameCalls := 0
	d := &Driver{client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/1/clouddrive/share/sharepage/token":
			return jsonResponse(`{"status":200,"code":0,"data":{"stoken":"secret-stoken"}}`), nil
		case "/1/clouddrive/share/sharepage/detail":
			return jsonResponse(`{"status":200,"code":0,"data":{"list":[{"fid":"file-1","file_name":"a.mkv","size":100,"file_type":1,"share_fid_token":"token-1"}]}}`), nil
		case "/1/clouddrive/file/sort":
			listCalls++
			if listCalls == 1 {
				// 转存前：目标目录只有一个旧文件。
				return jsonResponse(`{"status":200,"code":0,"data":{"list":[{"fid":"old-1","file_name":"old.mkv","file_type":1}]}}`), nil
			}
			// 转存后：目录里新增了 a.mkv。
			return jsonResponse(`{"status":200,"code":0,"data":{"list":[{"fid":"old-1","file_name":"old.mkv","file_type":1},{"fid":"new-1","file_name":"a.mkv","file_type":1}]}}`), nil
		case "/1/clouddrive/share/sharepage/save":
			return jsonResponse(`{"status":200,"code":0,"data":{"task_id":"task-1"}}`), nil
		case "/1/clouddrive/task":
			return jsonResponse(`{"status":200,"code":0,"data":{"status":2}}`), nil
		case "/1/clouddrive/file/rename":
			renameCalls++
			return jsonResponse(`{"status":200,"code":0,"data":{}}`), nil
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
	result, err := d.SaveOfflineShare(context.Background(), driver.OfflineShareSaveRequest{
		Preparation: *preparation, FileIDs: []string{"file-1"}, ParentID: "target-1",
		TargetName: "PanSou 标题", DeferRename: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.RenameDeferred || result.Name != "a.mkv" || strings.Contains(result.Name, "PanSou") {
		t.Fatalf("延迟重命名时应保留原名并回执待确认: %+v", result)
	}
	if !strings.Contains(result.Message, "等待确认重命名") {
		t.Fatalf("延迟重命名应在结果里说明: %+v", result)
	}
	if renameCalls != 0 {
		t.Fatalf("延迟重命名阶段不应调用改名接口，实际 %d 次", renameCalls)
	}

	updates, err := d.RefreshOfflineTasks(context.Background(), []driver.OfflineTaskRef{{
		ProviderTaskID: "task-1", ProviderState: result.ProviderState,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Status != driver.OfflineStatusSuccess {
		t.Fatalf("延迟重命名任务应正常完成: %+v", updates)
	}
	if updates[0].FileID != "new-1" || updates[0].Name != "a.mkv" {
		t.Fatalf("轮询应回填新增项 id 与原始名称: %+v", updates[0])
	}
	if renameCalls != 0 {
		t.Fatalf("等待确认期间不应自动改名，实际 %d 次", renameCalls)
	}
}
