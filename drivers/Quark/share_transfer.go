package quark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

const (
	pathShareToken  = "/share/sharepage/token"
	pathShareDetail = "/share/sharepage/detail"
	pathShareSave   = "/share/sharepage/save"
)

var (
	quarkSharePattern = regexp.MustCompile(`(?i)pan\.quark\.cn/s/([a-z0-9]+)`)
	shareCodePattern  = regexp.MustCompile(`(?i)(?:提取码|密码|passcode|password)\s*[:：=]?\s*([a-z0-9]{1,12})`)
	shareQueryPattern = regexp.MustCompile(`(?i)[?&](?:pwd|passcode|password)=([a-z0-9]{1,12})`)
)

type shareTokenData struct {
	Stoken string `json:"stoken"`
}

type shareEntry struct {
	FID           string `json:"fid"`
	FileName      string `json:"file_name"`
	Size          int64  `json:"size"`
	Dir           bool   `json:"dir"`
	FileType      int    `json:"file_type"`
	ShareFIDToken string `json:"share_fid_token"`
}

func (e shareEntry) isDir() bool { return e.Dir || e.FileType == 0 }

type shareDetailData struct {
	List []shareEntry `json:"list"`
}

type shareSaveData struct {
	TaskID string `json:"task_id"`
}

func (d *Driver) OfflineDownloadCapabilities() driver.OfflineDownloadCapabilities {
	return driver.OfflineDownloadCapabilities{
		SupportsShareLinks: true,
		ShareLinkHosts:     []string{"pan.quark.cn"},
		RootTargetAllowed:  true,
	}
}

type quarkRenameState struct {
	ParentID     string   `json:"parent_id"`
	TargetName   string   `json:"target_name"`
	OriginalName string   `json:"original_name"`
	ExistingIDs  []string `json:"existing_ids"`
	Attempts     int      `json:"attempts"`
	// Pending 为真表示目标名等待用户确认（转存完成后弹窗确认），
	// 轮询只负责定位转存后的文件，不自动改名。
	Pending bool `json:"pending,omitempty"`
}

// maxShareRenameAttempts 自动重命名失败后的最大重试次数；重试期间任务保持进行中，
// 只有重命名成功（或耗尽重试）才会把任务标记完成并触发完成钩子。
const maxShareRenameAttempts = 6

type quarkShareState struct {
	PwdID   string
	Stoken  string
	Entries []shareEntry
}

func (d *Driver) PrepareOfflineShare(ctx context.Context, req driver.OfflineSharePrepareRequest) (*driver.OfflineSharePreparation, error) {
	pwdID, passcode, err := parseQuarkShareLink(req.Link, req.Passcode)
	if err != nil {
		return nil, err
	}
	var token shareTokenData
	if _, err := d.apiRequest(ctx, http.MethodPost, pathShareToken, nil, map[string]any{
		"pwd_id": pwdID, "passcode": passcode,
	}, &token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(token.Stoken) == "" {
		return nil, domain.Errorf(domain.CodeDriverError, "夸克分享链接未返回转存令牌")
	}

	entries, err := d.listSharedRoot(ctx, pwdID, token.Stoken)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, domain.Errorf(domain.CodeNotFound, "夸克分享链接中没有可转存内容")
	}
	files := make([]driver.OfflineShareFile, 0, len(entries))
	var size int64
	for _, entry := range entries {
		files = append(files, driver.OfflineShareFile{
			ID: entry.FID, Name: entry.FileName, Size: entry.Size, IsDir: entry.isDir(), Wanted: true,
		})
		size += entry.Size
	}
	return &driver.OfflineSharePreparation{
		Source: "https://pan.quark.cn/s/" + pwdID,
		Name:   shareResultName(entries), TotalSize: size, Files: files,
		State: quarkShareState{PwdID: pwdID, Stoken: token.Stoken, Entries: entries},
	}, nil
}

