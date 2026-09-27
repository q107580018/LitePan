package pan115open

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/httpx"
)

const (
	webAPIBaseURL = "https://webapi.115.com"

	// maxShareRenameLookups / shareRenameLookupInterval：115 分享转存后目录可见性
	// 可能短暂延迟，重命名前最多重复查找这么多次。
	maxShareRenameLookups     = 3
	shareRenameLookupInterval = time.Second
)

var (
	pan115SharePattern = regexp.MustCompile(`(?i)(?:115|anxia|115cdn)\.com/s/([a-z0-9]+)`)
	pan115CodePattern  = regexp.MustCompile(`(?i)(?:提取码|访问码|密码|passcode|password)\s*[:：=]?\s*([a-z0-9]{1,12})`)
	pan115QueryPattern = regexp.MustCompile(`(?i)[?&](?:pwd|passcode|password)=([a-z0-9]{1,12})`)
)

type webShareEnvelope struct {
	State   bool            `json:"state"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type webShareEntry struct {
	FID    flexString `json:"fid"`
	FileID flexString `json:"file_id"`
	CID    flexString `json:"cid"`
	FC     flexString `json:"fc"`
	Name   string     `json:"n"`
	Size   flexNumber `json:"s"`
}

func (e webShareEntry) id() string {
	for _, value := range []string{string(e.FID), string(e.FileID), string(e.CID)} {
		if value = strings.TrimSpace(value); value != "" && value != "0" {
			return value
		}
	}
	return ""
}

func (e webShareEntry) isDir() bool {
	if fc := strings.TrimSpace(string(e.FC)); fc != "" {
		return fc == "0"
	}
	return strings.TrimSpace(string(e.FID)) == "" && strings.TrimSpace(string(e.FileID)) == ""
}

type webShareSnapshot struct {
	List []webShareEntry `json:"list"`
}

type pan115ShareState struct {
	ShareCode string
	Passcode  string
	Entries   []webShareEntry
}

func (d *Driver) PrepareOfflineShare(ctx context.Context, req driver.OfflineSharePrepareRequest) (*driver.OfflineSharePreparation, error) {
	shareCode, passcode, err := parse115ShareLink(req.Link, req.Passcode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.add.ShareCookie) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "115 分享转存需要在账号配置中填写网页版 Cookie")
	}
	entries, err := d.list115SharedRoot(ctx, shareCode, passcode)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, domain.Errorf(domain.CodeNotFound, "115 分享链接中没有可转存内容")
	}
	files := make([]driver.OfflineShareFile, 0, len(entries))
	var size int64
	for _, entry := range entries {
		id := entry.id()
		if id == "" {
			continue
		}
		files = append(files, driver.OfflineShareFile{
			ID: id, Name: entry.Name, Size: entry.Size.int64(), IsDir: entry.isDir(), Wanted: true,
		})
		size += entry.Size.int64()
	}
	return &driver.OfflineSharePreparation{
		Source: "https://115.com/s/" + shareCode,
		Name:   name115ShareResult(entries), TotalSize: size, Files: files,
		State: pan115ShareState{ShareCode: shareCode, Passcode: passcode, Entries: entries},
	}, nil
}

func (d *Driver) list115SharedRoot(ctx context.Context, shareCode, passcode string) ([]webShareEntry, error) {
	var entries []webShareEntry
	for offset := 0; ; offset += 100 {
		query := url.Values{
			"share_code": {shareCode}, "receive_code": {passcode},
			"offset": {strconv.Itoa(offset)}, "limit": {"100"}, "cid": {""},
		}
		var snapshot webShareSnapshot
		if err := d.webShareRequest(ctx, http.MethodGet, "/share/snap", query, nil, &snapshot); err != nil {
			return nil, err
		}
		entries = append(entries, snapshot.List...)
		if len(snapshot.List) < 100 {
			break
		}
	}
	return entries, nil
}

func (d *Driver) SaveOfflineShare(ctx context.Context, req driver.OfflineShareSaveRequest) (*driver.OfflineShareResult, error) {
	state, ok := req.Preparation.State.(pan115ShareState)
	if !ok {
		return nil, domain.Errorf(domain.CodeValidation, "115 分享解析结果已失效")
	}
	wanted := make(map[string]struct{}, len(req.FileIDs))
	for _, id := range req.FileIDs {
		wanted[id] = struct{}{}
	}
	entries := make([]webShareEntry, 0, len(req.FileIDs))
	ids := make([]string, 0, len(req.FileIDs))
	var size int64
	for _, entry := range state.Entries {
		id := entry.id()
		if _, ok := wanted[id]; !ok {
			continue
		}
		entries = append(entries, entry)
		ids = append(ids, id)
		size += entry.Size.int64()
	}
	if len(ids) == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请至少选择一个 115 分享文件")
	}
	// 与夸克一致：仅支持单个文件/文件夹转存后的自动重命名；
	// 多个顶层项无法对应用户标题时跳过并在结果里提示。
	// 用独立标志而不是 slice 是否为 nil 表达“是否需要重命名”，
	// 避免目标目录为空时（before 为空 → existingIDs 保持 nil）漏掉重命名。
	targetName := strings.TrimSpace(req.TargetName)
	wantRename := targetName != "" && len(entries) == 1
	var renameNote string
	var existingIDs []string
	if targetName != "" && !wantRename {
		renameNote = "；自动重命名已跳过（分享含 " + strconv.Itoa(len(entries)) + " 个顶层文件/文件夹，仅支持单个）"
	}
	if wantRename {
		before, err := d.ListFiles(ctx, req.ParentID)
		if err != nil {
			return nil, err
		}
		for _, item := range before {
			existingIDs = append(existingIDs, item.ID)
		}
	}
	form := url.Values{
		"cid": {d.normalizeParent(req.ParentID)}, "share_code": {state.ShareCode},
		"receive_code": {state.Passcode}, "file_id": {strings.Join(ids, ",")},
	}
	if err := d.webShareRequest(ctx, http.MethodPost, "/share/receive", nil, form, nil); err != nil {
		return nil, err
	}
	result := &driver.OfflineShareResult{
		Name: name115ShareResult(entries), Size: size, Completed: true,
		Message: "115 分享链接转存完成" + renameNote,
	}
	if wantRename {
		if err := d.renameSaved115Share(ctx, req.ParentID, existingIDs, entries[0].Name, targetName); err != nil {
			result.Message += "，但自动重命名失败：" + err.Error()
		} else {
			result.Name = targetName
		}
	}
	return result, nil
}

// renameSaved115Share 在转存后的目标目录里找到新增且名字匹配的顶层项并重命名。
// 115 的 /share/receive 虽为同步接口，但文件在目标目录可见可能短暂延迟，
// 因此对“查找新增项”做短间隔重试；找到后的 RenameFile 被拒（如目标名非法）
// 多为永久性错误，不重试直接返回。
func (d *Driver) renameSaved115Share(ctx context.Context, parentID string, existingIDs []string, originalName, targetName string) error {
	var lastErr error
	for attempt := 0; attempt < maxShareRenameLookups; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(shareRenameLookupInterval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		newID, err := d.findSaved115ShareID(ctx, parentID, existingIDs, originalName)
		if err != nil {
			lastErr = err
			continue
		}
		return d.RenameFile(ctx, newID, targetName)
	}
	return lastErr
}

func (d *Driver) findSaved115ShareID(ctx context.Context, parentID string, existingIDs []string, originalName string) (string, error) {
	items, err := d.ListFiles(ctx, parentID)
	if err != nil {
		return "", err
	}
	existing := make(map[string]struct{}, len(existingIDs))
	for _, id := range existingIDs {
		existing[id] = struct{}{}
	}
	for _, item := range items {
		if _, ok := existing[item.ID]; ok {
			continue
		}
		if item.Name == originalName {
			return item.ID, nil
		}
	}
	return "", domain.Errorf(domain.CodeNotFound, "未找到转存后的文件")
}

func (d *Driver) webShareRequest(ctx context.Context, method, path string, query, form url.Values, out any) error {
	if err := d.beforeCall(ctx); err != nil {
		return err
	}
	rawURL := webAPIBaseURL + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	var body *strings.Reader
	if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	httpx.SetHeaders(req, map[string]string{
		"Cookie": d.add.ShareCookie, "User-Agent": defaultUA,
		"Accept": "application/json, text/plain, */*", "Referer": "https://115.com/",
	})
	if len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, data, err := httpx.Execute(d.client, req, httpx.DefaultReadLimit)
	if err != nil {
		return domain.Wrap(domain.CodeDriverError, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return domain.Errorf(domain.CodeAuthExpired, "115 网页版 Cookie 已失效")
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Errorf(domain.CodeDriverError, "115 分享接口 HTTP %d: %s", resp.StatusCode, httpx.Truncate(data, 300))
	}
	var envelope webShareEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return domain.Errorf(domain.CodeDriverError, "115 分享接口返回非 JSON 内容: %s", httpx.Truncate(data, 300))
	}
	if !envelope.State {
		message := strings.TrimSpace(envelope.Error)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}
		if message == "" {
			message = "分享链接转存失败"
		}
		return domain.Errorf(domain.CodeDriverError, "115 %s", message)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return domain.Errorf(domain.CodeDriverError, "115 分享响应解析失败: %v", err)
		}
	}
	return nil
}

func parse115ShareLink(raw, overridePasscode string) (string, string, error) {
	match := pan115SharePattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return "", "", domain.Errorf(domain.CodeValidation, "不是有效的 115 分享链接")
	}
	passcode := strings.TrimSpace(overridePasscode)
	if passcode == "" {
		if found := pan115CodePattern.FindStringSubmatch(raw); len(found) > 1 {
			passcode = found[1]
		} else if found := pan115QueryPattern.FindStringSubmatch(raw); len(found) > 1 {
			passcode = found[1]
		}
	}
	return match[1], passcode, nil
}

func name115ShareResult(entries []webShareEntry) string {
	if len(entries) == 0 {
		return "分享文件"
	}
	if len(entries) == 1 {
		return entries[0].Name
	}
	return entries[0].Name + " 等 " + strconv.Itoa(len(entries)) + " 项"
}
