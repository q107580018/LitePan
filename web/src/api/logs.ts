import { http } from "./client";

export interface LogEntry {
  id: number;
  timestamp: string;
  level: number;
  level_name: string;
  level_emoji: string;
  module: string;
  module_name: string;
  module_color: string;
  message: string;
  details?: Record<string, unknown>;
  account_id?: string;
  driver_name?: string;
}

export interface LogStats {
  total: number;
  by_level: Record<string, number>;
  by_module: Record<string, number>;
  recent_errors: number;
  recent_errors_total: number;
  recent_unacknowledged_errors: number;
  last_recent_error_at?: string;
  last_acknowledged_error_at?: string;
}

export interface LogCleanupResult {
  deleted_files: number;
  retention_days: number;
  mode?: string;
}

export interface LogQuery {
  level?: number;
  module?: string;
  start_time?: string;
  end_time?: string;
  keyword?: string;
  limit?: number;
  offset?: number;
}

// 级别筛选档位：key 对应统计接口 by_level 的键，用于显示各档数量。
export const LOG_LEVELS = [
  { value: "", label: "全部", key: "" },
  { value: 10, label: "调试", key: "DEBUG" },
  { value: 20, label: "信息", key: "INFO" },
  { value: 30, label: "警告", key: "WARNING" },
  { value: 40, label: "错误", key: "ERROR" },
] as const;

export const LOG_MODULE_GROUPS = [
  { value: "", label: "所有模块" },
  { value: "driver", label: "驱动" },
  { value: "file", label: "文件" },
  { value: "cache", label: "缓存" },
  { value: "interface", label: "接口" },
  { value: "system", label: "系统" },
] as const;

export const LOG_PERIODS = [
  { value: "all", label: "全部时间" },
  { value: "today", label: "今天" },
  { value: "24h", label: "近 24 小时" },
  { value: "7d", label: "近 7 天" },
] as const;

export function logsApi() {
  return {
    list: (query?: LogQuery) => http.get<LogEntry[]>("/logs", query as Record<string, string | number | undefined>),
    stats: () => http.get<LogStats>("/logs/stats"),
    ackRecentErrors: () => http.post<LogStats>("/logs/ack-errors"),
    cleanup: () => http.post<LogCleanupResult>("/logs/cleanup"),
    cleanupKeepToday: () => http.post<LogCleanupResult>("/logs/cleanup/keep-today"),
    cleanupAll: () => http.post<LogCleanupResult>("/logs/cleanup/all"),
  };
}
