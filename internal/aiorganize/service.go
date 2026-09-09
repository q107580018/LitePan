package aiorganize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/httpx"
	"litepan/internal/mediaorganize/recognition"
	"litepan/internal/settings"
)

const (
	promptVersion         = "v2"
	cacheTTL              = 24 * time.Hour
	maxCacheItems         = 256
	maxChunkWorks         = 10
	maxChunkBytes         = 16 * 1024
	maxSampleFilesPerWork = 6
	maxEpisodeChunkFiles  = 20
	maxAdaptiveSplitDepth = 2
)

type cacheEntry struct {
	result    recognition.WorkResult
	expiresAt time.Time
}

type Service struct {
	settings *settings.Service
	http     *http.Client
	log      *slog.Logger

	mu        sync.Mutex
	cache     map[string]cacheEntry
	protocols map[string]modelProtocol
}

func New(settingsSvc *settings.Service) *Service {
	return &Service{
		settings:  settingsSvc,
		log:       slog.Default(),
		http:      httpx.NewClient(httpx.ClientOptions{Timeout: modelRequestTimeout}),
		cache:     make(map[string]cacheEntry),
		protocols: make(map[string]modelProtocol),
	}
}

func (s *Service) SetLogger(log *slog.Logger) {
	if log != nil {
		s.log = log
	}
}

func (s *Service) Available() bool {
	if s == nil || s.settings == nil || !s.settings.Bool(settings.KeyAIOrganizeEnabled) {
		return false
	}
	return validateConfig(s.runtimeConfig(), true) == nil
}

func (s *Service) Enhance(ctx context.Context, req recognition.BatchRequest) (recognition.BatchResult, error) {
	return s.EnhanceWithProgress(ctx, req, nil)
}

func (s *Service) EnhanceWithProgress(
	ctx context.Context,
	req recognition.BatchRequest,
	progress recognition.ProgressFunc,
) (recognition.BatchResult, error) {
	if !s.Available() {
		return recognition.BatchResult{}, recognition.ErrUnavailable
	}
	if len(req.Works) == 0 {
		return recognition.BatchResult{Items: []recognition.WorkResult{}}, nil
	}
	cfg := s.runtimeConfig()
	result := recognition.BatchResult{Items: make([]recognition.WorkResult, 0, len(req.Works))}
	pending := make([]recognition.Work, 0, len(req.Works))

	for _, work := range req.Works {
		key := fingerprint(cfg, work)
		if cached, ok := s.getCached(key); ok {
			cached.WorkID = work.WorkID
			result.Items = append(result.Items, cached)
			result.Cached++
			continue
		}
		pending = append(pending, work)
	}
	chunks := splitWorks(pending)
	reportRecognitionProgress(progress, recognition.BatchProgress{
		Total:       len(req.Works),
		Completed:   result.Cached,
		Cached:      result.Cached,
		TotalChunks: len(chunks),
	})

	var firstChunkErr error
	completedChunks := 0
	processedWorks := 0
	for chunkIndex, chunk := range chunks {
		items, failed, err := s.recognizeChunkAdaptive(ctx, cfg, chunk, 0, func(batchSize, splitDepth int) {
			reportRecognitionProgress(progress, recognition.BatchProgress{
				Total:                 len(req.Works),
				Completed:             result.Cached + processedWorks,
				Cached:                result.Cached,
				Failed:                result.Failed,
				CurrentChunk:          chunkIndex + 1,
				TotalChunks:           len(chunks),
				CurrentBatchSize:      batchSize,
				SplitDepth:            splitDepth,
				AttemptStartedAt:      time.Now().UnixMilli(),
				AttemptTimeoutSeconds: int(modelRequestTimeout / time.Second),
				RetryingSmallerBatch:  splitDepth > 0,
			})
		})
		processedWorks += len(chunk)
		result.Failed += failed
		if err != nil {
			if firstChunkErr == nil {
				firstChunkErr = err
			}
		}
		if len(items) > 0 {
			completedChunks++
		}
		valid := validateResults(chunk, items)
		for _, item := range valid {
			work, ok := findWork(chunk, item.WorkID)
			if !ok {
				continue
			}
			s.putCached(fingerprint(cfg, work), item)
			result.Items = append(result.Items, item)
		}
		reportRecognitionProgress(progress, recognition.BatchProgress{
			Total:        len(req.Works),
			Completed:    result.Cached + processedWorks,
			Cached:       result.Cached,
			Failed:       result.Failed,
			CurrentChunk: chunkIndex + 1,
			TotalChunks:  len(chunks),
		})
	}
	if completedChunks == 0 && len(pending) > 0 && firstChunkErr != nil && len(result.Items) == 0 {
		return recognition.BatchResult{}, firstChunkErr
	}

	sort.SliceStable(result.Items, func(i, j int) bool {
		return workPosition(req.Works, result.Items[i].WorkID) < workPosition(req.Works, result.Items[j].WorkID)
	})
	return result, nil
}

