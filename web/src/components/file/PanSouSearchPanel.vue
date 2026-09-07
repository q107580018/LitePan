<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { http, getApiErrorMessage } from "@/api/client";
import AppButton from "@/components/base/AppButton.vue";
import { copyTextToClipboard, toast } from "@/composables/useToast";
import {
  PANSOU_CLOUD_TYPES,
  pansouPlatformLabel,
  panSouTotal,
  parsePanSouPayload,
  type PanSouItem,
} from "@/utils/pansou";

/** “全部”标签页的特殊代号（平台代号不会与它冲突，因为平台代号不会为空字符串且已知代号为全小写）。 */
const ALL_TAB = "__all__";

interface PlatformGroup {
  code: string;
  label: string;
  items: PanSouItem[];
}

const enabled = ref(false);
const q = ref("");
const loading = ref(false);
const searched = ref(false);
const results = ref<PanSouItem[]>([]);
const total = ref<number | null>(null);
const errorMsg = ref("");

onMounted(async () => {
  try {
    const cfg: any = await http.get("/public/system-config");
    enabled.value = Boolean((cfg as any)?.pansou_enabled);
  } catch {
    /* 首页其他功能不受影响 */
  }
});

const activeTab = ref<string>(ALL_TAB);

/** 按平台分组：已知平台按 PANSOU_CLOUD_TYPES 顺序，未知平台排后面，组内保持原有顺序。 */
const platformGroups = computed<PlatformGroup[]>(() => {
  const orderIndex = new Map<string, number>(PANSOU_CLOUD_TYPES.map((code, i) => [code, i]));
  const byCode = new Map<string, PlatformGroup>();
  for (const item of results.value) {
    const code = item.code || "";
    let group = byCode.get(code);
    if (!group) {
      group = {
        code,
        label: item.platform || pansouPlatformLabel(code),
        items: [],
      };
      byCode.set(code, group);
    }
    group.items.push(item);
  }
  const groups = [...byCode.values()];
  groups.sort((a, b) => {
    const ia = orderIndex.get(a.code);
    const ib = orderIndex.get(b.code);
    if (ia != null && ib != null) return ia - ib;
    if (ia != null) return -1;
    if (ib != null) return 1;
    return a.label.localeCompare(b.label, "zh-Hans-CN");
  });
  return groups;
});

/** 每页条数：超过 10 条结果即启用分页。 */
const PAGE_SIZE = 10;
const currentPage = ref(1);

/** 当前标签页（全部 / 某平台）对应的完整结果，最长取前 200 条。 */
const tabItems = computed<PanSouItem[]>(() => {
  if (activeTab.value === ALL_TAB) return results.value.slice(0, 200);
  const group = platformGroups.value.find((g) => g.code === activeTab.value);
  return group ? group.items.slice(0, 200) : [];
});

const totalPages = computed(() => Math.max(1, Math.ceil(tabItems.value.length / PAGE_SIZE)));

/** 实际生效页码：自动钳制到合法区间，切平台/换词后不会残留越界页码。 */
const page = computed(() => Math.min(currentPage.value, totalPages.value));

/** 当前页展示的条目。 */
const pageItems = computed<PanSouItem[]>(() => {
  const start = (page.value - 1) * PAGE_SIZE;
  return tabItems.value.slice(start, start + PAGE_SIZE);
});

const tabLabel = computed(() => {
  if (activeTab.value === ALL_TAB) return "全部平台";
  const group = platformGroups.value.find((g) => g.code === activeTab.value);
  return group?.label ?? "全部平台";
});

function goToPage(next: number) {
  currentPage.value = Math.min(Math.max(1, next), totalPages.value);
}

function selectTab(code: string) {
  if (activeTab.value === code) return;
  activeTab.value = code;
  currentPage.value = 1;
}