func (d *Driver) SaveOfflineShare(ctx context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	state, ok := req.Preparation.State.(quarkShareState)
	if !ok {
		return nil, domain.Errorf(domain.CodeValidation, "夸克分享解析结果已失效")
	}
	wanted := make(map[string]struct{}, len(req.FileIDs))
	for _, id := range req.FileIDs {
		wanted[id] = struct{}{}
	}
	entries := make([]shareEntry, 0, len(req.FileIDs))
	fids := make([]string, 0, len(req.FileIDs))
	tokens := make([]string, 0, len(req.FileIDs))
	var size int64
	for _, entry := range state.Entries {
		if _, ok := wanted[entry.FID]; !ok {
			continue
		}
		entries = append(entries, entry)
		fids = append(fids, entry.FID)
		tokens = append(tokens, entry.ShareFIDToken)
		size += entry.Size
	}
	if len(entries) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请至少选择一个夸克分享文件")
	}
	var renameState quarkRenameState
	var renameStateData string
	var renameNote string
	targetName := strings.TrimSpace(req.TargetName)
	switch {
	case targetName == "":
		// 未提供目标名（未开启自动重命名），保持夸克原样保存。
	case len(entries) != 1:
		// 仅支持单个文件/文件夹转存后的自动重命名；多个顶层项无法对应用户标题，跳过并给出提示。
		renameNote = "；自动重命名已跳过（分享含 " + strconv.Itoa(len(entries)) + " 个顶层文件/文件夹，仅支持单个）"
	default:
		before, err := d.ListFiles(ctx, req.ParentID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(before))
		for _, item := range before {
			ids = append(ids, item.ID)
		}
		renameState = quarkRenameState{ParentID: req.ParentID, TargetName: targetName, OriginalName: entries[0].FileName, ExistingIDs: ids, Pending: req.DeferRename}
		if renameState.Pending {
			renameNote = "；已保留网盘原始名称，等待确认重命名"
		}
		data, _ := json.Marshal(renameState)
		renameStateData = string(data)
	}
	var saved shareSaveData
	if _, err := d.apiRequest(ctx, http.MethodPost, pathShareSave, nil, map[string]any{
		"fid_list": fids, "fid_token_list": tokens, "to_pdir_fid": d.normalizeParent(req.ParentID),
		"pwd_id": state.PwdID, "stoken": state.Stoken, "pdir_fid": "0", "scene": "link",
	}, &saved); err != nil {
		return nil, err
	}
	completed := strings.TrimSpace(saved.TaskID) == ""
	message := "夸克分享链接转存完成" + renameNote
	if !completed {
		message = "夸克分享转存任务已提交" + renameNote
	}
	result := &driver.OfflineShareResult{Name: shareResultName(entries), Size: size, ProviderTaskID: saved.TaskID, ProviderState: renameStateData, Completed: completed, Message: message}
	result.RenameDeferred = renameState.Pending
	if completed && renameStateData != "" && !renameState.Pending {
		if err := d.renameSavedShare(ctx, renameState); err != nil {
			result.Message += "，但自动重命名失败：" + err.Error()
		}
	}
	return result, nil
}

func (d *Driver) RefreshOfflineTasks(ctx context.Context, refs []driver.OfflineTaskRef) ([]driver.OfflineTaskUpdate, error) {
	updates := make([]driver.OfflineTaskUpdate, 0, len(refs))
	for _, ref := range refs {
		taskID := strings.TrimSpace(ref.ProviderTaskID)
		if taskID == "" {
			continue
		}
		query := url.Values{}
		query.Set("task_id", taskID)
		query.Set("retry_index", "0")
		var task taskData
		if _, err := d.apiRequest(ctx, http.MethodGet, pathTask, query, nil, &task); err != nil {
			return nil, err
		}
		update := driver.OfflineTaskUpdate{ProviderTaskID: taskID, Progress: 0, Status: driver.OfflineStatusRunning, Message: "夸克正在转存分享内容"}
		switch task.Status {
		case 2:
			// 转存已成功。若开启了自动重命名，把“重命名成功”作为任务完成的前置条件：
			// 重命名成功才置为完成并回填目标名；暂时失败则保持进行中并携带重试次数，
			// 由后续轮询继续尝试；耗尽重试后才以失败提示完成（内容已入盘，钩子仍触发）。
			update.Status = driver.OfflineStatusSuccess
			update.Progress = 100
			update.Message = "夸克分享链接转存完成"
			if ref.ProviderState != "" {
				var st quarkRenameState
				if err := json.Unmarshal([]byte(ref.ProviderState), &st); err == nil && strings.TrimSpace(st.TargetName) != "" {
					if st.Pending {
						// 目标名等待用户确认：这里只定位转存后的文件，确认后由公共层改名。
						// 定位失败时保持任务进行中并重试（代价低），耗尽后按完成处理，
						// 确认重命名时公共层还会再按原始名称在目标目录里找一次。
						id, err := d.findSavedShareID(ctx, st)
						switch {
						case err == nil:
							update.FileID = id
							update.Name = st.OriginalName
							update.Message = "夸克分享链接转存完成；已保留网盘原始名称，等待确认重命名"
						case st.Attempts+1 >= maxShareRenameAttempts:
							update.Message = "夸克分享链接转存完成；已保留网盘原始名称，等待确认重命名"
						default:
							st.Attempts++
							stateData, _ := json.Marshal(st)
							update.Status = driver.OfflineStatusRunning
							update.Progress = 100
							update.Message = fmt.Sprintf("夸克分享内容已转存，正在定位文件（第 %d/%d 次）", st.Attempts, maxShareRenameAttempts)
							update.ProviderState = string(stateData)
						}
					} else if err := d.renameSavedShare(ctx, st); err != nil {
						if st.Attempts+1 >= maxShareRenameAttempts {
							update.Message += "，但自动重命名失败：" + err.Error()
						} else {
							st.Attempts++
							stateData, _ := json.Marshal(st)
							update.Status = driver.OfflineStatusRunning
							update.Progress = 100
							update.Message = fmt.Sprintf("夸克分享内容已转存，自动重命名重试中（第 %d/%d 次）", st.Attempts, maxShareRenameAttempts)
							update.ProviderState = string(stateData)
						}
					} else {
						update.Name = st.TargetName
					}
				}
			}
		case 3:
			update.Status = driver.OfflineStatusFailed
			update.Message = "夸克分享链接转存失败"
			update.Error = update.Message
		}
		updates = append(updates, update)
	}
	return updates, nil
}

