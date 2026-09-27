package pan115open

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"litepan/internal/driver"
)

func Test115AddOfflineURLsAttachesRenameState(t *testing.T) {
	d := &Driver{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != pathOfflineAddURLs {
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
			}
			return jsonResponse(`{"state":true,"data":[{"state":true,"info_hash":"hash-1","url":"magnet:?xt=urn:btih:abc"}]}`), nil
		})},
	}
	results, err := d.AddOfflineURLs(context.Background(), driver.OfflineURLRequest{
		URLs: []string{"magnet:?xt=urn:btih:abc"}, ParentID: "target-1", FileName: "PanSou 标题",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("离线任务创建结果不正确: %+v", results)
	}
	var state pan115OfflineRenameState
	if err := json.Unmarshal([]byte(results[0].ProviderState), &state); err != nil {
		t.Fatalf("ProviderState 不是合法的重命名状态: %q", results[0].ProviderState)
	}
	if state.TargetName != "PanSou 标题" {
		t.Fatalf("重命名目标名不正确: %+v", state)
	}

	// 多链接提交时无法对应单一名称，不应挂载重命名状态。
	results, err = d.AddOfflineURLs(context.Background(), driver.OfflineURLRequest{
		URLs: []string{"magnet:?xt=urn:btih:abc", "magnet:?xt=urn:btih:def"}, ParentID: "target-1", FileName: "PanSou 标题",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, result := range results {
		if result.ProviderState != "" {
			t.Fatalf("第 %d 个多链接任务不应携带重命名状态: %+v", i, result)
		}
	}
}

func Test115RefreshOfflineTasksRenamesCompletedTask(t *testing.T) {
	var renameForm = make(map[string]string)
	d := &Driver{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case pathOfflineTaskList:
				return jsonResponse(`{"state":true,"data":{"page":1,"page_count":1,"count":1,"tasks":[{"info_hash":"hash-1","name":"orig.mkv","file_id":"file-9","status":2,"percentDone":"100","size":1024}]}}`), nil
			case pathRename:
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				for key := range req.Form {
					renameForm[key] = req.Form.Get(key)
				}
				return jsonResponse(`{"state":true}`), nil
			default:
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
				return nil, nil
			}
		})},
	}
	state, _ := json.Marshal(pan115OfflineRenameState{TargetName: "PanSou 标题"})
	updates, err := d.RefreshOfflineTasks(context.Background(), []driver.OfflineTaskRef{
		{InfoHash: "hash-1", ProviderState: string(state)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("应返回一条任务更新: %+v", updates)
	}
	update := updates[0]
	if update.Status != driver.OfflineStatusSuccess || update.Name != "PanSou 标题" {
		t.Fatalf("任务完成后应回填重命名名称: %+v", update)
	}
	if renameForm["file_id"] != "file-9" || renameForm["file_name"] != "PanSou 标题" {
		t.Fatalf("115 重命名表单不正确: %+v", renameForm)
	}
}

func Test115RefreshOfflineTasksRetriesFailedRename(t *testing.T) {
	d := &Driver{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case pathOfflineTaskList:
				return jsonResponse(`{"state":true,"data":{"page":1,"page_count":1,"count":1,"tasks":[{"info_hash":"hash-1","name":"orig.mkv","file_id":"file-9","status":2,"percentDone":"100","size":1024}]}}`), nil
			case pathRename:
				return jsonResponse(`{"state":false,"code":990001,"message":"重命名失败"}`), nil
			default:
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
				return nil, nil
			}
		})},
	}
	state, _ := json.Marshal(pan115OfflineRenameState{TargetName: "PanSou 标题"})
	updates, err := d.RefreshOfflineTasks(context.Background(), []driver.OfflineTaskRef{
		{InfoHash: "hash-1", ProviderState: string(state)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("应返回一条任务更新: %+v", updates)
	}
	update := updates[0]
	if update.Status != driver.OfflineStatusRunning {
		t.Fatalf("重命名暂失败时应保持进行中并重试: %+v", update)
	}
	if !strings.Contains(update.Message, "自动重命名重试中") {
		t.Fatalf("应提示重试中: %+v", update)
	}
	var next pan115OfflineRenameState
	if err := json.Unmarshal([]byte(update.ProviderState), &next); err != nil || next.Attempts != 1 {
		t.Fatalf("重试状态应携带递增后的次数: %+v", update.ProviderState)
	}
}

func Test115AddOfflineURLsRecoversExistingTaskWithRenameState(t *testing.T) {
	d := &Driver{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case pathOfflineAddURLs:
				// 重复提交同一磁力：115 拒绝新增，返回失败。
				return jsonResponse(`{"state":true,"data":[{"state":false,"info_hash":"","url":"magnet:?xt=urn:btih:abc","message":"任务已存在"}]}`), nil
			case pathOfflineTaskList:
				// 任务列表中恢复出 info_hash。
				return jsonResponse(`{"state":true,"data":{"page":1,"page_count":1,"count":1,"tasks":[{"info_hash":"hash-1","name":"orig.mkv","file_id":"","status":1,"percentDone":"40","size":1024,"url":"magnet:?xt=urn:btih:abc"}]}}`), nil
			default:
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
				return nil, nil
			}
		})},
	}
	results, err := d.AddOfflineURLs(context.Background(), driver.OfflineURLRequest{
		URLs: []string{"magnet:?xt=urn:btih:abc"}, ParentID: "target-1", FileName: "PanSou 标题",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("应返回一条任务结果: %+v", results)
	}
	if !results[0].Success || results[0].InfoHash != "hash-1" {
		t.Fatalf("恢复出的既有任务应标记成功以便继续跟踪: %+v", results[0])
	}
	var state pan115OfflineRenameState
	if err := json.Unmarshal([]byte(results[0].ProviderState), &state); err != nil || state.TargetName != "PanSou 标题" {
		t.Fatalf("恢复路径也应携带重命名状态: %+v", results[0].ProviderState)
	}
}