function pageNumbers(): (number | null)[] {
  const total = totalPages.value;
  if (total <= 7) {
    const out: number[] = [];
    for (let i = 1; i <= total; i++) out.push(i);
    return out;
  }
  const cur = page.value;
  const wanted = [...new Set([1, total, cur - 1, cur, cur + 1])]
    .filter((p) => p >= 1 && p <= total)
    .sort((a, b) => a - b);
  const out: (number | null)[] = [];
  let prev = 0;
  for (const p of wanted) {
    if (p - prev > 1) out.push(null);
    out.push(p);
    prev = p;
  }
  return out;
}

/** 收起状态：标题栏保留，下方搜索/结果整体隐藏，给网盘目录腾空间。 */
const collapsed = ref(false);

function toggleCollapsed() {
  collapsed.value = !collapsed.value;
}

const collapseHint = computed(() => {
  if (!collapsed.value) return "收起";
  return results.value.length ? `展开（${results.value.length} 条结果）` : "展开";
});

async function search() {
  const keyword = q.value.trim();
  if (!keyword || loading.value) return;
  loading.value = true;
  searched.value = true;
  errorMsg.value = "";
  results.value = [];
  total.value = null;
  activeTab.value = ALL_TAB;
  currentPage.value = 1;
  try {
    const payload: any = await http.get("/public/tools/pansou/search", { q: keyword });
    total.value = panSouTotal(payload);
    results.value = parsePanSouPayload(payload);
  } catch (e) {
    errorMsg.value = getApiErrorMessage(e, "资源搜索失败，请稍后重试");
    toast.error(errorMsg.value);
  } finally {
    loading.value = false;
  }
}

async function copyUrl(item: PanSouItem) {
  if (!item.url) return;
  const ok = await copyTextToClipboard(item.url);
  if (ok) toast.success("分享链接已复制，可在离线下载 / 分享转存中提交");
}

async function copyPassword(item: PanSouItem) {
  if (!item.password) return;
  await copyTextToClipboard(item.password);
}
</script>

