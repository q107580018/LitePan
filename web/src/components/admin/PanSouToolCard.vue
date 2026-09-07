<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { http, getApiErrorMessage } from "@/api/client";
import { accountsApi } from "@/api/accounts";
import type { Account } from "@/api/types";
import type { OfflineDownloadCapabilities } from "@/types/offline-download";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import CloudToolCard from "@/components/admin/CloudToolCard.vue";
import { copyTextToClipboard, toast } from "@/composables/useToast";
import {
  normalizePanSouTypes,
  PANSOU_CLOUD_TYPES,
  pansouPlatformLabel,
  parsePanSouPayload,
  type PanSouItem,
} from "@/utils/pansou";
import { loadPansouAccountCapabilities, pansouSaveCandidates } from "@/utils/pansouSave";
import PanSouSaveModal from "@/components/file/PanSouSaveModal.vue";

const props = withDefaults(defineProps<{ searchQuery?: string }>(), { searchQuery: "" });

interface PanSouConfigData {
  enabled: boolean;
  endpoint: string;
  username: string;
  password_configured: boolean;
  token_configured: boolean;
  platforms: string[];
}

const cfg = ref<PanSouConfigData>({
  enabled: false,
  endpoint: "https://so.252035.xyz",
  username: "",
  password_configured: false,
  token_configured: false,
  platforms: [],
});
const saving = ref(false);
const configOpen = ref(false);
const savingConfig = ref(false);
const testing = ref(false);
const testKw = ref("");
const testError = ref("");
const testCount = ref<number | null>(null);
const testItems = ref<PanSouItem[]>([]);

const draft = reactive({
  endpoint: "",
  username: "",
  password: "",
  token: "",
  types: [] as string[],
});

function matches(title: string) {
  const q = props.searchQuery.trim().toLowerCase();
  return !q || title.toLowerCase().includes(q);
}

const platformStat = computed(() => {
  const count = cfg.value.platforms.length;
  return count ? String(count) : "全部";
});
const platformStatLabel = computed(() => (cfg.value.platforms.length ? "个平台范围" : "平台类型"));

function selectedType(code: string) {
  return draft.types.includes(code);
}

function toggleType(code: string) {
  const index = draft.types.indexOf(code);
  if (index >= 0) {
    draft.types.splice(index, 1);
  } else {
    draft.types.push(code);
  }
}

function setAllTypes(checked: boolean) {
  draft.types = checked ? [...PANSOU_CLOUD_TYPES] : [];
}

function fillDraft(next: PanSouConfigData) {
  draft.endpoint = next.endpoint;
  draft.username = next.username;
  draft.password = "";
  draft.token = "";
  draft.types = [...next.platforms];
}

async function load() {
  try {
    const d: PanSouConfigData = await http.get("/admin/tools/pansou/config");
    cfg.value = {
      enabled: Boolean(d.enabled),
      endpoint: d.endpoint || "https://so.252035.xyz",
      username: d.username || "",
      password_configured: Boolean(d.password_configured),
      token_configured: Boolean(d.token_configured),
      platforms: normalizePanSouTypes(d.platforms),
    };
  } catch (e) {
    toast.error(getApiErrorMessage(e, "加载 PanSou 设置失败"));
  }
}

onMounted(() => {
  void load();
  void ensureSaveCapabilities();
});

async function toggleEnabled() {
  if (saving.value) return;
  saving.value = true;
  const next = !cfg.value.enabled;
  try {
    await http.put("/admin/tools/pansou/config", {
      enabled: next,
      endpoint: cfg.value.endpoint,
      username: cfg.value.username,
      platforms: cfg.value.platforms,
    });
    cfg.value.enabled = next;
    toast.success(
      next ? "已启用：前台首页将显示资源搜索入口" : "已停用：前台首页的资源搜索入口已隐藏",
    );
  } catch (e) {
    toast.error(getApiErrorMessage(e, "修改开关失败"));
  } finally {
    saving.value = false;
  }
}

function openConfig() {
  fillDraft(cfg.value);
  testKw.value = "";
  testError.value = "";
  testCount.value = null;
  testItems.value = [];
  configOpen.value = true;
}