// renameSavedShare 在目标目录里找到本次转存新增的顶层项并改名为目标名。
func (d *Driver) renameSavedShare(ctx context.Context, state quarkRenameState) error {
	id, err := d.findSavedShareID(ctx, state)
	if err != nil {
		return err
	}
	return d.RenameFile(ctx, id, state.TargetName)
}

// findSavedShareID 定位本次转存新增的顶层项（跳过转存前已存在的 id）。
func (d *Driver) findSavedShareID(ctx context.Context, state quarkRenameState) (string, error) {
	items, err := d.ListFiles(ctx, state.ParentID)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		for _, id := range state.ExistingIDs {
			if item.ID == id {
				goto next
			}
		}
		if item.Name == state.OriginalName {
			return item.ID, nil
		}
	next:
	}
	return "", domain.Errorf(domain.CodeNotFound, "未找到转存后的文件")
}

func (d *Driver) listSharedRoot(ctx context.Context, pwdID, stoken string) ([]shareEntry, error) {
	var all []shareEntry
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("pwd_id", pwdID)
		query.Set("stoken", stoken)
		query.Set("pdir_fid", "0")
		query.Set("_page", strconv.Itoa(page))
		query.Set("_size", "100")
		query.Set("_fetch_share", "1")
		query.Set("_fetch_total", "1")
		query.Set("_sort", "file_type:asc,updated_at:desc")
		var data shareDetailData
		if _, err := d.apiRequest(ctx, http.MethodGet, pathShareDetail, query, nil, &data); err != nil {
			return nil, err
		}
		for _, entry := range data.List {
			if entry.FID != "" && entry.ShareFIDToken != "" {
				all = append(all, entry)
			}
		}
		if len(data.List) < 100 {
			break
		}
	}
	return all, nil
}

func parseQuarkShareLink(raw, overridePasscode string) (string, string, error) {
	match := quarkSharePattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return "", "", domain.Errorf(domain.CodeValidation, "不是有效的夸克分享链接")
	}
	passcode := strings.TrimSpace(overridePasscode)
	if passcode == "" {
		passcode = passcodeFromShareText(raw)
	}
	return match[1], passcode, nil
}

func passcodeFromShareText(raw string) string {
	if match := shareCodePattern.FindStringSubmatch(raw); len(match) > 1 {
		return match[1]
	}
	if match := shareQueryPattern.FindStringSubmatch(raw); len(match) > 1 {
		return match[1]
	}
	return ""
}

func shareResultName(entries []shareEntry) string {
	if len(entries) == 0 {
		return "分享文件"
	}
	if len(entries) == 1 {
		return entries[0].FileName
	}
	return entries[0].FileName + " 等 " + strconv.Itoa(len(entries)) + " 项"
}