func reportRecognitionProgress(progress recognition.ProgressFunc, state recognition.BatchProgress) {
	if progress != nil {
		progress(state)
	}
}

func (s *Service) Test(ctx context.Context, in UpdateRequest) (testErr error) {
	if s == nil {
		return domain.Errorf(domain.CodeInternal, "AI 辅助增强服务未就绪")
	}
	started := time.Now()
	defer func() {
		logger := s.log
		if logger == nil {
			logger = slog.Default()
		}
		attrs := []any{"duration_ms", time.Since(started).Milliseconds()}
		if testErr != nil {
			attrs = append(attrs, "error", safeDiagnostic(testErr.Error(), in.BaseURL, []string{in.APIKey}, 2048))
			logger.WarnContext(ctx, "AI 连接测试失败", attrs...)
		} else {
			logger.InfoContext(ctx, "AI 连接测试成功", attrs...)
		}
	}()
	// 整个测试（包含协议回退）应在前端 90 秒超时前结束。
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	stored, found := s.storedConfigForTest(in.ID)
	if strings.TrimSpace(in.ID) != "" && !found {
		return domain.Errorf(domain.CodeNotFound, "AI 模型配置已不存在，请刷新后重试")
	}
	cfg := Config{
		BaseURL: strings.TrimSpace(in.BaseURL),
		APIKey:  strings.TrimSpace(in.APIKey),
		Model:   strings.TrimSpace(in.Model),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = stored.BaseURL
	}
	if cfg.Model == "" {
		cfg.Model = stored.Model
	}
	if cfg.APIKey == "" || isAPIKeyMask(cfg.APIKey, stored.APIKey) {
		cfg.APIKey = stored.APIKey
	}
	if err := validateConfig(cfg, true); err != nil {
		return err
	}
	content, err := s.chat(ctx, cfg, []chatMessage{
		{Role: "system", Content: "你是连接测试。只返回 JSON 对象。"},
		{Role: "user", Content: `返回 {"ok":true}。`},
	})
	if err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := decodeJSONObject(content, &out); err != nil || !out.OK {
		return domain.Errorf(domain.CodeDriverError, "模型已响应，但没有按要求返回 JSON")
	}
	return nil
}

func (s *Service) recognizeChunk(ctx context.Context, cfg Config, works []recognition.Work) ([]recognition.WorkResult, error) {
	payload, _ := json.Marshal(struct {
		Works []recognition.Work `json:"works"`
	}{Works: sampleWorks(works)})
	messages := []chatMessage{
		{Role: "system", Content: recognitionSystemPrompt},
		{Role: "user", Content: string(payload)},
	}
	raw, err := s.chat(ctx, cfg, messages)
	if err != nil {
		return nil, err
	}
	items, err := parseRecognitionResponse(raw)
	if err == nil {
		return items, nil
	}

	repair, repairErr := s.chat(ctx, cfg, []chatMessage{
		{Role: "system", Content: recognitionRepairPrompt},
		{Role: "user", Content: raw},
	})
	if repairErr != nil {
		return nil, repairErr
	}
	items, err = parseRecognitionResponse(repair)
	if err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "模型返回格式不正确")
	}
	return items, nil
}

func (s *Service) recognizeChunkAdaptive(
	ctx context.Context,
	cfg Config,
	works []recognition.Work,
	depth int,
	onAttempt func(batchSize, splitDepth int),
) ([]recognition.WorkResult, int, error) {
	if onAttempt != nil {
		onAttempt(len(works), depth)
	}
	items, err := s.recognizeChunk(ctx, cfg, works)
	if err == nil {
		return items, 0, nil
	}
	if !isModelTimeout(err) || len(works) <= 1 || depth >= maxAdaptiveSplitDepth {
		return nil, len(works), err
	}
	middle := len(works) / 2
	leftItems, leftFailed, leftErr := s.recognizeChunkAdaptive(ctx, cfg, works[:middle], depth+1, onAttempt)
	if ctx.Err() != nil {
		return leftItems, leftFailed + len(works[middle:]), ctx.Err()
	}
	rightItems, rightFailed, rightErr := s.recognizeChunkAdaptive(ctx, cfg, works[middle:], depth+1, onAttempt)
	items = append(leftItems, rightItems...)
	if leftErr != nil {
		return items, leftFailed + rightFailed, leftErr
	}
	return items, leftFailed + rightFailed, rightErr
}

