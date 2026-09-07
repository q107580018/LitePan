// PanSou（网盘搜索 API）共享解析逻辑：前台搜索面板与后台「影视搜索转存」卡片共用。
//
// 上游（github.com/fish2018/pansou 及其衍生）聚合返回结构不固定：
//   { code, message, data: { total, results?, merged_by_type?: { baidu: [...], quark: [...] } } }
// 本模块统一把各种包装剥掉，收敛成一行一条分享链接（platform + url + 提取码）。

/** PanSou 服务认可的网盘类型，顺序即界面展示/选择顺序。 */
export const PANSOU_CLOUD_TYPES = [
  "quark",
  "115",
  "magnet",
  "baidu",
  "aliyun",
  "xunlei",
  "tianyi",
  "uc",
  "pikpak",
  "ed2k",
  "mobile",
  "guangya",
  "123",
] as const;

export type PanSouCloudType = (typeof PANSOU_CLOUD_TYPES)[number];

export const PANSOU_PLATFORM_LABELS: Record<string, string> = {
  quark: "夸克",
  "115": "115",
  magnet: "磁力",
  baidu: "百度",
  aliyun: "阿里云盘",
  xunlei: "迅雷",
  tianyi: "天翼",
  uc: "UC",
  pikpak: "PikPak",
  ed2k: "电驴",
  mobile: "移动云盘",
  guangya: "光鸭",
  "123": "123",
};

export function pansouPlatformLabel(code: string): string {
  return PANSOU_PLATFORM_LABELS[code] || code || "资源";
}

/** 只保留上游认可的网盘类型（无效代号会被过滤，避免把结果搜空）。 */
export function normalizePanSouTypes(raw: string[] | string | undefined | null): string[] {
  const list = Array.isArray(raw)
    ? raw
    : typeof raw === "string" && raw.trim()
      ? raw.split(/[,;\s]+/)
      : [];
  const seen = new Set<string>();
  const valid = new Set<string>(PANSOU_CLOUD_TYPES as readonly string[]);
  const out: string[] = [];
  for (const token of list) {
    const code = String(token).trim().toLowerCase();
    if (!code || !valid.has(code) || seen.has(code)) continue;
    seen.add(code);
    out.push(code);
  }
  return out;
}

export interface PanSouItem {
  /** 展示用平台名（中文，如“夸克”），直接用于界面。 */
  platform: string;
  /** 原始平台代号（如 quark/baidu），为空表示无法识别，用于按平台分组/排序。 */
  code: string;
  title: string;
  url: string;
  password: string;
  source: string;
}

function pick(...values: unknown[]): string {
  for (const v of values) {
    if (typeof v === "string" && v.trim()) return v.trim();
  }
  return "";
}

function pushLink(item: Record<string, unknown>, platform: string, out: PanSouItem[]) {
  const links = Array.isArray(item.links) ? item.links : [];
  const fallbackTitle = pick(item.note, item.title, item.name, item.work_title);
  const fallbackSource = pick(item.source, item.channel, item.platform);
  if (links.length) {
    for (const link of links) {
      if (!link || typeof link !== "object") continue;
      const record = link as Record<string, unknown>;
      const url = pick(record.url, record.link);
      if (!url) continue;
      const rawType = pick(record.type, platform);
      out.push({
        platform: pansouPlatformLabel(rawType),
        code: rawType,
        title: pick(record.work_title, record.note, fallbackTitle),
        url,
        password: pick(record.password, record.pwd),
        source: fallbackSource,
      });
    }
    return;
  }
  const url = pick(item.url, item.link);
  if (!url) return;
  const rawType = pick(item.type, platform);
  out.push({
    platform: pansouPlatformLabel(rawType),
    code: rawType,
    title: fallbackTitle,
    url,
    password: pick(item.password, item.pwd),
    source: fallbackSource,
  });
}

function walk(node: unknown, platform: string, out: PanSouItem[]) {
  if (node == null) return;
  if (Array.isArray(node)) {
    for (const item of node) walk(item, platform, out);
    return;
  }
  if (typeof node !== "object") return;
  const record = node as Record<string, unknown>;
  if (Array.isArray(record.links) || pick(record.url, record.link)) {
    pushLink(record, platform, out);
    return;
  }
  // { data: { total, results, merged_by_type } } 包装
  const merged = record.merged_by_type;
  if (merged && typeof merged === "object") {
    for (const [key, value] of Object.entries(merged)) walk(value, key, out);
    return;
  }
  const data = record.data;
  if (data && typeof data === "object") {
    walk(data, platform, out);
    return;
  }
  if (Array.isArray(record.results)) {
    walk(record.results, platform, out);
    return;
  }
  // 平台 -> 链接数组 的分组字典
  const keys = Object.keys(record);
  if (keys.some((key) => Array.isArray(record[key]))) {
    for (const [key, value] of Object.entries(record)) walk(value, key, out);
  }
  // 其余情况（如 message / total 标量）直接忽略
}

/** 把上游任意包装解析为一行一条的链接列表（已去重）。 */
export function parsePanSouPayload(payload: unknown): PanSouItem[] {
  const out: PanSouItem[] = [];
  walk(payload, "", out);
  const seen = new Set<string>();
  const result: PanSouItem[] = [];
  for (const item of out) {
    const key = `${item.code || item.platform}\u0000${item.url}`;
    if (seen.has(key)) continue;
    seen.add(key);
    result.push(item);
  }
  return result;
}

/** 取上游响应的 total 字段（可能嵌套在 data 里）。 */
export function panSouTotal(payload: unknown): number | null {
  const read = (v: unknown): number | null => {
    if (v == null) return null;
    if (typeof v === "number") return Number.isFinite(v) ? v : null;
    if (typeof v !== "object") return null;
    const record = v as Record<string, unknown>;
    if (typeof record.total === "number") return Number.isFinite(record.total) ? record.total : null;
    if (record.data && typeof record.data === "object") return read(record.data);
    return null;
  };
  return read(payload);
}