<template>
  <section v-if="enabled" class="pansou-panel" :class="{ 'is-collapsed': collapsed }">
    <div
      class="pansou-panel__head"
      role="button"
      tabindex="0"
      :aria-expanded="!collapsed"
      :title="collapsed ? '展开资源搜索面板' : '收起搜索结果，给下方网盘目录腾出空间'"
      @click="toggleCollapsed"
      @keydown.enter.prevent="toggleCollapsed"
      @keydown.space.prevent="toggleCollapsed"
    >
      <div class="pansou-panel__titles">
        <h2>资源搜索</h2>
        <p>聚合已配置的网盘平台，搜索片名即可找到分享链接，复制后在离线下载 / 分享转存中提交。</p>
      </div>
      <span class="pansou-panel__head-side">
        <span class="pansou-panel__badge">PanSou</span>
        <span class="pansou-panel__collapse" :class="{ on: collapsed }">
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path
              d="M4 10l4-4 4 4"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          {{ collapseHint }}
        </span>
      </span>
    </div>
    <div v-show="!collapsed" class="pansou-panel__body">
      <div class="pansou-panel__search">
      <input
        v-model="q"
        placeholder="搜索电影、电视剧、动漫，如：流浪地球"
        :disabled="loading"
        @keyup.enter="search"
      />
      <AppButton :disabled="loading || !q.trim()" @click="search">
        {{ loading ? "搜索中…" : "搜索" }}
      </AppButton>
    </div>

    <p v-if="errorMsg" class="pansou-error">{{ errorMsg }}</p>

    <template v-if="results.length">
      <div class="pansou-meta">
        <template v-if="activeTab === ALL_TAB">
          共 {{ results.length }} 条结果<template v-if="total != null && total > results.length">（上游共 {{ total }} 条，已展示部分）</template>
        </template>
        <template v-else>
          「{{ tabLabel }}」{{ tabItems.length }} 条结果
        </template>
      </div>

      <div class="pansou-tabs" role="tablist" aria-label="按平台查看搜索结果">
        <button
          type="button"
          role="tab"
          :aria-selected="activeTab === ALL_TAB"
          :class="{ on: activeTab === ALL_TAB }"
          @click="selectTab(ALL_TAB)"
        >
          全部 <em>{{ results.length }}</em>
        </button>
        <button
          v-for="g in platformGroups"
          :key="g.code || 'unk'"
          type="button"
          role="tab"
          :aria-selected="activeTab === g.code"
          :class="{ on: activeTab === g.code }"
          @click="selectTab(g.code)"
        >
          {{ g.label }} <em>{{ g.items.length }}</em>
        </button>
      </div>

      <div class="pansou-results">
        <div v-for="(r, i) in pageItems" :key="`${r.code}-${r.url}-${i}`" class="pansou-result">
          <span class="pansou-result__plat">{{ r.platform }}</span>
          <div class="pansou-result__body">
            <strong :title="r.title">{{ r.title || "（未命名资源）" }}</strong>
            <small class="pansou-result__url" :title="r.url">{{ r.url }}</small>
          </div>
          <div class="pansou-result__actions">
            <button
              v-if="r.password"
              type="button"
              class="pansou-result__pwd"
              title="点击复制提取码"
              @click="copyPassword(r)"
            >
              提取码 {{ r.password }}
            </button>
            <button type="button" class="pansou-result__copy" @click="copyUrl(r)">复制链接</button>
          </div>
        </div>
      </div>

      <div v-if="totalPages > 1" class="pansou-pager">
        <button
          type="button"
          class="pansou-pager__btn"
          :disabled="page <= 1"
          @click="goToPage(page - 1)"
        >
          上一页
        </button>
        <div class="pansou-pager__nums">
          <template v-for="(p, idx) in pageNumbers()" :key="p == null ? `e-${idx}` : `p-${p}`">
            <button
              v-if="p != null"
              type="button"
              class="pansou-pager__num"
              :class="{ on: p === page }"
              :aria-current="p === page ? 'page' : undefined"
              @click="goToPage(p)"
            >
              {{ p }}
            </button>
            <span v-else class="pansou-pager__ellipsis">…</span>
          </template>
        </div>
        <button
          type="button"
          class="pansou-pager__btn"
          :disabled="page >= totalPages"
          @click="goToPage(page + 1)"
        >
          下一页
        </button>
      </div>
    </template>

    <p v-if="!loading && !errorMsg && searched && !results.length" class="pansou-hint">
      没有搜到相关资源，换个关键词，或在「后台 → 增强工具 → 影视搜索转存」里调整搜索平台范围试试。
    </p>
    <p v-else-if="!loading && !errorMsg && !searched" class="pansou-hint">
      输入片名开始搜索，复制分享链接后可在文件管理 / 离线下载中继续整理或转存。
    </p>
    </div>
  </section>
</template>

<style scoped>
.pansou-panel {
  margin-bottom: 18px;
  padding: 24px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.pansou-panel__head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  cursor: pointer;
  user-select: none;
  -webkit-user-select: none;
  border-radius: 10px;
}
.pansou-panel__head:hover h2 {
  color: var(--primary);
}
.pansou-panel__head:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 3px;
}
.pansou-panel__titles {
  min-width: 0;
}
.pansou-panel__head h2 {
  margin: 0 0 4px;
  font-size: 20px;
  transition: color 0.15s ease;
}
.pansou-panel__head p {
  margin: 0;
  color: var(--text-muted);
  font-size: 13px;
}
.pansou-panel__head-side {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
  margin-top: 2px;
}
.pansou-panel__badge {
  color: var(--primary);
  font-size: 12px;
  white-space: nowrap;
}
.pansou-panel__collapse {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 11px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1;
  white-space: nowrap;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}
