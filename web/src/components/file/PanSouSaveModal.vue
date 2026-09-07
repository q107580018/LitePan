<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { Account } from "@/api/types";
import { offlineDownloadApi } from "@/api/offlineDownload";
import { getApiErrorMessage } from "@/api/client";
import { toast } from "@/composables/useToast";
import type { OfflineDownloadTask } from "@/types/offline-download";
import type { PanSouItem } from "@/utils/pansou";
import type { PanSouSaveCandidate } from "@/utils/pansouSave";
import AppModal from "@/components/base/AppModal.vue";
import AppButton from "@/components/base/AppButton.vue";
import DriverIcon from "@/components/driver/DriverIcon.vue";
import FolderPickerModal from "./FolderPickerModal.vue";
import type { FolderSelection } from "./FolderSelector.vue";

const props = defineProps<{
  open: boolean;
  item: PanSouItem | null;
  candidates: PanSouSaveCandidate[];
}>();

const emit = defineEmits<{
  close: [];
  created: [tasks: OfflineDownloadTask[], target: { accountId: number; parentId: string; path: string }];
}>();

const folderPickerOpen = ref(false);
const saving = ref(false);
// 当前已选中的目标账号与目录（点「一键转存」前的临时状态，提交后即关闭）。
const chosenAccountId = ref<number | null>(null);
const targetParentId = ref("");
const targetPath = ref("/");

const chosenCandidate = computed(
  () => props.candidates.find((c) => c.accountId === chosenAccountId.value) ?? props.candidates[0] ?? null,
);
const displayAccount = computed(() => {
  const candidate = chosenCandidate.value;
  if (!candidate) return null;
  return {
    id: candidate.accountId,
    name: candidate.accountName,
    driver: candidate.driverCardName?.trim() || candidate.driverType,
  };
});

/** FolderPickerModal 只认 Account 结构，这里把候选账号转成最小可用对象。 */
const folderPickerAccounts = computed<Account[]>(() =>
  props.candidates.map((candidate) => ({
    id: candidate.accountId,
    name: candidate.accountName,
    driver_type: candidate.driverType,
    driver_card_name: candidate.driverCardName,
    driver_card_color: candidate.driverCardColor,
    driver_card_logo: candidate.driverCardLogo,
    config: "{}",
    is_active: true,
    is_default: false,
    sort_order: 0,
  })),
);

const submitDisabled = computed(() => {
  if (saving.value || !props.open) return true;
  if (!props.item || !props.candidates.length) return true;
  return !chosenCandidate.value;
});

watch(
  () => props.open,
  (open) => {
    if (!open) return;
    saving.value = false;
    folderPickerOpen.value = false;
    // 默认定位到第一个可转存账号的根目录。
    const first = props.candidates[0];
    chosenAccountId.value = first?.accountId ?? null;
    targetParentId.value = "";
    targetPath.value = "/";
  },
  { immediate: true },
);

function openFolderPicker() {
  if (!props.candidates.length || saving.value) return;
  folderPickerOpen.value = true;
}

function onFolderResolve(payload: {
  accountId: number;
  accountName: string;
  parentId: string;
  path: string;
  selections?: FolderSelection[];
}) {
  folderPickerOpen.value = false;
  chosenAccountId.value = payload.accountId;
  targetParentId.value = payload.parentId;
  targetPath.value = payload.path || "/";
}

function close() {
  if (saving.value) return;
  emit("close");
}