function closeConfig() {
  configOpen.value = false;
}

async function saveConfig() {
  savingConfig.value = true;
  try {
    await http.put("/admin/tools/pansou/config", {
      enabled: cfg.value.enabled,
      endpoint: draft.endpoint,
      username: draft.username,
      password: draft.password,
      token: draft.token,
      platforms: draft.types,
    });
    await load();
    toast.success("PanSou 配置已保存，前台搜索将使用新的服务与平台范围");
    configOpen.value = false;
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存 PanSou 设置失败"));
  } finally {
    savingConfig.value = false;
  }
}

function testQuery() {
  const query: Record<string, string> = {
    q: testKw.value.trim(),
    endpoint: draft.endpoint.trim() || cfg.value.endpoint,
    username: draft.username.trim(),
    platforms: draft.types.join(","),
  };
  if (draft.password.trim()) query.password = draft.password.trim();
  if (draft.token.trim()) query.token = draft.token.trim();
  if (!query.username) delete query.username;
  if (!query.platforms) delete query.platforms;
  return query;
}

async function runTest() {
  if (!testKw.value.trim() || testing.value) return;
  testing.value = true;
  testError.value = "";
  testCount.value = null;
  testItems.value = [];
  try {
    const payload: any = await http.get("/admin/tools/pansou/search", testQuery());
    const items = parsePanSouPayload(payload);
    testCount.value = items.length;
    testItems.value = items.slice(0, 5);
    if (!items.length) testError.value = "连通正常，但没有搜到结果，可换个关键词或减少平台范围试试";
  } catch (e) {
    testError.value = getApiErrorMessage(e, "测试失败");
  } finally {
    testing.value = false;
  }
}

async function copyItemUrl(item: PanSouItem) {
  const ok = await copyTextToClipboard(item.url);
  if (ok) toast.success("链接已复制");
}

// —— 一键转存：测试搜索结果同样可直接转存到已绑定账号 ——
const accounts = ref<Account[]>([]);
const capsByAccount = ref<Record<number, OfflineDownloadCapabilities>>({});
const capsLoading = ref(false);
const capsReady = ref(false);
const saveOpen = ref(false);
const saveItem = ref<PanSouItem | null>(null);

async function ensureSaveCapabilities() {
  if (capsReady.value) return;
  capsLoading.value = true;
  try {
    if (!accounts.value.length) {
      accounts.value = await accountsApi.list();
    }
    capsByAccount.value = await loadPansouAccountCapabilities(accounts.value);
    capsReady.value = true;
  } catch (e) {
    toast.error(getApiErrorMessage(e, "探测账号转存能力失败"));
  } finally {
    capsLoading.value = false;
  }
}

function saveCandidates(item: PanSouItem) {
  return pansouSaveCandidates(item, accounts.value, capsByAccount.value);
}

function saveDisabled(item: PanSouItem) {
  return capsLoading.value || saveCandidates(item).length === 0;
}

function saveTitle(item: PanSouItem) {
  if (capsLoading.value) return "正在探测可转存的网盘账号…";
  if (saveCandidates(item).length === 0) {
    return "没有可接收该资源的网盘账号：需绑定对应平台的账号（夸克/115 支持分享转存），磁力、电驴链接可转到支持离线下载的账号";
  }
  return "一键转存到网盘，可选择目标目录";
}

function openSave(item: PanSouItem) {
  if (!capsReady.value || capsLoading.value || saveCandidates(item).length === 0) return;
  saveItem.value = item;
  saveOpen.value = true;
}
</script>

