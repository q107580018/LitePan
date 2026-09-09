package aiorganize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/httpx"
)

const recognitionSystemPrompt = `你是媒体文件识别助手。输入是内置规则无法稳定识别的多个作品组。
只能根据输入中已有的 work_id、目录名、文件名和候选信息判断，不得虚构文件。
只返回 JSON 对象，格式为：
{"items":[{"work_id":"work_1","recognized":true,"title":"中文或常用标题","original_title":"可选原名","year":2024,"media_type":"movie|tv","season":1,"files":[{"source_id":"source_1","episode":1,"kind":"episode|movie|extra"}]}]}
每个 work_id 最多返回一次。无法稳定判断时返回 recognized=false，不要猜。不要返回目标目录、TMDB ID、置信度、文件新名或任何操作。`

const recognitionRepairPrompt = `将下面内容修正为严格 JSON。只返回一个对象，顶层只有 items 数组。items 中只允许 work_id、recognized、title、original_title、year、media_type、season、files；files 中只允许 source_id、episode、kind。不要解释，不要 Markdown 代码块。`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type modelProtocol uint8

const (
	protocolOpenAI modelProtocol = iota + 1
	protocolAnthropic
	protocolOpenAIResponses
)

type anthropicRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    string        `json:"system,omitempty"`
	Messages  []chatMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responsesInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequest struct {
	Model        string                  `json:"model"`
	Instructions string                  `json:"instructions,omitempty"`
	Input        []responsesInputMessage `json:"input"`
}

type responsesResponse struct {
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func (s *Service) chat(ctx context.Context, cfg Config, messages []chatMessage) (string, error) {
	protocols := s.protocolCandidates(cfg)
	var lastErr error
	for index, protocol := range protocols {
		content, status, body, err := s.chatWithProtocol(ctx, cfg, messages, protocol)
		if err == nil {
			s.rememberProtocol(cfg, protocol)
			return content, nil
		}
		lastErr = err
		if stopModelRetry(ctx, err) || index == len(protocols)-1 || !isProtocolMismatch(status, body) {
			return "", err
		}
	}
	return "", lastErr
}

func (s *Service) protocolCandidates(cfg Config) []modelProtocol {
	key := protocolCacheKey(cfg)
	s.mu.Lock()
	remembered := s.protocols[key]
	s.mu.Unlock()
	if remembered != 0 {
		return protocolCandidatesAfter(remembered)
	}
	preferred := protocolOpenAI
	hint := strings.ToLower(cfg.BaseURL + " " + cfg.Model)
	switch {
	case strings.Contains(hint, "/responses") || strings.Contains(hint, "openai-responses"):
		preferred = protocolOpenAIResponses
	case strings.Contains(hint, "anthropic") || strings.Contains(hint, "claude") || strings.Contains(hint, "/messages"):
		preferred = protocolAnthropic
	}
	return protocolCandidatesAfter(preferred)
}

func protocolCandidatesAfter(preferred modelProtocol) []modelProtocol {
	all := []modelProtocol{protocolOpenAI, protocolAnthropic, protocolOpenAIResponses}
	out := make([]modelProtocol, 0, len(all))
	for _, protocol := range all {
		if protocol == preferred {
			out = append(out, protocol)
			break
		}
	}
	for _, protocol := range all {
		if protocol != preferred {
			out = append(out, protocol)
		}
	}
	return out
}

func (s *Service) rememberProtocol(cfg Config, protocol modelProtocol) {
	s.mu.Lock()
	s.protocols[protocolCacheKey(cfg)] = protocol
	s.mu.Unlock()
}

func protocolCacheKey(cfg Config) string {
	return strings.ToLower(strings.TrimSpace(cfg.BaseURL)) + "\x00" + strings.ToLower(strings.TrimSpace(cfg.Model))
}

func (s *Service) chatWithProtocol(
	ctx context.Context,
	cfg Config,
	messages []chatMessage,
	protocol modelProtocol,
) (string, int, []byte, error) {
	switch protocol {
	case protocolAnthropic:
		return s.doAnthropicChat(ctx, cfg, messages)
	case protocolOpenAIResponses:
		return s.doOpenAIResponsesChat(ctx, cfg, messages)
	default:
		content, status, body, err := s.doOpenAIChat(ctx, cfg, messages, true)
		if !stopModelRetry(ctx, err) && status == http.StatusBadRequest && strings.Contains(strings.ToLower(string(body)), "response_format") {
			return s.doOpenAIChat(ctx, cfg, messages, false)
		}
		return content, status, body, err
	}
}

func (s *Service) doOpenAIChat(
	ctx context.Context,
	cfg Config,
	messages []chatMessage,
	jsonMode bool,
) (string, int, []byte, error) {
	endpoint, err := openAIEndpoint(cfg.BaseURL)
	if err != nil {
		return "", 0, nil, err
	}
	payload := chatRequest{Model: cfg.Model, Messages: messages}
	if jsonMode {
		payload.ResponseFormat = map[string]any{"type": "json_object"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, nil, err
	}
	status, data, err := s.executeModelRequest(ctx, endpoint, body, map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	})
	if err != nil {
		return "", status, data, err
	}
	var decoded chatResponse
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Choices) == 0 {
		return "", status, data, domain.Errorf(domain.CodeDriverError, "模型服务返回了无法识别的内容")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", status, data, domain.Errorf(domain.CodeDriverError, "模型没有返回识别结果")
	}
	return content, status, data, nil
}

