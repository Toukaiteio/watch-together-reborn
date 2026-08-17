<script setup lang="ts">
import { computed } from 'vue'
import { FileVideo, ListVideo } from 'lucide-vue-next'
import type { MagnetPlayableFile } from '@/types'

const props = defineProps<{
  files: MagnetPlayableFile[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  select: [index: number]
}>()

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '未知大小'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

const sortedFiles = computed(() =>
  [...props.files].sort((a, b) =>
    a.fileName.localeCompare(b.fileName, 'zh-CN', { numeric: true, sensitivity: 'base' })
  )
)

const recommendedFileIndex = computed(() => {
  const primary = props.files.filter((file) => !/\b(sample|trailer|preview|extra)\b/i.test(file.fileName))
  const candidates = primary.length > 0 ? primary : props.files
  return candidates.reduce<MagnetPlayableFile | null>(
    (largest, file) => !largest || file.fileSize > largest.fileSize ? file : largest,
    null,
  )?.index
})
</script>

<template>
  <section class="w-full max-w-xl rounded-lg border border-line bg-bg p-3 text-left shadow-lg">
    <div class="mb-2 flex items-center gap-2 text-sm font-medium text-fg">
      <ListVideo :size="16" class="text-accent" />
      选择要播放的视频
    </div>
    <p class="mb-3 text-2xs text-fg-subtle">
      此磁力链接包含 {{ props.files.length }} 个可播放视频{{ props.disabled ? '，等待房主选择' : '。已按文件名排序，建议优先选择主视频文件' }}。
    </p>
    <div class="max-h-52 space-y-1 overflow-y-auto pr-1">
      <button
        v-for="(file, episodeIndex) in sortedFiles"
        :key="file.index"
        type="button"
        class="flex w-full items-center gap-2 rounded px-2 py-2 text-left transition-colors"
        :class="props.disabled ? 'cursor-default text-fg-muted' : 'hover:bg-bg-sunken hover:text-fg'"
        :disabled="props.disabled"
        @click="emit('select', file.index)"
      >
        <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-bg-sunken text-2xs text-fg-subtle">
          {{ episodeIndex + 1 }}
        </span>
        <FileVideo :size="15" class="shrink-0 text-accent" />
        <span class="min-w-0 flex-1 truncate text-xs">
          {{ file.fileName }}
          <span v-if="file.index === recommendedFileIndex" class="ml-1 rounded bg-accent/15 px-1 py-0.5 text-2xs text-accent">推荐</span>
        </span>
        <span class="shrink-0 rounded border border-line px-1 py-0.5 text-2xs text-fg-subtle">{{ file.extension.replace('.', '').toUpperCase() }}</span>
        <span class="shrink-0 text-2xs text-fg-subtle">{{ formatBytes(file.fileSize) }}</span>
      </button>
    </div>
  </section>
</template>
