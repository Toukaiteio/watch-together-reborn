<script setup lang="ts">
import { computed } from 'vue'
import { useRoomStore } from '@/stores/room'
import { p2pChunkManager } from '@/composables/useP2PChunk'
import { Crown } from 'lucide-vue-next'

const room = useRoomStore()

function getInitial(name: string): string {
  return name.charAt(0).toUpperCase()
}

// Build a map of userId -> color for P2P peers
const peerColorMap = computed(() => {
  const map = new Map<string, string>()
  for (const peer of p2pChunkManager.peerList) {
    map.set(peer.id, peer.color)
  }
  return map
})
</script>

<template>
  <div class="h-full overflow-y-auto p-4">
    <div class="space-y-1">
      <div
        v-for="user in room.users"
        :key="user.id"
        class="flex items-center gap-3 px-2 py-2 rounded transition-fast border-l-2"
        :class="user.id === room.userId ? 'bg-bg-sunken' : 'hover:bg-bg-sunken'"
        :style="{ borderLeftColor: peerColorMap.get(user.id) || 'transparent' }"
      >
        <!-- Avatar -->
        <div
          class="flex items-center justify-center w-8 h-8 rounded-full text-xs font-medium shrink-0"
          :class="user.isHost
            ? 'bg-accent text-fg-onAccent'
            : 'bg-bg-sunken text-fg-muted border border-line'"
        >
          {{ getInitial(user.name) }}
        </div>

        <!-- Name -->
        <span class="text-sm flex-1 truncate" :class="user.id === room.userId ? 'font-medium' : ''">
          {{ user.name }}
          <span v-if="user.id === room.userId" class="text-fg-subtle text-xs ml-1">（你）</span>
        </span>

        <!-- Host badge -->
        <span
          v-if="user.isHost"
          class="flex items-center gap-1 text-2xs text-fg-muted px-1.5 py-0.5 border border-line rounded"
        >
          <Crown :size="10" />
          房主
        </span>
      </div>

      <div
        v-if="room.users.length === 0"
        class="text-center text-sm text-fg-subtle py-8"
      >
        暂无用户
      </div>
    </div>
  </div>
</template>
