<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { useChatStore } from '@/stores/chat'
import { useRoomStore } from '@/stores/room'
import { Send } from 'lucide-vue-next'

const chat = useChatStore()
const room = useRoomStore()

const inputText = ref('')
const messagesRef = ref<HTMLDivElement>()

function handleSend() {
  const text = inputText.value.trim()
  if (!text) return
  room.sendChat(text)
  inputText.value = ''
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// Auto-scroll to bottom on new messages
watch(
  () => chat.messages.length,
  () => {
    nextTick(() => {
      if (messagesRef.value) {
        messagesRef.value.scrollTop = messagesRef.value.scrollHeight
      }
    })
  }
)
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Messages -->
    <div
      ref="messagesRef"
      class="flex-1 overflow-y-auto px-4 py-3 space-y-3"
    >
      <div v-if="chat.messages.length === 0" class="flex items-center justify-center h-full text-sm text-fg-subtle">
        暂无消息
      </div>
      <div
        v-for="msg in chat.messages"
        :key="msg.timestamp + msg.userId"
        class="flex flex-col gap-0.5"
        :class="msg.isSystem ? 'items-center py-1' : (msg.isSelf ? 'items-end' : 'items-start')"
      >
        <template v-if="msg.isSystem">
          <span class="max-w-[92%] text-center text-2xs leading-5 text-fg-subtle">{{ msg.text }}</span>
        </template>
        <template v-else>
          <div class="flex items-center gap-2 text-2xs text-fg-subtle">
            <span :class="msg.isSelf ? 'text-fg-subtle' : 'text-fg-muted font-medium'">
              {{ msg.isSelf ? '我' : msg.username }}
            </span>
            <span>{{ formatTime(msg.timestamp) }}</span>
          </div>
          <div
            class="text-sm px-3 py-1.5 rounded max-w-[85%] break-words"
            :class="msg.isSelf
              ? 'bg-accent text-fg-onAccent'
              : 'bg-bg-sunken text-fg'"
          >
            {{ msg.text }}
          </div>
        </template>
      </div>
    </div>

    <!-- Input -->
    <div class="shrink-0 border-t border-line p-3">
      <div class="flex items-center gap-2">
        <input
          v-model="inputText"
          type="text"
          placeholder="发送消息..."
          class="input flex-1"
          maxlength="200"
          @keyup.enter="handleSend"
        />
        <button
          class="btn px-3"
          :disabled="!inputText.trim()"
          @click="handleSend"
        >
          <Send :size="15" />
        </button>
      </div>
    </div>
  </div>
</template>