func (s *Service) ResolveEpisodes(
	ctx context.Context,
	req recognition.EpisodeRequest,
) ([]recognition.FileResult, error) {
	if !s.Available() || len(req.Files) == 0 {
		return nil, nil
	}
	cfg := s.runtimeConfig()
	chunks := splitEpisodeFiles(req.Files)
	resolved := make([]recognition.FileResult, 0, len(req.Files))
	var firstErr error
	for _, files := range chunks {
		part := req
		part.Files = files
		items, err := s.resolveEpisodeChunkAdaptive(ctx, cfg, part, 0)
		resolved = append(resolved, items...)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			return resolved, ctx.Err()
		}
	}
	return resolved, firstErr
}

func (s *Service) resolveEpisodeChunk(
	ctx context.Context,
	cfg Config,
	req recognition.EpisodeRequest,
) ([]recognition.FileResult, error) {
	payload, _ := json.Marshal(req)
	raw, err := s.chat(ctx, cfg, []chatMessage{
		{Role: "system", Content: episodeSystemPrompt},
		{Role: "user", Content: string(payload)},
	})
	if err != nil {
		return nil, err
	}
	items, err := parseEpisodeResponse(raw)
	if err != nil {
		repair, repairErr := s.chat(ctx, cfg, []chatMessage{
			{Role: "system", Content: episodeRepairPrompt},
			{Role: "user", Content: raw},
		})
		if repairErr != nil {
			return nil, repairErr
		}
		items, err = parseEpisodeResponse(repair)
		if err != nil {
			return nil, domain.Errorf(domain.CodeDriverError, "模型返回的集数格式不正确")
		}
	}
	return validateEpisodeResults(req.Files, items), nil
}

func (s *Service) resolveEpisodeChunkAdaptive(
	ctx context.Context,
	cfg Config,
	req recognition.EpisodeRequest,
	depth int,
) ([]recognition.FileResult, error) {
	items, err := s.resolveEpisodeChunk(ctx, cfg, req)
	if err == nil {
		return items, nil
	}
	if !isModelTimeout(err) || len(req.Files) <= 1 || depth >= maxAdaptiveSplitDepth {
		return nil, err
	}
	middle := len(req.Files) / 2
	left := req
	left.Files = req.Files[:middle]
	leftItems, leftErr := s.resolveEpisodeChunkAdaptive(ctx, cfg, left, depth+1)
	if ctx.Err() != nil {
		return leftItems, ctx.Err()
	}
	right := req
	right.Files = req.Files[middle:]
	rightItems, rightErr := s.resolveEpisodeChunkAdaptive(ctx, cfg, right, depth+1)
	items = append(leftItems, rightItems...)
	if leftErr != nil {
		return items, leftErr
	}
	return items, rightErr
}

func parseEpisodeResponse(raw string) ([]recognition.FileResult, error) {
	var out struct {
		Files []recognition.FileResult `json:"files"`
	}
	if err := decodeJSONObject(raw, &out); err != nil {
		return nil, err
	}
	if out.Files == nil {
		return nil, errors.New("missing files")
	}
	return out.Files, nil
}

func parseRecognitionResponse(raw string) ([]recognition.WorkResult, error) {
	var out struct {
		Items []recognition.WorkResult `json:"items"`
	}
	if err := decodeJSONObject(raw, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		return nil, errors.New("missing items")
	}
	return out.Items, nil
}

