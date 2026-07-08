<script setup lang="ts">
import { useRoomStore } from '@/stores/room'
import ChatPanel from './ChatPanel.vue'
import UserList from './UserList.vue'
import HostPanel from './HostPanel.vue'
import { MessageSquare, Users as UsersIcon, Settings } from 'lucide-vue-next'

const room = useRoomStore()
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Tab bar -->
    <nav class="flex shrink-0 border-b border-line">
      <button
        class="flex-1 flex items-center justify-center gap-1.5 py-3 text-sm font-medium transition-fast"
        :class="room.activeTab === 'chat'
          ? 'text-fg border-b-2 border-accent'
          : 'text-fg-muted border-b-2 border-transparent hover:text-fg'"
        @click="room.activeTab = 'chat'"
      >
        <MessageSquare :size="15" />
        聊天
      </button>
      <button
        class="flex-1 flex items-center justify-center gap-1.5 py-3 text-sm font-medium transition-fast"
        :class="room.activeTab === 'users'
          ? 'text-fg border-b-2 border-accent'
          : 'text-fg-muted border-b-2 border-transparent hover:text-fg'"
        @click="room.activeTab = 'users'"
      >
        <UsersIcon :size="15" />
        用户
        <span class="text-xs text-fg-subtle">{{ room.userCount }}</span>
      </button>
      <button
        v-if="room.isHost"
        class="flex-1 flex items-center justify-center gap-1.5 py-3 text-sm font-medium transition-fast"
        :class="room.activeTab === 'host'
          ? 'text-fg border-b-2 border-accent'
          : 'text-fg-muted border-b-2 border-transparent hover:text-fg'"
        @click="room.activeTab = 'host'"
      >
        <Settings :size="15" />
        管理
      </button>
    </nav>

    <!-- Tab content -->
    <div class="flex-1 overflow-hidden">
      <ChatPanel v-show="room.activeTab === 'chat'" />
      <UserList v-show="room.activeTab === 'users'" />
      <HostPanel v-show="room.activeTab === 'host'" />
    </div>
  </div>
</template>