async function submit() {
  if (submitDisabled.value) return;
  const item = props.item;
  const candidate = chosenCandidate.value;
  if (!item || !candidate) return;
  const accountId = candidate.accountId;
  const parentId = targetParentId.value;
  const path = targetPath.value || "/";
  saving.value = true;
  const target = { accountId, parentId, path };
  // 与离线下载弹窗一致：提交后立刻收起，进度交给任务面板与提示。
  emit("close");
  toast.info("正在转存，可打开「任务」面板查看进度");
  try {
    if (candidate.mode === "share") {
      const preparation = await offlineDownloadApi.prepareShare({
        account_id: accountId,
        link: item.url,
        passcode: item.password?.trim() || undefined,
      });
      const fileIds = preparation.files.map((file) => file.id);
      if (!fileIds.length) {
        toast.error("分享里没有可转存的内容");
        return;
      }
      const task = await offlineDownloadApi.addShare({
        account_id: accountId,
        preparation_id: preparation.preparation_id,
        file_ids: fileIds,
        target_parent_id: parentId,
        target_display_path: path,
      });
      emit("created", [task], target);
      if (task.status === "success") toast.success("分享已转存到你的网盘");
      else toast.success("转存任务已提交，网盘处理完成后可在任务面板查看");
      return;
    }
    const [task] = await offlineDownloadApi.addURLs({
      account_id: accountId,
      provider_kind: "native",
      urls: [item.url],
      target_parent_id: parentId,
      target_display_path: path,
    });
    emit("created", [task], target);
    if (task.status === "success") toast.success("已离线下载并保存到你的网盘");
    else if (task.status === "failed") toast.warning(task.error || "离线下载任务已失败，请在任务面板查看原因");
    else toast.success("离线下载任务已提交，完成后会自动入盘");
  } catch (error) {
    toast.error(getApiErrorMessage(error, "转存失败，请稍后重试"));
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <AppModal :open="open" size="lg" title="转存到我的网盘" @close="close">
    <div class="pansou-save">
      <template v-if="item">
        <section class="pansou-save__resource">
          <span class="pansou-save__plat">{{ item.platform }}</span>
          <div class="pansou-save__resource-body">
            <strong :title="item.title">{{ item.title || "（未命名资源）" }}</strong>
            <small class="pansou-save__url" :title="item.url">{{ item.url }}</small>
          </div>
          <button
            v-if="item.password"
            type="button"
            class="pansou-save__pwd"
            title="分享提取码"
          >
            提取码 {{ item.password }}
          </button>
        </section>

        <div v-if="!candidates.length" class="pansou-save__empty">
          <strong>当前没有可用于转存的网盘账号</strong>
          <p>
            该资源为{{ item.platform }}分享链接，需要在「存储管理」里绑定并登录对应的网盘账号
            （夸克 / 115 支持分享一键转存，磁力、电驴链接可转存到支持离线下载的账号）。
          </p>
          <p class="pansou-save__empty-hint">也可以先复制链接，在「离线下载 → 分享转存」中手动提交。</p>
        </div>

        <template v-else>
          <div class="pansou-save__mode-note">
            {{ chosenCandidate?.reason }} 会保存到下方选择的账号与目录。
          </div>

          <section class="pansou-save__target">
            <div class="pansou-save__target-row">
              <div class="pansou-save__target-account">
                <DriverIcon
                  v-if="displayAccount"
                  :name="displayAccount.driver"
                  :color="chosenCandidate?.driverCardColor"
                  :logo="chosenCandidate?.driverCardLogo"
                  :size="30"
                />
                <span v-if="displayAccount" class="pansou-save__account-body">
                  <strong>{{ displayAccount.name }}</strong>
                  <small>{{ displayAccount.driver }}</small>
                </span>
              </div>
              <div class="pansou-save__target-folder">
                <strong :title="targetPath">{{ targetPath }}</strong>
              </div>
              <AppButton size="sm" :disabled="saving" @click="openFolderPicker">选择账号/目录</AppButton>
            </div>
          </section>
        </template>
      </template>
    </div>

    <template #footer>
      <AppButton variant="ghost" :disabled="saving" @click="close">取消</AppButton>
      <AppButton
        variant="primary"
        :disabled="submitDisabled"
        :title="!candidates.length ? '没有可用的转存账号' : ''"
        @click="submit"
      >
        {{ saving ? "正在转存…" : "一键转存" }}
      </AppButton>
    </template>
  </AppModal>

  <FolderPickerModal
    :open="folderPickerOpen"
    title="选择转存目标（账号 + 目录）"
    confirm-text="保存到当前目录"
    :accounts="folderPickerAccounts"
    selectable-account
    :allow-create-folder="true"
    :show-refresh="false"
    @resolve="onFolderResolve"
    @close="folderPickerOpen = false"
  />
</template>

<style scoped>
.pansou-save {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.pansou-save__resource {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 11px;
  background: var(--surface-sunken);
}
.pansou-save__plat {
  align-self: start;
  padding: 3px 9px;
  border-radius: 7px;
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, transparent);
  font-size: 12px;
  white-space: nowrap;
}
.pansou-save__resource-body {
  min-width: 0;
  display: grid;
  gap: 4px;
}
.pansou-save__resource-body strong {
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pansou-save__url {
  color: var(--text-muted);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pansou-save__pwd {
  justify-self: end;
  border: 1px dashed var(--border);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12px;
  border-radius: 7px;
  padding: 5px 9px;
  white-space: nowrap;
}
.pansou-save__empty {
  padding: 20px 22px;
  border: 1px solid var(--border);
  border-radius: 11px;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.7;
  text-align: center;
}
.pansou-save__empty strong {
  display: block;
  color: var(--text-regular);
  margin-bottom: 6px;
}
.pansou-save__empty p {
  margin: 0 auto;
  max-width: 520px;
}
.pansou-save__empty .pansou-save__empty-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-muted);
}
.pansou-save__mode-note {
  color: var(--text-muted);
  font-size: 12px;
}
.pansou-save__target {
  border: 1px solid var(--border);
  border-radius: 11px;
  background: var(--surface);
  overflow: hidden;
}
.pansou-save__target-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  padding: 12px 14px;
}
.pansou-save__target-account {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}
.pansou-save__account-body {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.pansou-save__account-body strong {
  color: var(--text);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pansou-save__account-body small {
  color: var(--text-muted);
  font-size: 11px;
}
.pansou-save__target-folder {
  min-width: 0;
  flex: 1;
}
.pansou-save__target-folder strong {
  display: block;
  color: var(--text);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (max-width: 640px) {
  .pansou-save__resource {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .pansou-save__pwd {
    grid-column: 1 / -1;
    justify-self: start;
  }
}
</style>
