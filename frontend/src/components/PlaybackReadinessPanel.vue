<script setup lang="ts">
import { computed } from 'vue'
import { CircleCheck, Clock3, ListVideo, LoaderCircle, UsersRound } from 'lucide-vue-next'
import type { PlaybackReadiness } from '@/types'

const props = defineProps<{
  readiness: PlaybackReadiness
  isHost: boolean
  autoStartEnabled: boolean
}>()

const emit = defineEmits<{
  playNow: []
  waitForMembers: []
  cancelAutoStart: []
}>()

const icon = computed(() => {
  switch (props.readiness.state) {
    case 'ready': return CircleCheck
    case 'selecting': return ListVideo
    case 'waiting_for_members': return UsersRound
    case 'preparing': return LoaderCircle
    default: return Clock3
  }
})

const toneClass = computed(() => {
  switch (props.readiness.state) {
    case 'ready': return 'border-accent/30 bg-accent/5 text-accent'
    case 'waiting_for_members': return 'border-line-strong bg-bg-sunken text-fg'
    case 'selecting': return 'border-accent/30 bg-accent/5 text-accent'
    default: return 'border-line bg-bg-sunken text-fg-muted'
  }
})
</script>

<template>
  <section class="mt-3 rounded border px-3 py-2.5" :class="toneClass">
    <div class="flex items-start gap-2">
      <component :is="icon" :size="16" class="mt-0.5 shrink-0" :class="readiness.state === 'preparing' ? 'animate-spin' : ''" />
      <div class="min-w-0 flex-1">
        <div class="flex items-center justify-between gap-2 text-xs font-medium">
          <span>{{ readiness.label }}</span>
          <span v-if="readiness.totalMembers > 0" class="shrink-0 text-2xs font-normal opacity-80">
            {{ readiness.readyMembers }}/{{ readiness.totalMembers }} 就绪
          </span>
        </div>
        <p class="mt-1 text-2xs leading-5 opacity-85">{{ readiness.detail }}</p>
        <p class="mt-1 text-2xs leading-5 font-medium">{{ readiness.recommendation }}</p>
        <div v-if="isHost && readiness.state !== 'no_source' && readiness.state !== 'selecting' && readiness.state !== 'preparing'" class="mt-2 flex flex-wrap gap-2">
          <button class="btn px-2 py-1 text-2xs" @click="emit('playNow')">
            立即播放
          </button>
          <button
            v-if="!autoStartEnabled"
            class="btn-outline px-2 py-1 text-2xs"
            @click="emit('waitForMembers')"
          >
            全员就绪后播放
          </button>
          <button
            v-else
            class="btn-outline px-2 py-1 text-2xs"
            @click="emit('cancelAutoStart')"
          >
            取消自动播放
          </button>
        </div>
        <p v-if="autoStartEnabled" class="mt-2 text-2xs leading-5 font-medium">已开启：全员缓存达到流畅播放标准后将自动开始。</p>
      </div>
    </div>
  </section>
</template>
