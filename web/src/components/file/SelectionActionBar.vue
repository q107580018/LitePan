<script setup lang="ts">
import { computed } from "vue";
import SvgIcon from "@/components/icons/SvgIcon.vue";
import type { FileItem } from "@/api/types";
import { buildSelectedCountText } from "@/composables/useFileActions";

// 底部悬浮操作栏：单选/多选项目后直接展示可点击的操作按钮，
// 与右键菜单并存——手机上无法右键，操作从这里触达。
const props = defineProps<{
  selectedFiles: FileItem[];
  acting?: boolean;
  // 移动端悬浮账号切换器也停靠在底部，需要额外抬高避免遮挡。
  floatingAccounts?: boolean;
}>();

const emit = defineEmits<{
  rename: [];
  download: [];
  move: [];
  copy: [];
  delete: [];
  clear: [];
}>();

const count = computed(() => props.selectedFiles.length);
const singleFile = computed(() =>
  props.selectedFiles.length === 1 ? props.selectedFiles[0] : null,
);
const canRename = computed(() => count.value === 1 && !!singleFile.value);
const canDownload = computed(
  () => count.value === 1 && !!singleFile.value && !singleFile.value.is_dir,
);
const countText = computed(() =>
  count.value > 0 ? `已选 ${buildSelectedCountText(props.selectedFiles)}` : "",
);
</script>

<template>
  <Transition name="selection-bar">
    <div
      v-if="count > 0"
      class="selection-bar"
      :class="{ 'selection-bar--floating-accounts': floatingAccounts }"
      role="toolbar"
      aria-label="选中项操作"
    >
      <span class="selection-bar__count">{{ countText }}</span>
      <div class="selection-bar__actions">
        <button
          v-if="canRename"
          type="button"
          class="selection-bar__btn"
          :disabled="acting"
          @click="emit('rename')"
        >
          <SvgIcon name="pen" :size="14" />
          <span>重命名</span>
        </button>
        <button
          v-if="canDownload"
          type="button"
          class="selection-bar__btn"
          :disabled="acting"
          @click="emit('download')"
        >
          <SvgIcon name="download" :size="14" />
          <span>下载</span>
        </button>
        <button
          type="button"
          class="selection-bar__btn"
          :disabled="acting"
          @click="emit('move')"
        >
          <SvgIcon name="right-left" :size="14" />
          <span>移动</span>
        </button>
        <button
          type="button"
          class="selection-bar__btn"
          :disabled="acting"
          @click="emit('copy')"
        >
          <SvgIcon name="hand-copy" :size="14" />
          <span>复制</span>
        </button>
        <button
          type="button"
          class="selection-bar__btn selection-bar__btn--danger"
          :disabled="acting"
          @click="emit('delete')"
        >
          <SvgIcon name="hand-trash-alt" :size="14" />
          <span>删除</span>
        </button>
      </div>
      <button
        type="button"
        class="selection-bar__close"
        title="取消选择"
        aria-label="取消选择"
        @click="emit('clear')"
      >
        <SvgIcon name="hand-close" :size="14" />
      </button>
    </div>
  </Transition>
</template>

<style scoped>
.selection-bar {
  position: fixed;
  left: 50%;
  /* 底部固定页脚（AppFooter，min-height 56px）之上留出间距 */
  bottom: calc(56px + 16px);
  transform: translateX(-50%);
  z-index: 60;
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: calc(100vw - 24px);
  padding: 8px 10px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-lg);
  background: color-mix(in srgb, var(--surface) 94%, transparent);
  backdrop-filter: blur(10px);
  box-shadow: 0 14px 34px color-mix(in srgb, rgb(15 23 42) 20%, transparent);
}

.selection-bar__count {
  flex-shrink: 0;
  padding: 0 6px;
  color: var(--brand);
  font-size: 13px;
  font-weight: 700;
  line-height: 1.3;
  white-space: nowrap;
}

.selection-bar__actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.selection-bar__btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 13px;
  font-weight: 600;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  transition: var(--transition);
}

.selection-bar__btn:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.selection-bar__btn:disabled {
  opacity: 0.55;
  cursor: wait;
}

.selection-bar__btn--danger {
  color: var(--danger);
}

.selection-bar__btn--danger:hover:not(:disabled) {
  border-color: var(--danger);
  background: color-mix(in srgb, var(--danger) 8%, transparent);
  color: var(--danger);
}

.selection-bar__close {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border: 0;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.selection-bar__close:hover {
  background: var(--surface-sunken);
  color: var(--text);
}

/* 移动端：页脚在窄屏会换行变高（悬浮账号切换器同样按 88px 估算），
   操作栏整体抬高到页脚之上；开启悬浮账号切换时再避开底部的账号胶囊。 */
@media (max-width: 768px) {
  .selection-bar {
    bottom: calc(88px + 12px);
    gap: 6px;
    padding: 7px 8px;
  }

  .selection-bar__btn {
    height: 30px;
    padding: 0 10px;
    font-size: 12px;
  }

  .selection-bar--floating-accounts {
    /* 88px(页脚) + 12px(间距) + 账号胶囊(~50px) + 10px(间距) */
    bottom: calc(88px + 12px + 50px + 10px);
  }
}

.selection-bar-enter-active,
.selection-bar-leave-active {
  transition:
    opacity 0.18s ease,
    transform 0.22s ease;
}

.selection-bar-enter-from,
.selection-bar-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(10px);
}
</style>
