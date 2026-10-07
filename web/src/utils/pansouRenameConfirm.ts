// 「转存完成后确认重命名」的前端编排：
// 转存任务先在网盘里保留原始名称，任务完成后弹窗询问「是否将 xxx 重命名成 PanSou 标题」，
// 用户确认时调用后端改名接口，放弃时让后端清掉待确认状态。
//
// 待确认状态保存在后端任务上，因此这里只负责跟进任务进度与弹窗，不做本地持久化。
import { getApiErrorMessage } from "@/api/client";
import { offlineDownloadApi } from "@/api/offlineDownload";
import { showConfirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import type { OfflineDownloadTask } from "@/types/offline-download";

/** 改名成功后广播，方便正在浏览目标目录的文件列表刷新。 */
export const PANSOU_RENAME_EVENT = "litepan:pansou-renamed";

export interface PansouRenameEventDetail {
  accountId: number;
  parentId: string;
  name: string;
}

/** 轮询间隔：任务还在进行中时用来跟进状态（服务端自带刷新节流）。 */
const RENAME_POLL_MS = 6000;
/** 连续多少次在任务列表里找不到就放弃跟进（任务被删除/清理）。 */
const MAX_MISSES = 3;

interface WatchedTask {
  /** 最近一次看到的任务快照，用于判断是否还需要轮询。 */
  task?: OfflineDownloadTask;
  /** 连续未在任务列表里出现的次数。 */
  misses: number;
}

const watched = new Map<string, WatchedTask>();
/** 同一会话内只自动弹一次，避免每轮轮询都打断用户；放弃确认后可在任务面板手动触发。 */
const prompted = new Set<string>();

let timer: number | undefined;
let polling = false;
/** 弹窗串行化：确认框是全局单例，多个任务同时完成时排队询问。 */
let promptChain: Promise<void> = Promise.resolve();

function isFinished(task: OfflineDownloadTask): boolean {
  return task.status === "success" || task.status === "failed";
}

/** 记录刚创建/刚看到的任务（保存返回或新任务列表片段），并立即处理已完成的待确认任务。 */
export function trackPansouRenameTasks(tasks: OfflineDownloadTask[] | undefined | null): void {
  if (!tasks?.length) return;
  let unfinished = false;
  for (const task of tasks) {
    const id = String(task?.task_id || "");
    if (!id) continue;
    if (!task.rename_pending) {
      watched.delete(id);
      continue;
    }
    watched.set(id, { task, misses: 0 });
    if (!isFinished(task)) unfinished = true;
  }
  void promptReadyTasks(tasks);
  if (unfinished) ensurePolling();
}

/** 用完整任务列表刷新快照（丢已确认或已消失的任务）。 */
export function syncPansouRenameTasks(tasks: OfflineDownloadTask[] | undefined | null): void {
  if (!tasks?.length || !watched.size) return;
  const byId = new Map(tasks.map((task) => [String(task.task_id || ""), task]));
  let unfinished = false;
  for (const [id, entry] of [...watched.entries()]) {
    const task = byId.get(id);
    if (!task) {
      const misses = entry.misses + 1;
      if (misses >= MAX_MISSES) watched.delete(id);
      else watched.set(id, { task: entry.task, misses });
      continue;
    }
    if (!task.rename_pending) {
      watched.delete(id);
      continue;
    }
    watched.set(id, { task, misses: 0 });
    if (!isFinished(task)) unfinished = true;
  }
  if (unfinished) ensurePolling();
}

/** 主动弹窗确认（任务面板里的「重命名」按钮走这里）。 */
export function promptPansouRename(task: OfflineDownloadTask): Promise<void> {
  const next = promptChain.then(() => runPansouRenamePrompt(task));
  promptChain = next.catch(() => undefined);
  return next;
}

async function runPansouRenamePrompt(task: OfflineDownloadTask): Promise<void> {
  const target = String(task.rename_target || "").trim();
  if (!target) {
    await resolvePansouRename(task, false);
    return;
  }
  const origin = String(task.name || "").trim() || String(task.rename_origin || "").trim();
  let result: { action: string } | null = null;
  try {
    result = await showConfirm({
      title: "转存完成，是否重命名？",
      message: `是否将「${origin || "转存内容"}」重命名成「${target}」？`,
      icon: "question",
      danger: false,
      confirmText: "重命名",
      cancelText: "保持原名",
      hint: "选择「保持原名」会保留网盘里的原始名称，之后也可以在文件列表中手动重命名。",
    });
  } catch {
    // 直接关闭弹窗：保持待确认状态，之后可在任务面板里再次确认。
    return;
  }
  await resolvePansouRename(task, result?.action === "confirm");
}

async function resolvePansouRename(task: OfflineDownloadTask, apply: boolean): Promise<void> {
  try {
    const updated = await offlineDownloadApi.confirmRename(task.task_id, apply);
    watched.delete(task.task_id);
    if (apply) {
      const name = String(updated?.name || task.rename_target || "").trim();
      toast.success(name ? `已重命名为「${name}」` : "重命名完成");
      announceRenamed(task, name);
    } else {
      toast.info("已保留网盘原始名称");
    }
  } catch (error) {
    toast.error(
      getApiErrorMessage(error, apply ? "重命名失败，可在文件列表中手动重命名" : "操作失败，请稍后重试"),
    );
  } finally {
    ensurePolling();
  }
}

/** 广播改名结果，供文件浏览器刷新当前目录。 */
function announceRenamed(task: OfflineDownloadTask, name: string): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent<PansouRenameEventDetail>(PANSOU_RENAME_EVENT, {
      detail: {
        accountId: Number(task.account_id || 0),
        parentId: String(task.target_parent_id || ""),
        name,
      },
    }),
  );
}