<template>
  <div v-show="matches('影视搜索转存') || matches('PanSou') || matches('盘搜')">
    <CloudToolCard
      :enabled="cfg.enabled"
      name="影视搜索转存"
      driver="PanSou · 聚合网盘资源搜索"
      logo-text="搜"
      :stat-value="platformStat"
      :stat-label="platformStatLabel"
      :compact-stat="true"
    >
      前台首页提供聚合搜索，覆盖所选网盘与磁力 / 电驴链接；复制分享链接后可在离线下载或分享转存中提交。
      <template #toggle>
        <button
          class="check-toggle"
          type="button"
          :class="{ on: cfg.enabled }"
          :aria-label="cfg.enabled ? '停用影视搜索转存' : '启用影视搜索转存'"
          :disabled="saving"
          title="启用 / 停用"
          @click="toggleEnabled"
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <path
              d="M3.5 8.5 6.5 11.5 12.5 4.5"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </template>
      <template #actions>
        <AppButton size="sm" variant="secondary" :disabled="saving" @click="openConfig">配置</AppButton>
      </template>
    </CloudToolCard>

    <AppModal :open="configOpen" title="影视搜索转存 · 配置" size="md" @close="closeConfig">
      <div class="ps-config">
        <p class="ps-config__tip">
          前台首页的搜索会通过本服务代理到 PanSou API；此处保存后立即生效，无需重新部署。
        </p>

        <div class="ps-field">
          <label>服务地址</label>
          <input v-model.trim="draft.endpoint" placeholder="https://so.252035.xyz" spellcheck="false" />
        </div>

        <div class="ps-field">
          <label>Basic Auth 用户名</label>
          <input v-model.trim="draft.username" placeholder="上游需要 Basic Auth 时填写" spellcheck="false" />
        </div>
        <div class="ps-field">
          <label>Basic Auth 密码</label>
          <input
            v-model="draft.password"
            type="password"
            autocomplete="new-password"
            :placeholder="cfg.password_configured ? '留空保持当前已保存的密码' : '可选'"
          />
        </div>
        <div class="ps-field">
          <label>API Token</label>
          <input
            v-model="draft.token"
            type="password"
            autocomplete="new-password"
            :placeholder="cfg.token_configured ? '留空保持当前已保存的 Token' : '可选'"
          />
        </div>

        <div class="ps-field">
          <label class="ps-field__label-row">
            <span>搜索平台范围（不选表示搜索全部类型）</span>
            <span class="ps-chip-actions">
              <button type="button" @click="setAllTypes(true)">全选</button>
              <button type="button" @click="setAllTypes(false)">清空</button>
            </span>
          </label>
          <div class="ps-chips">
            <button
              v-for="code in PANSOU_CLOUD_TYPES"
              :key="code"
              type="button"
              class="ps-chip"
              :class="{ on: selectedType(code) }"
              @click="toggleType(code)"
            >
              {{ pansouPlatformLabel(code) }}
            </button>
          </div>
        </div>

        <div class="ps-test">
          <div class="ps-test__head">
            <span>连通性测试（未保存的配置也可直接测试）</span>
          </div>
          <div class="ps-test__row">
            <input v-model="testKw" placeholder="输入关键词，如：流浪地球" :disabled="testing" @keyup.enter="runTest" />
            <AppButton size="sm" :disabled="testing || !testKw.trim()" @click="runTest">
              {{ testing ? "测试中…" : "测试搜索" }}
            </AppButton>
          </div>
          <p v-if="testError" class="ps-test__error">{{ testError }}</p>
          <p v-else-if="testCount != null" class="ps-test__ok">共 {{ testCount }} 条分享链接</p>
          <div v-if="testItems.length" class="ps-test__list">
            <div v-for="(item, i) in testItems" :key="i" class="ps-test__item">
              <span class="ps-test__plat">{{ item.platform }}</span>
              <div class="ps-test__body">
                <strong :title="item.title">{{ item.title || "（未命名资源）" }}</strong>
                <small :title="item.url">{{ item.url }}</small>
              </div>
              <span class="ps-test__acts">
                <button
                  type="button"
                  class="ps-test__save"
                  :class="{ disabled: saveDisabled(item) }"
                  :disabled="saveDisabled(item)"
                  :title="saveTitle(item)"
                  @click="openSave(item)"
                >
                  转存
                </button>
                <button type="button" @click="copyItemUrl(item)">复制</button>
              </span>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <span class="ps-config__foot">平台代号只保留 PanSou 认可的类型，无效代号会在保存时自动过滤。</span>
        <AppButton variant="secondary" :disabled="savingConfig" @click="closeConfig">取消</AppButton>
        <AppButton variant="primary" :disabled="savingConfig" @click="saveConfig">
          {{ savingConfig ? "保存中…" : "保存设置" }}
        </AppButton>
      </template>
    </AppModal>

    <PanSouSaveModal
      :open="saveOpen"
      :item="saveItem"
      :candidates="saveItem ? saveCandidates(saveItem) : []"
      @close="saveOpen = false"
    />
  </div>
