<script setup lang="ts">
import { useRoomStore } from '@/stores/room'
import VideoPlayer from '@/components/VideoPlayer.vue'
import Sidebar from '@/components/Sidebar.vue'
import { Copy, RotateCcw, X } from 'lucide-vue-next'

const room = useRoomStore()

async function copyErrorDetails() {
  if (!room.connectionError) return
  try {
    await navigator.clipboard.writeText(room.connectionError)
  } catch {}
}
</script>

<template>
  <div
    class="h-full flex flex-col"
    :class="{ 'room-fullscreen': room.playerFullscreen }"
  >
    <div class="room-body flex-1 flex overflow-hidden">
      <div class="room-player flex-1 min-w-0 bg-bg-sunken relative">
        <div
          v-if="room.connectionError"
          class="absolute top-3 left-3 z-20 max-w-[min(520px,calc(100%-24px))] rounded border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger backdrop-blur-sm"
        >
          <div class="flex items-start gap-2">
            <span class="min-w-0 flex-1 leading-5">{{ room.connectionError }}</span>
            <button class="shrink-0 opacity-75 hover:opacity-100" title="关闭提示" @click="room.dismissConnectionError()">
              <X :size="14" />
            </button>
          </div>
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              v-if="room.videoState.sourceType === 'magnet' && room.videoState.source"
              class="inline-flex items-center gap-1 rounded border border-danger/30 bg-bg/50 px-2 py-1 text-2xs hover:bg-bg"
              @click="room.retryCurrentVideo()"
            >
              <RotateCcw :size="11" />
              重试
            </button>
            <button
              v-if="room.isHost"
              class="inline-flex items-center gap-1 rounded border border-danger/30 bg-bg/50 px-2 py-1 text-2xs hover:bg-bg"
              @click="room.activeTab = 'host'"
            >
              更换视频
            </button>
            <button
              class="inline-flex items-center gap-1 rounded border border-danger/30 bg-bg/50 px-2 py-1 text-2xs hover:bg-bg"
              @click="copyErrorDetails"
            >
              <Copy :size="11" />
              复制详情
            </button>
          </div>
        </div>
        <VideoPlayer />
      </div>

      <aside class="room-sidebar w-sidebar border-l border-line bg-bg-elevated shrink-0 flex flex-col overflow-hidden">
        <Sidebar />
      </aside>
    </div>
  </div>
</template>

<style scoped>
.room-fullscreen .room-sidebar {
  display: none;
}

.room-fullscreen .room-player {
  width: 100%;
  height: 100%;
}
</style>