function ensurePolling(): void {
  if (timer !== undefined) return;
  let unfinished = false;
  for (const entry of watched.values()) {
    if (!entry.task || !isFinished(entry.task)) {
      unfinished = true;
      break;
    }
  }
  if (!unfinished) return;
  timer = window.setTimeout(() => {
    void pollOnce();
  }, RENAME_POLL_MS);
}

async function pollOnce(): Promise<void> {
  timer = undefined;
  if (polling) {
    // 上一轮还在飞行中：稍后再来，避免跟进中断。
    ensurePolling();
    return;
  }
  polling = true;
  try {
    const tasks = await offlineDownloadApi.listTasks(true);
    syncPansouRenameTasks(tasks);
    await promptReadyTasks(tasks);
  } catch {
    // 网络或权限问题：下一轮继续，不影响其它功能。
  } finally {
    polling = false;
    ensurePolling();
  }
}

/** 对已完成且等待确认的任务按顺序弹窗。 */
async function promptReadyTasks(tasks: OfflineDownloadTask[] | undefined): Promise<void> {
  if (!tasks?.length) return;
  for (const task of tasks) {
    if (!task?.rename_pending || task.status !== "success") continue;
    const id = String(task.task_id || "");
    if (!id || prompted.has(id)) continue;
    prompted.add(id);
    await promptPansouRename(task);
  }
}

/** 进入页面时跟进一次遗留的待确认任务（服务端状态，刷新页面后仍会提示）。 */
export async function primePansouRenameWatch(): Promise<void> {
  try {
    const tasks = await offlineDownloadApi.listTasks(false);
    trackPansouRenameTasks(tasks);
    await promptReadyTasks(tasks);
  } catch {
    // 非管理员或未登录：静默忽略。
  }
}

/** 离开文件浏览器时停止跟进，避免后台无谓轮询（服务端待确认状态不受影响）。 */
export function stopPansouRenameWatch(): void {
  if (timer !== undefined) {
    window.clearTimeout(timer);
    timer = undefined;
  }
  watched.clear();
  prompted.clear();
}
