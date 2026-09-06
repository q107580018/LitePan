package pan115open

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
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func Test115SharePrepareAndSaveRequest(t *testing.T) {
	var receiveForm = make(map[string]string)
	d := &Driver{
		add: Addition{ShareCookie: "UID=test-cookie"},
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Cookie") != "UID=test-cookie" {
				t.Fatalf("115 请求未携带网页版 Cookie")
			}
			switch req.URL.Path {
			case "/share/snap":
				if req.URL.Query().Get("share_code") != "abc123" || req.URL.Query().Get("receive_code") != "7788" {
					t.Fatalf("115 分享查询参数不正确: %s", req.URL.RawQuery)
				}
				return jsonResponse(`{"state":true,"data":{"list":[{"fid":"file-1","cid":"parent-1","fc":"1","n":"a.mkv","s":"100"},{"cid":"dir-1","fc":"0","n":"folder","s":"0"}]}}`), nil
			case "/share/receive":
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				for key := range req.Form {
					receiveForm[key] = req.Form.Get(key)
				}
				return jsonResponse(`{"state":true,"data":{}}`), nil
			default:
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
				return nil, nil
			}
		})},
	}
	preparation, err := d.PrepareOfflineShare(context.Background(), driver.OfflineSharePrepareRequest{
		Link: "https://115.com/s/abc123", Passcode: "7788",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preparation.Files) != 2 || preparation.Files[0].IsDir || !preparation.Files[1].IsDir {
		t.Fatalf("115 分享列表解析不正确: %+v", preparation.Files)
	}
	result, err := d.SaveOfflineShare(context.Background(), driver.OfflineShareSaveRequest{
		Preparation: *preparation, FileIDs: []string{"file-1"}, ParentID: "target-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.Size != 100 {
		t.Fatalf("115 转存结果不正确: %+v", result)
	}
	if receiveForm["file_id"] != "file-1" || receiveForm["cid"] != "target-1" || receiveForm["share_code"] != "abc123" || receiveForm["receive_code"] != "7788" {
		t.Fatalf("115 转存表单不正确: %+v", receiveForm)
	}
}

func TestParse115ShareLink(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		override string
		wantID   string
		wantCode string
	}{
		{name: "115", raw: "https://115.com/s/AbC123", wantID: "AbC123"},
		{name: "anxia text", raw: "https://anxia.com/s/a1b2c3 访问码：9xY2", wantID: "a1b2c3", wantCode: "9xY2"},
		{name: "password query", raw: "https://115cdn.com/s/a1b2c3?password=7788", wantID: "a1b2c3", wantCode: "7788"},
		{name: "override", raw: "https://115.com/s/a1b2c3?password=1111", override: "2222", wantID: "a1b2c3", wantCode: "2222"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, code, err := parse115ShareLink(tt.raw, tt.override)
			if err != nil {
				t.Fatal(err)
			}
			if id != tt.wantID || code != tt.wantCode {
				t.Fatalf("got (%q, %q), want (%q, %q)", id, code, tt.wantID, tt.wantCode)
			}
		})
	}
}

func TestWebShareEntryID(t *testing.T) {
	tests := []struct {
		entry webShareEntry
		want  string
	}{
		{entry: webShareEntry{FID: "file-fid", CID: "parent-cid"}, want: "file-fid"},
		{entry: webShareEntry{FileID: "file-id", CID: "parent-cid"}, want: "file-id"},
		{entry: webShareEntry{CID: "folder-cid"}, want: "folder-cid"},
	}
	for _, tt := range tests {
		if got := tt.entry.id(); got != tt.want {
			t.Fatalf("id() = %q, want %q", got, tt.want)
		}
	}
}

func TestWebShareEntryAcceptsStringFC(t *testing.T) {
	var file webShareEntry
	if err := json.Unmarshal([]byte(`{"fid":"file-1","cid":"parent-1","fc":"1","n":"movie.mkv","s":"1024"}`), &file); err != nil {
		t.Fatal(err)
	}
	if file.id() != "file-1" || file.isDir() {
		t.Fatalf("文件响应解析不正确: %+v", file)
	}
	var folder webShareEntry
	if err := json.Unmarshal([]byte(`{"cid":"folder-1","fc":"0","n":"folder"}`), &folder); err != nil {
		t.Fatal(err)
	}
	if folder.id() != "folder-1" || !folder.isDir() {
		t.Fatalf("目录响应解析不正确: %+v", folder)
	}
}

func TestParse115ShareLinkRejectsOtherHost(t *testing.T) {
	if _, _, err := parse115ShareLink("https://example.com/s/abc", ""); err == nil {
		t.Fatal("非 115 链接应返回错误")
	}
}