</template>

<style scoped>
.check-toggle {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 0;
  padding: 0;
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  background: var(--border);
  color: var(--text-muted);
  transition: background 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
}
.check-toggle svg {
  width: 14px;
  height: 14px;
}
.check-toggle:hover {
  background: var(--surface-hover);
}
.check-toggle.on {
  background: var(--success);
  color: #fff;
  box-shadow: 0 0 0 4px rgba(16, 185, 129, 0.16);
}
.check-toggle.on:hover {
  background: color-mix(in srgb, var(--success) 88%, #000);
}
.check-toggle:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ps-config {
  display: grid;
  gap: 14px;
}
.ps-config__tip {
  margin: 0;
  padding: 10px 12px;
  border-radius: 10px;
  background: var(--surface-sunken);
  border: 1px solid var(--border-soft);
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.6;
}
.ps-field {
  display: grid;
  gap: 6px;
}
.ps-field label {
  font-size: 12px;
  color: var(--text-muted);
}
.ps-field__label-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.ps-chip-actions {
  display: flex;
  gap: 10px;
}
.ps-chip-actions button {
  border: 0;
  background: none;
  color: var(--primary);
  font-size: 12px;
  cursor: pointer;
  padding: 0;
}
.ps-field input {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--bg);
  color: var(--text);
  font-size: 13px;
  outline: none;
}
.ps-field input:focus {
  border-color: var(--primary);
}
.ps-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.ps-chip {
  padding: 6px 13px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--text-regular);
  font-size: 13px;
  cursor: pointer;
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}
.ps-chip:hover {
  border-color: var(--primary);
}
.ps-chip.on {
  border-color: var(--primary);
  background: color-mix(in srgb, var(--primary) 12%, transparent);
  color: var(--primary);
}
.ps-test {
  border: 1px solid var(--border-soft);
  border-radius: 12px;
  padding: 12px;
  background: var(--surface-sunken);
  display: grid;
  gap: 10px;
}
.ps-test__head {
  font-size: 12px;
  color: var(--text-muted);
}
.ps-test__row {
  display: flex;
  gap: 8px;
}
.ps-test__row input {
  flex: 1;
  min-width: 0;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  outline: none;
}
.ps-test__error {
  margin: 0;
  color: #e5484d;
  font-size: 13px;
}
.ps-test__ok {
  margin: 0;
  color: var(--success);
  font-size: 13px;
}
.ps-test__list {
  display: grid;
  gap: 8px;
}
.ps-test__item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  border: 1px solid var(--border-soft);
  background: var(--surface);
  border-radius: 10px;
  padding: 8px 10px;
}
.ps-test__plat {
  color: var(--primary);
  background: color-mix(in srgb, var(--primary) 10%, transparent);
  font-size: 12px;
  border-radius: 6px;
  padding: 2px 7px;
  white-space: nowrap;
}
.ps-test__body {
  min-width: 0;
  display: grid;
  gap: 2px;
}
.ps-test__body strong {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ps-test__body small {
  color: var(--text-muted);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ps-test__item button {
  border: 0;
  background: none;
  color: var(--primary);
  font-size: 13px;
  cursor: pointer;
}
.ps-test__acts {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}
.ps-test__acts button {
  white-space: nowrap;
}
.ps-test__save {
  background: var(--brand-gradient-h) !important;
  color: var(--text-on-brand) !important;
  border-radius: 7px;
  padding: 5px 11px;
  transition: opacity 0.15s ease;
}
.ps-test__save:hover {
  opacity: 0.9;
}
.ps-test__save.disabled {
  background: var(--border) !important;
  color: var(--text-muted) !important;
  cursor: not-allowed;
}
.ps-test__save.disabled:hover {
  opacity: 1;
}
.ps-config__foot {
  margin-right: auto;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}
@media (max-width: 560px) {
  .ps-test__item {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .ps-test__item button {
    grid-column: 1 / -1;
    justify-self: end;
  }
}
</style>
