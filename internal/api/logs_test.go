package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"litepan/internal/logx"
)

func TestListLogsIncludesAndFiltersDebugEntries(t *testing.T) {
	logs, err := logx.New(logx.Options{
		Dir:           t.TempDir(),
		Level:         "debug",
		DisableStdout: true,
	})
	if err != nil {
		t.Fatalf("logx.New() error = %v", err)
	}
	logs.Storage().Enqueue(logx.Entry{
		Timestamp: "2026-07-19T09:00:00+08:00",
		Level:     logx.LevelDebug,
		Module:    "system",
		Message:   "debug-entry",
	})
	logs.Storage().Enqueue(logx.Entry{
		Timestamp: "2026-07-19T10:00:00+08:00",
		Level:     logx.LevelInfo,
		Module:    "system",
		Message:   "info-entry",
	})
	if err := logs.Close(context.Background()); err != nil {
		t.Fatalf("logs.Close() error = %v", err)
	}

	handler := &Handler{logs: logs}

	all := requestLogEntries(t, handler, "/api/admin/logs")
	if len(all) != 2 || all[0].Level != logx.LevelInfo || all[1].Level != logx.LevelDebug {
		t.Fatalf("all log levels = %v, want [20 10]", logLevels(all))
	}

	debug := requestLogEntries(t, handler, "/api/admin/logs?level=10")
	if len(debug) != 1 || debug[0].Level != logx.LevelDebug {
		t.Fatalf("debug log levels = %v, want [10]", logLevels(debug))
	}
}

func requestLogEntries(t *testing.T, handler *Handler, target string) []logEntryDTO {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.listLogs(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d", target, response.Code)
	}
	var payload struct {
		Data []logEntryDTO `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode GET %s response: %v", target, err)
	}
	return payload.Data
}

func logLevels(entries []logEntryDTO) []int {
	levels := make([]int, 0, len(entries))
	for _, entry := range entries {
		levels = append(levels, entry.Level)
	}
	return levels
}
