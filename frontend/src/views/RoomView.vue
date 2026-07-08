<script setup lang="ts">
import { useRoomStore } from '@/stores/room'
import VideoPlayer from '@/components/VideoPlayer.vue'
import Sidebar from '@/components/Sidebar.vue'

const room = useRoomStore()
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
          {{ room.connectionError }}
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