func Test115RefreshOfflineTasksGivesUpRenameAfterMaxAttempts(t *testing.T) {
	renameCalls := 0
	d := &Driver{
		client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case pathOfflineTaskList:
				return jsonResponse(`{"state":true,"data":{"page":1,"page_count":1,"count":1,"tasks":[{"info_hash":"hash-1","name":"orig.mkv","file_id":"file-9","status":2,"percentDone":"100","size":1024}]}}`), nil
			case pathRename:
				renameCalls++
				return jsonResponse(`{"state":false,"code":990001,"message":"重命名失败"}`), nil
			default:
				t.Fatalf("unexpected 115 request: %s", req.URL.String())
				return nil, nil
			}
		})},
	}
	state, _ := json.Marshal(pan115OfflineRenameState{TargetName: "PanSou 标题", Attempts: maxOfflineRenameAttempts - 1})
	updates, err := d.RefreshOfflineTasks(context.Background(), []driver.OfflineTaskRef{
		{InfoHash: "hash-1", ProviderState: string(state)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("应返回一条任务更新: %+v", updates)
	}
	update := updates[0]
	if update.Status != driver.OfflineStatusSuccess {
		t.Fatalf("重试耗尽后应以完成收尾（文件已入盘）: %+v", update)
	}
	if !strings.Contains(update.Message, "自动重命名失败") {
		t.Fatalf("耗尽后应提示重命名失败: %+v", update)
	}
	if update.Name != "orig.mkv" {
		t.Fatalf("耗尽后任务名应保持 115 原始文件名，不回填目标名: %+v", update)
	}
	if update.ProviderState != "" {
		t.Fatalf("耗尽后不应再携带重试状态: %+v", update)
	}
	if renameCalls != 1 {
		t.Fatalf("耗尽后本次只应尝试重命名一次，实际 %d 次", renameCalls)
	}
}
