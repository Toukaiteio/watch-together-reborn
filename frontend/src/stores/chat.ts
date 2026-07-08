import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { ChatMessage } from '@/types'

export const useChatStore = defineStore('chat', () => {
  const messages = ref<ChatMessage[]>([])

  function addMessage(msg: ChatMessage) {
    messages.value.push(msg)
    if (messages.value.length > 500) {
      messages.value = messages.value.slice(-500)
    }
  }

  function clear() {
    messages.value = []
  }

  return { messages, addMessage, clear }
})
