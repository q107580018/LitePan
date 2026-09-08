import { http } from "@/api/client";

interface StartResult { job_id: string }
interface JobResult { status: "pending" | "running" | "succeeded" | "failed"; payload?: unknown; error?: string }

/** Submit a PanSou request without holding the browser/API connection open. */
export async function asyncPanSouSearch(query: Record<string, string>, signal?: AbortSignal): Promise<unknown> {
  const start = await http.post<StartResult>("/public/tools/pansou/search/jobs", query, undefined, signal);
  return pollPanSouJob("/public/tools/pansou/search/jobs", start.job_id, signal);
}

export async function asyncAdminPanSouSearch(query: Record<string, string>, signal?: AbortSignal): Promise<unknown> {
  const start = await http.post<StartResult>("/admin/tools/pansou/search/jobs", query, undefined, signal);
  return pollPanSouJob("/admin/tools/pansou/search/jobs", start.job_id, signal);
}

async function pollPanSouJob(base: string, id: string, signal?: AbortSignal): Promise<unknown> {
  for (;;) {
    const job = await http.get<JobResult>(`${base}/${encodeURIComponent(id)}`);
    if (job.status === "succeeded") return job.payload;
    if (job.status === "failed") throw new Error(job.error || "PanSou 搜索失败");
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(resolve, 800);
      signal?.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); }, { once: true });
    });
  }
}