.pansou-panel__collapse svg {
  width: 13px;
  height: 13px;
  transition: transform 0.2s ease;
}
.pansou-panel__head:hover .pansou-panel__collapse {
  border-color: var(--primary);
  color: var(--primary);
}
.pansou-panel__collapse.on svg {
  transform: rotate(180deg);
}
.pansou-panel__collapse.on {
  border-color: var(--primary);
  color: var(--primary);
  background: color-mix(in srgb, var(--primary) 10%, transparent);
}
/* 收起状态：面板只剩一行标题栏，描述与箭头状态联动 */
.pansou-panel.is-collapsed {
  padding-bottom: 12px;
}
.pansou-panel.is-collapsed .pansou-panel__head h2 {
  margin-bottom: 0;
}
.pansou-panel.is-collapsed .pansou-panel__head p {
  display: none;
}
.pansou-panel__search {
  display: flex;
  gap: 10px;
  margin-top: 18px;
}
.pansou-panel__search input {
  flex: 1;
  min-width: 0;
  padding: 11px 13px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg);
  color: var(--text);
  outline: none;
}
.pansou-panel__search input:focus {
  border-color: var(--primary);
}
.pansou-error {
  margin: 12px 0 0;
  color: #e5484d;
  font-size: 13px;
}
.pansou-meta {
  margin: 14px 0 0;
  color: var(--text-muted);
  font-size: 13px;
}
.pansou-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-soft);
}
.pansou-tabs button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text-muted);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}
.pansou-tabs button em {
  font-style: normal;
  font-size: 11px;
  line-height: 1;
  padding: 3px 6px;
  border-radius: 999px;
  background: var(--border-soft);
  color: var(--text-muted);
}
.pansou-tabs button:hover {
  border-color: var(--primary);
  color: var(--primary);
}
.pansou-tabs button.on {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary);
  font-weight: 600;
}
.pansou-tabs button.on em {
  background: var(--primary);
  color: #fff;
}
.pansou-results {
  margin-top: 4px;
}
.pansou-pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid var(--border-soft);
}
.pansou-pager__btn {
  padding: 6px 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text-regular);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}
.pansou-pager__btn:hover:not(:disabled) {
  border-color: var(--primary);
  color: var(--primary);
}
.pansou-pager__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.pansou-pager__nums {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.pansou-pager__num {
  min-width: 30px;
  height: 30px;
  padding: 0 6px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  color: var(--text-muted);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease, background 0.15s ease;
}
.pansou-pager__num:hover {
  border-color: var(--primary);
  color: var(--primary);
}
.pansou-pager__num.on {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary);
  font-weight: 600;
  cursor: default;
}
.pansou-pager__ellipsis {
  color: var(--text-muted);
  font-size: 13px;
  padding: 0 2px;
}
.pansou-result {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 12px 0;
  border-top: 1px solid var(--border-soft);
}
.pansou-result__plat {
  align-self: start;
  padding: 3px 8px;
  border-radius: 6px;
  color: var(--primary);
  background: color-mix(in srgb, var(--primary) 10%, transparent);
  font-size: 12px;
  white-space: nowrap;
}
.pansou-result__body {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.pansou-result__body strong {
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pansou-result__url {
  color: var(--text-muted);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pansou-result__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: flex-end;
}
.pansou-result__pwd {
  border: 1px dashed var(--border);
  background: var(--bg);
  color: var(--text-muted);
  font-size: 12px;
  border-radius: 7px;
  padding: 5px 9px;
  cursor: pointer;
}
.pansou-result__pwd:hover {
  border-color: var(--primary);
  color: var(--primary);
}
.pansou-result__copy {
  border: 0;
  background: none;
  color: var(--primary);
  cursor: pointer;
  font-size: 13px;
  padding: 4px 6px;
  white-space: nowrap;
}
.pansou-result__copy:hover {
  text-decoration: underline;
}
.pansou-hint {
  margin: 16px 0 0;
  color: var(--text-muted);
  font-size: 13px;
}
@media (max-width: 700px) {
  .pansou-result {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .pansou-result__actions {
    grid-column: 1 / -1;
    justify-content: flex-start;
  }
}
</style>
