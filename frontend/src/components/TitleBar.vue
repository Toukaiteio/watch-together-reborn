<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoomStore } from '@/stores/room'
import { useSettingsStore } from '@/stores/settings'
import {
  Sun,
  Moon,
  Minus,
  Square,
  Copy,
  X,
  LogOut,
  AlertTriangle,
  Users,
} from 'lucide-vue-next'
import {
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  Quit,
} from '../../wailsjs/runtime/runtime'

const room = useRoomStore()
const settings = useSettingsStore()
const isMaximised = ref(false)
let pollTimer: ReturnType<typeof setInterval> | null = null

const isInRoom = computed(() => room.view === 'room')
const showPortWarning = computed(() =>
  room.isHost && !room.isDefaultPort && isInRoom.value,
)

async function syncMaximised() {
  isMaximised.value = await WindowIsMaximised()
}

onMounted(() => {
  syncMaximised()
  pollTimer = setInterval(syncMaximised, 500)
})

onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})

function handleMinimise() {
  WindowMinimise()
}

async function handleToggleMaximise() {
  await WindowToggleMaximise()
  await syncMaximised()
}

function handleClose() {
  Quit()
}
</script>

<template>
  <header class="title-bar flex h-9 shrink-0 items-stretch border-b border-line bg-bg-elevated">
    <div
      class="title-bar__drag flex min-w-0 flex-1 items-center gap-2 px-3"
      style="--wails-draggable: drag"
    >
      <div class="title-bar__icon flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-accent text-[10px] font-bold text-fg-onAccent">
        W
      </div>
      <span class="shrink-0 text-xs font-medium text-fg-muted">Watch Together</span>

      <template v-if="isInRoom">
        <span class="title-bar__sep shrink-0" />

        <div class="flex min-w-0 items-center gap-1.5 text-xs">
          <span class="shrink-0 text-fg-subtle">房间</span>
          <span class="truncate font-mono font-medium text-fg">{{ room.roomId }}</span>
        </div>

        <span class="title-bar__sep shrink-0" />

        <div class="flex shrink-0 items-center gap-1 text-xs text-fg-muted">
          <Users :size="12" />
          <span>{{ room.userCount }}</span>
        </div>

        <div
          v-if="showPortWarning"
          class="title-bar__port-warn flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-danger"
        >
          <AlertTriangle :size="10" />
          <span>端口 {{ room.serverPort }}</span>
        </div>
      </template>
    </div>

    <div class="title-bar__actions flex shrink-0 items-stretch" style="--wails-draggable: no-drag">
      <template v-if="isInRoom">
        <div class="title-bar__status flex items-center gap-2.5 px-2.5 text-[10px] text-fg-muted">
          <span class="flex items-center gap-1">
            <span
              class="inline-block h-1.5 w-1.5 rounded-full"
              :class="room.wsConnected ? 'bg-fg' : 'bg-fg-subtle'"
            />
            WS
          </span>
          <span class="flex items-center gap-1">
            <span
              class="inline-block h-1.5 w-1.5 rounded-full"
              :class="room.webrtcConnected ? 'bg-fg' : 'bg-fg-subtle'"
            />
            P2P
          </span>
        </div>

        <button
          class="title-bar__action gap-1 px-2.5 text-xs text-fg-muted transition-fast hover:bg-bg-sunken hover:text-danger"
          title="退出房间"
          @click="room.leaveRoom()"
        >
          <LogOut :size="13" />
          <span class="hidden sm:inline">退出</span>
        </button>

        <span class="title-bar__sep-v shrink-0" />
      </template>

      <button
        class="title-bar__action px-2.5 text-fg-muted transition-fast hover:bg-bg-sunken hover:text-fg"
        title="切换主题"
        @click="settings.toggleTheme()"
      >
        <Sun v-if="settings.theme === 'dark'" :size="14" />
        <Moon v-else :size="14" />
      </button>
      <button
        class="title-bar__action px-3 text-fg-muted transition-fast hover:bg-bg-sunken hover:text-fg"
        title="最小化"
        @click="handleMinimise"
      >
        <Minus :size="14" />
      </button>
      <button
        class="title-bar__action px-3 text-fg-muted transition-fast hover:bg-bg-sunken hover:text-fg"
        :title="isMaximised ? '还原' : '最大化'"
        @click="handleToggleMaximise"
      >
        <Copy v-if="isMaximised" :size="12" />
        <Square v-else :size="12" />
      </button>
      <button
        class="title-bar__action title-bar__action--close px-3 text-fg-muted transition-fast hover:bg-danger hover:text-danger-fg"
        title="关闭"
        @click="handleClose"
      >
        <X :size="14" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.title-bar__action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.title-bar__sep {
  width: 1px;
  height: 12px;
  background: var(--color-line);
}

.title-bar__sep-v {
  width: 1px;
  align-self: stretch;
  margin-block: 6px;
  background: var(--color-line);
}

.title-bar__port-warn {
  border: 1px solid color-mix(in srgb, var(--color-danger-rgb) 30%, transparent);
  background: color-mix(in srgb, var(--color-danger-rgb) 8%, transparent);
}
</style>