func decodeJSONObject(raw string, out any) error {
	text := strings.TrimSpace(raw)
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < start {
		return errors.New("json object not found")
	}
	dec := json.NewDecoder(strings.NewReader(text[start : end+1]))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func splitWorks(works []recognition.Work) [][]recognition.Work {
	chunks := make([][]recognition.Work, 0)
	current := make([]recognition.Work, 0, maxChunkWorks)
	size := 0
	for _, work := range works {
		encoded, _ := json.Marshal(sampleWork(work))
		if len(current) > 0 && (len(current) >= maxChunkWorks || size+len(encoded) > maxChunkBytes) {
			chunks = append(chunks, current)
			current = make([]recognition.Work, 0, maxChunkWorks)
			size = 0
		}
		current = append(current, work)
		size += len(encoded)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func sampleWorks(works []recognition.Work) []recognition.Work {
	out := make([]recognition.Work, len(works))
	for i, work := range works {
		out[i] = sampleWork(work)
	}
	return out
}

func sampleWork(work recognition.Work) recognition.Work {
	if len(work.Files) <= maxSampleFilesPerWork {
		return work
	}
	files := make([]recognition.File, 0, maxSampleFilesPerWork)
	last := len(work.Files) - 1
	for i := 0; i < maxSampleFilesPerWork; i++ {
		index := i * last / (maxSampleFilesPerWork - 1)
		files = append(files, work.Files[index])
	}
	work.Files = files
	return work
}

func splitEpisodeFiles(files []recognition.File) [][]recognition.File {
	chunks := make([][]recognition.File, 0)
	current := make([]recognition.File, 0, maxEpisodeChunkFiles)
	size := 0
	for _, file := range files {
		encoded, _ := json.Marshal(file)
		if len(current) > 0 && (len(current) >= maxEpisodeChunkFiles || size+len(encoded) > maxChunkBytes) {
			chunks = append(chunks, current)
			current = make([]recognition.File, 0, maxEpisodeChunkFiles)
			size = 0
		}
		current = append(current, file)
		size += len(encoded)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func validateResults(works []recognition.Work, items []recognition.WorkResult) []recognition.WorkResult {
	allowedWorks := make(map[string]recognition.Work, len(works))
	for _, work := range works {
		allowedWorks[work.WorkID] = work
	}
	seenWorks := make(map[string]struct{}, len(items))
	out := make([]recognition.WorkResult, 0, len(items))
	for _, item := range items {
		work, ok := allowedWorks[item.WorkID]
		if !ok {
			continue
		}
		if _, duplicate := seenWorks[item.WorkID]; duplicate {
			continue
		}
		seenWorks[item.WorkID] = struct{}{}
		item.Title = strings.TrimSpace(item.Title)
		item.OriginalTitle = strings.TrimSpace(item.OriginalTitle)
		item.MediaType = strings.ToLower(strings.TrimSpace(item.MediaType))
		if !item.Recognized || item.Title == "" || len([]rune(item.Title)) > 200 {
			item = recognition.WorkResult{WorkID: item.WorkID, Recognized: false}
			out = append(out, item)
			continue
		}
		if item.MediaType != "movie" && item.MediaType != "tv" {
			item.MediaType = work.MediaTypeHint
			if item.MediaType != "movie" && item.MediaType != "tv" {
				item.MediaType = "movie"
			}
		}
		if item.Year != nil && (*item.Year < 1870 || *item.Year > time.Now().Year()+3) {
			item.Year = nil
		}
		if item.Season != nil && (*item.Season < 0 || *item.Season > 100) {
			item.Season = nil
		}
		item.Files = nil
		out = append(out, item)
	}
	return out
}

func validateEpisodeResults(
	files []recognition.File,
	items []recognition.FileResult,
) []recognition.FileResult {
	allowed := make(map[string]struct{}, len(files))
	for _, file := range files {
		allowed[file.SourceID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]recognition.FileResult, 0, len(items))
	for _, item := range items {
		if _, ok := allowed[item.SourceID]; !ok || item.Episode == nil || *item.Episode < 0 || *item.Episode > 100000 {
			continue
		}
		if _, duplicate := seen[item.SourceID]; duplicate {
			continue
		}
		seen[item.SourceID] = struct{}{}
		item.Kind = "episode"
		out = append(out, item)
	}
	return out
}

func fingerprint(cfg Config, work recognition.Work) string {
	work.WorkID = ""
	data, _ := json.Marshal(struct {
		Version string           `json:"version"`
		BaseURL string           `json:"base_url"`
		Model   string           `json:"model"`
		Work    recognition.Work `json:"work"`
	}{Version: promptVersion, BaseURL: cfg.BaseURL, Model: cfg.Model, Work: work})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) getCached(key string) (recognition.WorkResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.cache, key)
		return recognition.WorkResult{}, false
	}
	return entry.result, true
}

func (s *Service) putCached(key string, result recognition.WorkResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= maxCacheItems {
		for oldKey, entry := range s.cache {
			if time.Now().After(entry.expiresAt) {
				delete(s.cache, oldKey)
			}
		}
	}
	if len(s.cache) >= maxCacheItems {
		for oldKey := range s.cache {
			delete(s.cache, oldKey)
			break
		}
	}
	s.cache[key] = cacheEntry{result: result, expiresAt: time.Now().Add(cacheTTL)}
}

func findWork(works []recognition.Work, id string) (recognition.Work, bool) {
	for _, work := range works {
		if work.WorkID == id {
			return work, true
		}
	}
	return recognition.Work{}, false
}

func workPosition(works []recognition.Work, id string) int {
	for i, work := range works {
		if work.WorkID == id {
			return i
		}
	}
	return len(works)
}

var _ recognition.Enhancer = (*Service)(nil)
var _ recognition.ProgressEnhancer = (*Service)(nil)
var _ recognition.EpisodeResolver = (*Service)(nil)