func (s *Service) doOpenAIResponsesChat(
	ctx context.Context,
	cfg Config,
	messages []chatMessage,
) (string, int, []byte, error) {
	endpoint, err := openAIResponsesEndpoint(cfg.BaseURL)
	if err != nil {
		return "", 0, nil, err
	}
	instructions, input := splitResponsesMessages(messages)
	payload := responsesRequest{Model: cfg.Model, Instructions: instructions, Input: input}
	// Responses API 的兼容网关对 text.format=json_object 的支持并不一致；
	// 提示词已经要求严格 JSON，因此不发送该可选字段，兼容性更好。
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, nil, err
	}
	status, data, err := s.executeModelRequest(ctx, endpoint, body, map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	})
	if err != nil {
		return "", status, data, err
	}
	secrets := diagnosticSecrets(endpoint, map[string]string{"Authorization": "Bearer " + cfg.APIKey})
	var decoded responsesResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", status, nil, domain.Errorf(domain.CodeDriverError, "Responses 服务返回的 JSON 无法解析（endpoint=%s）", safeDiagnostic(endpoint, endpoint, secrets, 512))
	}
	if decoded.Error != nil || (decoded.Status != "" && decoded.Status != "completed") {
		detail := "status=" + decoded.Status
		if decoded.Error != nil {
			detail += ": " + decoded.Error.Message
		}
		if decoded.IncompleteDetails != nil {
			detail += ": " + decoded.IncompleteDetails.Reason
		}
		return "", status, nil, domain.Errorf(domain.CodeDriverError, "Responses 请求未完成：%s", safeDiagnostic(detail, endpoint, secrets, 512))
	}
	for _, item := range decoded.Output {
		for _, block := range item.Content {
			if block.Type == "refusal" {
				return "", status, nil, domain.Errorf(domain.CodeDriverError, "Responses 模型拒绝了识别请求")
			}
		}
	}
	content := strings.TrimSpace(decoded.OutputText)
	if content == "" {
		parts := make([]string, 0)
		for _, item := range decoded.Output {
			for _, block := range item.Content {
				if (block.Type == "output_text" || block.Type == "text") && strings.TrimSpace(block.Text) != "" {
					parts = append(parts, strings.TrimSpace(block.Text))
				}
			}
		}
		content = strings.TrimSpace(strings.Join(parts, "\n"))
	}
	if content == "" {
		return "", status, data, domain.Errorf(domain.CodeDriverError, "模型没有返回识别结果")
	}
	return content, status, data, nil
}

func splitResponsesMessages(messages []chatMessage) (string, []responsesInputMessage) {
	instructions := make([]string, 0, 1)
	input := make([]responsesInputMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			instructions = append(instructions, message.Content)
			continue
		}
		input = append(input, responsesInputMessage{Role: message.Role, Content: message.Content})
	}
	return strings.Join(instructions, "\n\n"), input
}
func (s *Service) doAnthropicChat(
	ctx context.Context,
	cfg Config,
	messages []chatMessage,
) (string, int, []byte, error) {
	endpoint, err := anthropicEndpoint(cfg.BaseURL)
	if err != nil {
		return "", 0, nil, err
	}
	system, input := splitAnthropicMessages(messages)
	body, err := json.Marshal(anthropicRequest{
		Model:     cfg.Model,
		MaxTokens: 4096,
		System:    system,
		Messages:  input,
	})
	if err != nil {
		return "", 0, nil, err
	}
	status, data, err := s.executeModelRequest(ctx, endpoint, body, map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		return "", status, data, err
	}
	var decoded anthropicResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", status, data, domain.Errorf(domain.CodeDriverError, "模型服务返回了无法识别的内容")
	}
	parts := make([]string, 0, len(decoded.Content))
	for _, block := range decoded.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, strings.TrimSpace(block.Text))
		}
	}
	content := strings.Join(parts, "\n")
	if content == "" {
		return "", status, data, domain.Errorf(domain.CodeDriverError, "模型没有返回识别结果")
	}
	return content, status, data, nil
}

