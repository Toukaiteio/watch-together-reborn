<script setup lang="ts">
import { computed } from 'vue'
import { useRoomStore } from '@/stores/room'
import { p2pChunkManager } from '@/composables/useP2PChunk'
import { CircleAlert, CircleCheck, Crown, Download, LoaderCircle, Radio, WifiOff } from 'lucide-vue-next'

const room = useRoomStore()

function getInitial(name: string): string {
  return name.charAt(0).toUpperCase()
}

function formatRate(bytesPerSecond: number): string {
  if (!bytesPerSecond || bytesPerSecond <= 0) return '等待数据'
  if (bytesPerSecond >= 1024 * 1024) return `${(bytesPerSecond / 1024 / 1024).toFixed(1)} MB/s`
  if (bytesPerSecond >= 1024) return `${Math.round(bytesPerSecond / 1024)} KB/s`
  return `${Math.round(bytesPerSecond)} B/s`
}

function formatBuffer(seconds: number): string {
  if (!seconds || seconds <= 0) return '缓存建立中'
  if (seconds >= 60) return `缓存 ${Math.floor(seconds / 60)} 分 ${Math.round(seconds % 60)} 秒`
  return `缓存 ${Math.round(seconds)} 秒`
}

// Build a map of userId -> color for P2P peers
const peerColorMap = computed(() => {
  const map = new Map<string, string>()
  for (const peer of p2pChunkManager.peerList) {
    map.set(peer.id, peer.color)
  }
  return map
})

// Depend on the reactive status map so the list re-renders when peer snapshots update.
const memberStatuses = computed(() => {
  void room.p2pDownloadStatuses.size
  const map = new Map<string, ReturnType<typeof room.getMemberPlaybackStatus>>()
  for (const user of room.users) {
    // Touch each entry so Vue tracks Map mutations for known members.
    void room.p2pDownloadStatuses.get(user.id)
    map.set(user.id, room.getMemberPlaybackStatus(user.id))
  }
  return map
})

const sortedUsers = computed(() =>
  [...room.users].sort((a, b) => {
    if (a.isHost !== b.isHost) return a.isHost ? -1 : 1
    const aPriority = memberStatuses.value.get(a.id)?.priority ?? 4
    const bPriority = memberStatuses.value.get(b.id)?.priority ?? 4
    return aPriority - bPriority
  })
)

function statusIcon(userId: string) {
  const state = memberStatuses.value.get(userId)?.state
  if (state === 'host') return Radio
  if (state === 'ready') return CircleCheck
  if (state === 'error') return CircleAlert
  if (state === 'waiting') return WifiOff
  if (state === 'buffering') return Download
  return LoaderCircle
}

function statusClass(userId: string) {
  switch (memberStatuses.value.get(userId)?.state) {
    case 'ready': return 'text-accent'
    case 'error': return 'text-danger'
    case 'buffering': return 'text-fg-muted'
    default: return 'text-fg-subtle'
  }
}

function statusLabel(userId: string) {
  return memberStatuses.value.get(userId)?.label || '等待状态'
}

function statusDetail(userId: string) {
  return memberStatuses.value.get(userId)?.detail || ''
}

function statusState(userId: string) {
  return memberStatuses.value.get(userId)?.state
}
</script>

<template>
  <div class="h-full overflow-y-auto p-4">
    <div class="space-y-1">
      <div
        v-for="user in sortedUsers"
        :key="user.id"
        class="flex items-start gap-3 px-2 py-2 rounded transition-fast border-l-2"
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

        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1.5">
            <span class="text-sm truncate" :class="user.id === room.userId ? 'font-medium' : ''">
              {{ user.name }}
            </span>
            <span v-if="user.id === room.userId" class="text-fg-subtle text-xs shrink-0">（你）</span>
          </div>
          <div class="mt-1 flex min-w-0 items-center gap-1.5 text-2xs text-fg-subtle">
            <component
              :is="statusIcon(user.id)"
              :size="11"
              class="shrink-0"
              :class="[statusClass(user.id), statusState(user.id) === 'catching_up' ? 'animate-spin' : '']"
            />
            <span :class="statusClass(user.id)">{{ statusLabel(user.id) }}</span>
            <template v-if="room.getP2PDownloadStatus(user.id)?.state === 'downloading' || room.getP2PDownloadStatus(user.id)?.state === 'ready'">
              <span>{{ Math.round(room.getP2PDownloadStatus(user.id)?.progress || 0) }}%</span>
              <span>·</span>
              <span>{{ formatRate(room.getP2PDownloadStatus(user.id)?.bytesPerSecond || 0) }}</span>
              <span>·</span>
              <span class="truncate">{{ formatBuffer(room.getP2PDownloadStatus(user.id)?.bufferedSeconds || 0) }}</span>
            </template>
            <span v-else class="truncate">{{ statusDetail(user.id) }}</span>
          </div>
        </div>

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
        v-if="sortedUsers.length === 0"
        class="text-center text-sm text-fg-subtle py-8"
      >
        暂无用户
      </div>
    </div>
  </div>
</template>
