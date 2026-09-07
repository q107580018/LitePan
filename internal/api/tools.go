package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"litepan/internal/settings"
)

// panSouClient 带明确超时，避免 PanSou 上游挂起时请求无限等待、前端一直处于加载态。
var panSouClient = &http.Client{Timeout: 10 * time.Second}

// panSouCloudTypes 是 PanSou 服务认可的网盘类型标识（cloud_types 参数），
// 顺序即展示顺序。仅这些值会被透传/展示，避免历史配置里的无效代号把结果搜空。
var panSouCloudTypes = []string{"115", "quark", "baidu", "aliyun", "xunlei", "tianyi", "uc", "pikpak", "magnet", "ed2k", "guangya", "mobile", "123"}

func splitPanSouPlatforms(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	return parts
}

// normalizePanSouPlatforms 只保留 PanSou 认可的网盘类型并统一大小写，
// 无效代号（如 mag/et）会被过滤掉，避免“有配置却搜不到结果”。
func normalizePanSouPlatforms(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	valid := make(map[string]bool, len(panSouCloudTypes))
	for _, code := range panSouCloudTypes {
		valid[code] = true
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		code := strings.ToLower(strings.TrimSpace(token))
		if !valid[code] || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

type panSouConfig struct {
	Enabled   bool     `json:"enabled"`
	Endpoint  string   `json:"endpoint"`
	Username  string   `json:"username"`
	Password  string   `json:"password,omitempty"`
	Token     string   `json:"token,omitempty"`
	Platforms []string `json:"platforms"`
}

func (h *Handler) panSouConfig() panSouConfig {
	if h.settings == nil {
		return panSouConfig{Endpoint: "https://so.252035.xyz"}
	}
	platforms := normalizePanSouPlatforms(splitPanSouPlatforms(h.settings.String(settings.KeyPanSouPlatforms)))
	return panSouConfig{Enabled: h.settings.Bool(settings.KeyPanSouEnabled), Endpoint: h.settings.String(settings.KeyPanSouEndpoint), Username: h.settings.String(settings.KeyPanSouUsername), Password: h.settings.StringAllowEmpty(settings.KeyPanSouPassword), Token: h.settings.StringAllowEmpty(settings.KeyPanSouToken), Platforms: platforms}
}

func (h *Handler) getPanSouConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.panSouConfig()
	cfg.Password, cfg.Token = "", ""
	writeOK(w, map[string]any{"enabled": cfg.Enabled, "endpoint": cfg.Endpoint, "username": cfg.Username, "password_configured": h.settings.StringAllowEmpty(settings.KeyPanSouPassword) != "", "token_configured": h.settings.StringAllowEmpty(settings.KeyPanSouToken) != "", "platforms": cfg.Platforms})
}

func (h *Handler) updatePanSouConfig(w http.ResponseWriter, r *http.Request) {
	var in panSouConfig
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	values := map[string]string{settings.KeyPanSouEnabled: strconv.FormatBool(in.Enabled), settings.KeyPanSouEndpoint: strings.TrimRight(strings.TrimSpace(in.Endpoint), "/"), settings.KeyPanSouUsername: strings.TrimSpace(in.Username), settings.KeyPanSouPlatforms: strings.Join(normalizePanSouPlatforms(in.Platforms), ",")}
	if in.Password != "" {
		values[settings.KeyPanSouPassword] = in.Password
	}
	if in.Token != "" {
		values[settings.KeyPanSouToken] = in.Token
	}
	if err := h.settings.Update(r.Context(), values); err != nil {
		writeErr(w, err)
		return
	}
	h.getPanSouConfig(w, r)
}

// searchPanSou 代理 PanSou 搜索，避免浏览器跨域并统一返回格式。
// 注意：网盘类型过滤用的是 cloud_types 参数；src 只表示 tg/plugin/all 数据来源，
// 传网盘代号会直接导致上游返回空结果。
func (h *Handler) searchPanSou(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, fmt.Errorf("搜索关键词不能为空"))
		return
	}
	cfg := h.panSouConfig()
	if strings.HasSuffix(r.URL.Path, "/public/tools/pansou/search") && !cfg.Enabled {
		writeErr(w, fmt.Errorf("PanSou 资源搜索未启用"))
		return
	}
	adminRequest := strings.HasPrefix(r.URL.Path, "/api/admin/")
	base := ""
	if adminRequest {
		base = strings.TrimRight(strings.TrimSpace(r.URL.Query().Get("endpoint")), "/")
	}
	if base == "" {
		base = strings.TrimRight(cfg.Endpoint, "/")
	}
	if base == "" {
		base = "https://so.252035.xyz"
	}
	// 网盘范围：管理员可用 platforms 参数临时覆盖（用于配置弹窗里的连通性测试），否则用已保存配置。
	platforms := cfg.Platforms
	if adminRequest {
		if override := strings.TrimSpace(r.URL.Query().Get("platforms")); override != "" {
			platforms = normalizePanSouPlatforms(splitPanSouPlatforms(override))
		}
	}
	u, _ := url.Parse(base + "/api/search")
	qs := u.Query()
	qs.Set("kw", q)
	if len(platforms) > 0 {
		qs.Set("cloud_types", strings.Join(platforms, ","))
	}
	u.RawQuery = qs.Encode()
	requestLogger(r.Context()).Info("PanSou 搜索请求", "kw", q, "endpoint", base, "cloud_types", platforms)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	user, pass := "", ""
	if adminRequest {
		user, pass = r.URL.Query().Get("username"), r.URL.Query().Get("password")
	}
	if user == "" {
		user, pass = cfg.Username, cfg.Password
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	} else if basic := strings.TrimSpace(r.URL.Query().Get("basic")); basic != "" {
		if decoded, err := base64.StdEncoding.DecodeString(basic); err == nil {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(decoded))
		}
	}
	if token := strings.TrimSpace(r.Header.Get("X-PanSou-Token")); (adminRequest && (token != "" || strings.TrimSpace(r.URL.Query().Get("token")) != "")) || cfg.Token != "" {
		if token == "" {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
		}
		if token == "" {
			token = cfg.Token
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-API-Key", token)
	}
	var (
		payload    json.RawMessage
		lastStatus int
		lastErr    error
	)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 600 * time.Millisecond):
			case <-r.Context().Done():
				writeErr(w, r.Context().Err())
				return
			}
		}
		resp, err := panSouClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		lastStatus = resp.StatusCode
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			continue
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		// 业务层错误（HTTP 200 但 code != 0，例如被限流）也按失败处理并给出上游文案。
		var brief struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(payload, &brief) == nil && brief.Code != 0 {
			msg := brief.Message
			if msg == "" {
				msg = fmt.Sprintf("PanSou 服务返回错误（code %d）", brief.Code)
			}
			lastErr = fmt.Errorf("%s", msg)
			continue
		}
		requestLogger(r.Context()).Info("PanSou 搜索成功", "kw", q, "bytes", len(payload))
		writeOK(w, payload)
		return
	}
	if lastErr != nil && lastStatus == 0 {
		writeErr(w, lastErr)
		return
	}
	if lastStatus >= 400 {
		writeErr(w, fmt.Errorf("PanSou 服务暂不可用（HTTP %d），请稍后重试", lastStatus))
		return
	}
	if lastErr != nil {
		writeErr(w, lastErr)
		return
	}
	writeErr(w, fmt.Errorf("PanSou 服务暂不可用（HTTP %d），请稍后重试", lastStatus))
}

// get115StrmToolStatus 返回 115 STRM 增强（目录树清单模式）卡片状态。
func (h *Handler) get115StrmToolStatus(w http.ResponseWriter, r *http.Request) {
	if h.strm == nil {
		writeOK(w, map[string]any{"enabled": false, "cache_count": 0, "available": false})
		return
	}
	count, err := h.strm.DirCacheCount(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{
		"enabled":     h.strm.DirCacheEnabled(),
		"cache_count": count,
		"available":   true,
	})
}

// set115StrmToolEnabled 写 115 STRM 增强开关。
func (h *Handler) set115StrmToolEnabled(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if h.settings != nil {
		if err := h.settings.Update(r.Context(), map[string]string{
			settings.KeyStrmTool115TreeEnabled: strconv.FormatBool(in.Enabled),
		}); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeOK(w, map[string]any{"enabled": in.Enabled})
}

// clear115StrmDirCache 清空 pid→路径 缓存；account_id<=0 表示全部账号。
func (h *Handler) clear115StrmDirCache(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AccountID int64 `json:"account_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if h.strm == nil {
		writeOK(w, map[string]any{"removed": 0})
		return
	}
	n, err := h.strm.ClearDirCache(r.Context(), in.AccountID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"removed": n})
}
