// PanSou 搜索结果「一键转存」候选账号计算：纯函数 + 账号能力缓存。
//
// 判断规则：
//   - magnet / ed2k 链接 → 走「链接离线下载」：候选 = 原生支持该协议的账号（如 115/光鸭）。
//   - 其余均为各网盘的分享链接 → 走「分享转存」：候选 = 支持分享转存且域名匹配的账号
//     （当前为 115 / 夸克）。云盘分享页不可用离线下载直接抓取，因此不做协议兜底。
import type { Account } from "@/api/types";
import { offlineDownloadApi } from "@/api/offlineDownload";
import type { OfflineDownloadCapabilities } from "@/types/offline-download";
import type { PanSouItem } from "./pansou";

export type PanSouSaveMode = "share" | "url";

/** 一个可接收该资源并转存到自己网盘的账号候选。 */
export interface PanSouSaveCandidate {
  accountId: number;
  accountName: string;
  driverType: string;
  driverCardName?: string;
  driverCardColor?: string;
  driverCardLogo?: string;
  /** share=分享转存；url=链接离线下载（磁力/电驴）。 */
  mode: PanSouSaveMode;
  /** 命中的分享域名或链接协议，用于展示与排查。 */
  match: string;
  /** 转存方式说明，如「夸克分享转存」「磁力链接离线下载」。 */
  reason: string;
}

export function noOfflineCapabilities(): OfflineDownloadCapabilities {
  return {
    supported: true,
    supports_urls: false,
    supports_batch_urls: false,
    supports_torrent: false,
    supports_share_links: false,
    share_link_hosts: [],
    url_schemes: [],
    root_target_allowed: false,
    remote_delete: false,
    builtin_enabled: true,
    builtin_supports_urls: true,
    builtin_url_schemes: ["http", "https", "magnet"],
    builtin_supports_torrent: false,
  };
}

function itemUrlScheme(raw: string): string {
  const matched = /^([a-zA-Z][a-zA-Z0-9+.-]*):/.exec(String(raw || "").trim());
  return matched ? matched[1].toLowerCase() : "";
}

function itemUrlHost(raw: string): string {
  try {
    return new URL(String(raw || "").trim()).hostname.toLowerCase().replace(/^www\./, "");
  } catch {
    return "";
  }
}

function hostMatches(host: string, patterns: string[] | undefined): string {
  if (!host || !patterns?.length) return "";
  for (const raw of patterns) {
    const pattern = String(raw || "").trim().toLowerCase();
    if (!pattern) continue;
    if (host === pattern || host.endsWith(`.${pattern}`)) return host;
  }
  return "";
}

/** 判断账号能力能否接收该结果，能则返回转存方式信息。 */
function accountAccept(item: Pick<PanSouItem, "url">, caps: OfflineDownloadCapabilities): {
  mode: PanSouSaveMode;
  match: string;
} | null {
  if (!caps) return null;
  const scheme = itemUrlScheme(item.url);
  const linkLike = scheme === "magnet" || scheme === "ed2k";
  if (linkLike) {
    if (caps.supports_urls && caps.url_schemes?.some((raw) => String(raw).toLowerCase() === scheme)) {
      return { mode: "url", match: scheme };
    }
    return null;
  }
  const host = itemUrlHost(item.url);
  if (caps.supports_share_links && hostMatches(host, caps.share_link_hosts)) {
    return { mode: "share", match: host };
  }
  return null;
}

/** 生成便于展示的转存方式说明。 */
export function saveModeReason(mode: PanSouSaveMode, match: string): string {
  if (mode === "share") return match ? `「${match}」分享链接转存` : "分享链接转存";
  if (match === "magnet") return "磁力链接离线下载";
  if (match === "ed2k") return "电驴链接离线下载";
  return "链接离线下载";
}

/** 计算某条 PanSou 结果的候选转存账号（已按账号配置顺序返回）。 */
export function pansouSaveCandidates(
  item: Pick<PanSouItem, "url">,
  accounts: Account[] | undefined,
  capsByAccount: Record<number, OfflineDownloadCapabilities>,
): PanSouSaveCandidate[] {
  const candidates: PanSouSaveCandidate[] = [];
  if (!accounts?.length) return candidates;
  for (const account of accounts) {
    if (account.is_active === false) continue;
    const caps = capsByAccount[account.id];
    if (!caps) continue;
    const accepted = accountAccept(item, caps);
    if (!accepted) continue;
    candidates.push({
      accountId: account.id,
      accountName: account.name,
      driverType: account.driver_type,
      driverCardName: account.driver_card_name,
      driverCardColor: account.driver_card_color,
      driverCardLogo: account.driver_card_logo,
      mode: accepted.mode,
      match: accepted.match,
      reason: saveModeReason(accepted.mode, accepted.match),
    });
  }
  return candidates;
}

/** 拉取一组账号的离线能力（失败账号按无能力处理），返回 accountId → 能力。 */
export async function loadPansouAccountCapabilities(
  accounts: Account[] | undefined,
): Promise<Record<number, OfflineDownloadCapabilities>> {
  const result: Record<number, OfflineDownloadCapabilities> = {};
  if (!accounts?.length) return result;
  await Promise.all(
    accounts.map(async (account) => {
      if (account.is_active === false) {
        result[account.id] = noOfflineCapabilities();
        return;
      }
      try {
        result[account.id] = await offlineDownloadApi.capabilities(account.id);
      } catch {
        result[account.id] = noOfflineCapabilities();
      }
    }),
  );
  return result;
}
