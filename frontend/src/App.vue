<script setup lang="ts">
import { onMounted } from 'vue'
import { useRoomStore } from '@/stores/room'
import TitleBar from '@/components/TitleBar.vue'
import HomeView from '@/views/HomeView.vue'
import RoomView from '@/views/RoomView.vue'

const room = useRoomStore()

onMounted(() => {
  room.init()
})
</script>

<template>
  <div class="flex h-full w-full flex-col overflow-hidden bg-bg-base text-fg">
    <TitleBar v-if="!room.playerFullscreen" />
    <main class="min-h-0 flex-1 overflow-hidden">
      <HomeView v-if="room.view === 'home'" />
      <RoomView v-else />
    </main>
  </div>
</template>