func splitAnthropicMessages(messages []chatMessage) (string, []chatMessage) {
	system := make([]string, 0, 1)
	input := make([]chatMessage, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			system = append(system, message.Content)
			continue
		}
		input = append(input, message)
	}
	return strings.Join(system, "\n\n"), input
}

func (s *Service) executeModelRequest(
	ctx context.Context,
	endpoint string,
	body []byte,
	headers map[string]string,
) (int, []byte, error) {
	// 最多尝试 3 次：网络错误仅重试一次；HTTP 429/5xx 可重试两次（网关常见瞬时 503）。
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return 0, nil, modelRequestError(ctx, endpoint, headers, nil, err)
		}
		req.Header.Set("Content-Type", "application/json")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		started := time.Now()
		resp, data, err := httpx.Execute(s.http, req, 4<<20)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		if err != nil {
			diagnostic := modelRequestError(ctx, endpoint, headers, resp, err)
			s.logModelAttempt(ctx, endpoint, body, headers, resp, attempt, started, diagnostic)
			if attempt == 0 && !stopModelRetry(ctx, err) && waitContext(ctx, 350*time.Millisecond) {
				continue
			}
			if ctx.Err() != nil {
				return status, nil, modelRequestError(ctx, endpoint, headers, resp, ctx.Err())
			}
			return status, nil, diagnostic
		}
		var httpErr error
		if status < 200 || status >= 300 {
			httpErr = modelHTTPDiagnostic(status, data, resp, endpoint, headers)
		}
		s.logModelAttempt(ctx, endpoint, body, headers, resp, attempt, started, httpErr)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < 2 && waitContext(ctx, time.Duration(attempt+1)*500*time.Millisecond) {
				continue
			}
			if ctx.Err() != nil {
				return status, nil, modelRequestError(ctx, endpoint, headers, resp, ctx.Err())
			}
		}
		if httpErr != nil {
			return status, data, httpErr
		}
		return resp.StatusCode, data, nil
	}
	return 0, nil, fmt.Errorf("model request failed")
}

func openAIEndpoint(baseURL string) (string, error) {
	u, err := parseModelURL(baseURL)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), "/chat/completions") {
		u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	}
	return u.String(), nil
}

func openAIResponsesEndpoint(baseURL string) (string, error) {
	u, err := parseModelURL(baseURL)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), "/responses") {
		u.Path = strings.TrimRight(u.Path, "/") + "/responses"
	}
	return u.String(), nil
}

func anthropicEndpoint(baseURL string) (string, error) {
	u, err := parseModelURL(baseURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(strings.ToLower(path), "/messages"):
	case strings.HasSuffix(strings.ToLower(path), "/v1"):
		path += "/messages"
	default:
		path += "/v1/messages"
	}
	u.Path = path
	return u.String(), nil
}

func parseModelURL(baseURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, domain.Errorf(domain.CodeValidation, "API 地址无效")
	}
	return u, nil
}

func isProtocolMismatch(status int, body []byte) bool {
	if status == http.StatusOK && len(body) == 0 {
		return false
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusOK {
		return true
	}
	if status != http.StatusBadRequest && status != http.StatusUnauthorized &&
		status != http.StatusForbidden && status != http.StatusUnprocessableEntity {
		return false
	}
	message := strings.ToLower(string(body))
	for _, hint := range []string{"chat/completions", "/v1/messages", "/responses", "x-api-key", "anthropic-version", "unknown endpoint"} {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}

func modelHTTPError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return domain.Errorf(domain.CodePermissionDenied, "API Key 无效或没有模型访问权限")
	case http.StatusNotFound:
		return domain.Errorf(domain.CodeNotFound, "API 地址或模型名称不正确")
	case http.StatusTooManyRequests:
		return domain.Errorf(domain.CodeRateLimited, "模型服务请求过于频繁，请稍后重试")
	default:
		return domain.Errorf(domain.CodeDriverError, "模型服务请求失败（HTTP %d）", status)
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
