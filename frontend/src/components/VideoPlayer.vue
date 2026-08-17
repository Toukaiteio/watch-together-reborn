<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import DPlayer from 'dplayer'
import { useRoomStore } from '@/stores/room'
import { p2pChunkManager } from '@/composables/useP2PChunk'
import { WindowFullscreen, WindowUnfullscreen, WindowIsFullscreen } from '../../wailsjs/runtime/runtime'
import { Monitor, Upload, Loader2, Gauge } from 'lucide-vue-next'
import MagnetFileSelector from './MagnetFileSelector.vue'

const room = useRoomStore()
const containerRef = ref<HTMLDivElement>()
const chunkProgressRef = ref<HTMLCanvasElement>()
let dp: any = null
let applyingRemote = false
let removeDocumentFullscreenListener: (() => void) | null = null
let remoteGuardTimer: ReturnType<typeof setTimeout> | null = null
let remoteGuardUntil = 0
let lastMemberNoticeAt = 0
let lastChunkSyncAt = 0
let fullscreenSyncTimers: ReturnType<typeof setTimeout>[] = []
let removeMemberHotkeyBlocker: (() => void) | null = null

const hasSource = computed(() =>
  room.videoState.sourceType === 'magnet'
    ? !!room.videoState.localBlobUrl
    : !!room.videoState.source || !!room.videoState.localBlobUrl
)
const shouldInitPlayer = computed(() => hasSource.value)
const hasChunkDistribution = computed(() => !!room.videoState.chunkManifest)
const shouldPreferSegmentedPlayback = computed(() =>
  !room.isHost &&
  !!room.videoState.chunkManifest &&
  room.videoState.sourceType === 'hls' &&
  room.videoState.source.startsWith('/video') &&
  p2pChunkManager.status.value !== 'error'
)
const resolvedPlaybackSource = computed(() =>
  room.videoState.localBlobUrl ||
  (shouldPreferSegmentedPlayback.value ? '' : room.resolveMediaRef(room.videoState.source))
)
const resolvedPlaybackSourceType = computed(() =>
  room.videoState.localBlobSourceType || room.videoState.sourceType
)
const canShowProgressOverlay = computed(() =>
  room.magnetPreparing ||
  room.localFilePreparing ||
  (hasChunkDistribution.value && (
    p2pChunkManager.isDownloading.value || p2pChunkManager.status.value === 'assembling'
  ))
)
const progressPercent = computed(() => {
  if (room.magnetPreparing) return 0
  if (room.localFilePreparing) {
    return Math.round(room.localFileProgress?.percent || 0)
  }
  return Math.round(p2pChunkManager.progress.value || 0)
})
const progressLabel = computed(() => {
  if (room.magnetPreparing) return '磁力准备中'
  if (room.localFilePreparing) return '预处理中'
  if (p2pChunkManager.status.value === 'assembling') return '拼装中'
  return '下载中'
})
const progressDetail = computed(() => {
  if (room.magnetPreparing) {
    return room.magnetStatusText || '正在获取磁力元数据'
  }
  if (room.localFilePreparing) {
    return room.localFileProgress?.stage === 'encoding'
      ? '正在切块与纠删码编码'
      : room.localFileProgress?.stage === 'hashing'
        ? '正在计算文件哈希'
        : '正在准备本地视频'
  }
  return `P2P: ${p2pChunkManager.p2pStats.value.fromPeers} | 房主: ${p2pChunkManager.p2pStats.value.fromHost} | 节点: ${p2pChunkManager.peerCount.value}`
})
const downloadSpeedText = computed(() => {
  const bytesPerSecond = p2pChunkManager.downloadStats.value.bytesPerSecond
  if (!bytesPerSecond || bytesPerSecond <= 0) return '计算中'
  if (bytesPerSecond >= 1024 * 1024) return `${(bytesPerSecond / 1024 / 1024).toFixed(2)} MB/s`
  if (bytesPerSecond >= 1024) return `${(bytesPerSecond / 1024).toFixed(1)} KB/s`
  return `${Math.round(bytesPerSecond)} B/s`
})
const showMemberFollowHint = computed(() =>
  shouldInitPlayer.value && !room.isHost
)

// Whether to show the chunk progress bar for local-file chunk distribution.
const showChunkProgress = computed(() => {
  if (!hasChunkDistribution.value) return false
  if (!dp || !dp.video) return false
  if (!dp.video.duration || dp.video.duration <= 0) return false
  return true
})

function getVideoConfig(source: string, sourceType: string) {
  const config: any = {
    url: source,
    type: 'auto',
  }
  if (sourceType === 'hls') {
    config.type = 'customHls'
    config.customType = {
      customHls: (video: HTMLVideoElement) => {
        import('hls.js').then(({ default: Hls }) => {
          if (Hls.isSupported()) {
            const hls = new Hls({
              lowLatencyMode: true,
              backBufferLength: 90,
            })
            hls.on(Hls.Events.ERROR, (_event, data) => {
              console.error('[player] hls error', data)
              if (data?.fatal) {
                switch (data.type) {
                  case Hls.ErrorTypes.NETWORK_ERROR:
                    hls.startLoad()
                    break
                  case Hls.ErrorTypes.MEDIA_ERROR:
                    hls.recoverMediaError()
                    break
                  default:
                    hls.destroy()
                    break
                }
              }
            })
            hls.loadSource(source)
            hls.attachMedia(video)
          } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
            video.src = source
          }
        })
      },
    }
  }
  return config
}

function fallbackToHostPlayback(reason: string) {
  if (!room.videoState.localBlobUrl || room.videoState.localBlobSourceType !== 'url' || !room.videoState.source) {
    return
  }
  console.warn('[player] fallback to host playback', {
    reason,
    source: room.videoState.source,
  })
  p2pChunkManager.status.value = 'error'
  p2pChunkManager.errorMessage.value = reason
  if (room.videoState.sourceType === 'magnet') {
    room.connectionError = `磁力视频播放失败: ${reason}`
  }
  room.clearPlaybackOverride()
}

function initPlayer() {
  if (!containerRef.value || !shouldInitPlayer.value) return

  if (dp) {
    dp.destroy()
    dp = null
  }

  const playbackSource = resolvedPlaybackSource.value
  if (!playbackSource) return

  dp = new DPlayer({
    container: containerRef.value,
    video: getVideoConfig(playbackSource, resolvedPlaybackSourceType.value),
    screenshot: false,
    hotkey: true,
    preload: 'auto',
    volume: 0.7,
    autoplay: false,
    mutex: true,
    playbackSpeed: [0.5, 0.75, 1, 1.25, 1.5, 2],
    danmaku: {
      id: room.roomId || 'watch-together-room',
      api: '/danmaku',
      user: room.currentUser?.name || 'guest',
      bottom: '15%',
    },
    apiBackend: {
      read: (_options: any) => Promise.resolve([]),
      send: (_options: any) => Promise.resolve(),
    },
  })

  const danmakuLoading = containerRef.value.querySelector('.dplayer-danloading') as HTMLElement | null
  if (danmakuLoading) {
    danmakuLoading.style.display = 'none'
  }

  dp.on('error', () => {
    fallbackToHostPlayback('dplayer-error')
  })
  dp.video?.addEventListener('error', () => {
    fallbackToHostPlayback('video-error')
  })

  dp.on('play', () => {
    if (!room.isHost && !isRemoteGuardActive()) {
      beginRemoteGuard(250)
      dp.pause()
      showMemberNotice('只有房主可以播放或暂停，音量和全屏仍可自行调整', 1800)
      return
    }
    if (!isRemoteGuardActive()) {
      room.sendVideoPlay(true, dp.video.currentTime)
    }
  })

  dp.on('pause', () => {
    if (!room.isHost && !isRemoteGuardActive()) {
      const shouldResume = room.videoState.playing
      if (shouldResume) {
        beginRemoteGuard(350)
        dp.play().catch(() => {})
      }
      showMemberNotice('只有房主可以播放或暂停，音量和全屏仍可自行调整', 1800)
      return
    }
    if (!isRemoteGuardActive()) {
      room.sendVideoPlay(false, dp.video.currentTime)
    }
  })

  dp.on('seeked', () => {
    if (!room.isHost && !isRemoteGuardActive()) {
      beginRemoteGuard(450)
      dp.seek(room.videoState.currentTime || 0)
      showMemberNotice('只有房主可以拖动进度条', 1500)
      return
    }
    if (!isRemoteGuardActive()) {
      room.sendVideoSeek(dp.video.currentTime)
    }
    // Notify P2P manager of seek position for priority download scheduling
    if (hasChunkDistribution.value && dp.video.duration > 0) {
      const manifest = p2pChunkManager.getManifest()
      if (manifest) {
        const mediaChunk = [...manifest.chunks]
          .filter((chunk) => !chunk.isInit)
          .find((chunk, index, arr) => {
            const endTime = chunk.startTime + (chunk.duration || (arr[index + 1]?.startTime ?? dp.video.duration) - chunk.startTime)
            return dp.video.currentTime >= chunk.startTime && dp.video.currentTime < Math.max(endTime, chunk.startTime + 0.01)
          })
        if (mediaChunk) {
          p2pChunkManager.setSeekGroup(mediaChunk.index)
        }
      }
    }
  })

  dp.on('ratechange', () => {
    if (!room.isHost && !isRemoteGuardActive()) {
      dp.video.playbackRate = room.videoState.speed || 1
      showMemberNotice('只有房主可以调整倍速', 1500)
      return
    }
    if (!isRemoteGuardActive()) {
      room.sendVideoSpeed(dp.video.playbackRate)
    }
  })

  dp.on('loadedmetadata', () => {
    showMemberPlaybackBadge()
    if (!room.isHost) {
      beginRemoteGuard(700)
      if (room.videoState.currentTime > 0) {
        dp.seek(room.videoState.currentTime)
      }
      if (room.videoState.playing) {
        dp.play().catch(() => {})
      } else {
        dp.pause()
      }
      dp.video.playbackRate = room.videoState.speed || 1
    }
    // Initial draw of chunk progress bar
    nextTick(() => drawChunkProgress())
  })

  // Redraw on timeupdate for playhead movement
  dp.on('timeupdate', () => {
    throttledDraw()
    if (
      room.isHost &&
      hasChunkDistribution.value &&
      !isRemoteGuardActive() &&
      !dp.video.paused
    ) {
      const now = Date.now()
      if (now - lastChunkSyncAt >= 1000) {
        lastChunkSyncAt = now
        room.sendVideoSeek(dp.video.currentTime)
      }
    }
  })

  bindFullscreenControls()
  bindMemberRestrictions()
  syncFullscreenState()
  showMemberPlaybackBadge()
}

function bindMemberRestrictions() {
  if (!containerRef.value || room.isHost) return

  const playButton = containerRef.value.querySelector('.dplayer-play-icon') as HTMLButtonElement | null
  const mobilePlayButton = containerRef.value.querySelector('.dplayer-mobile-play') as HTMLButtonElement | null
  const barWrap = containerRef.value.querySelector('.dplayer-bar-wrap') as HTMLElement | null
  const videoWrap = containerRef.value.querySelector('.dplayer-video-wrap') as HTMLElement | null
  const bezel = containerRef.value.querySelector('.dplayer-bezel') as HTMLElement | null
  const speedSetting = containerRef.value.querySelector('.dplayer-setting-speed') as HTMLElement | null
  const speedItems = containerRef.value.querySelectorAll('.dplayer-setting-speed-item')

  const stopEvent = (event: Event) => {
    event.preventDefault()
    event.stopPropagation()
    showMemberNotice('只有房主可以控制播放进度', 1500)
  }

  const markLocked = (element: HTMLElement | null, message: string) => {
    if (!element) return
    element.classList.add('wt-member-control-locked')
    element.setAttribute('aria-disabled', 'true')
    element.setAttribute('title', message)
  }

  markLocked(playButton, '跟随房主播放：房主控制播放和暂停')
  markLocked(mobilePlayButton, '跟随房主播放：房主控制播放和暂停')
  markLocked(barWrap, '跟随房主播放：房主控制进度')
  markLocked(videoWrap, '跟随房主播放：房主控制播放和暂停')
  markLocked(bezel, '跟随房主播放：房主控制播放和暂停')
  markLocked(speedSetting, '跟随房主播放：房主控制播放速度')
  speedItems.forEach((item) => markLocked(item as HTMLElement, '跟随房主播放：房主控制播放速度'))

  const stopPlayEvent = (event: Event) => {
    event.preventDefault()
    event.stopPropagation()
    showMemberNotice('跟随房主播放；音量和全屏仍可自行调整', 1800)
  }

  playButton?.addEventListener('click', stopPlayEvent, true)
  mobilePlayButton?.addEventListener('click', stopPlayEvent, true)
  videoWrap?.addEventListener('click', stopPlayEvent, true)
  bezel?.addEventListener('click', stopPlayEvent, true)
  barWrap?.addEventListener('mousedown', stopEvent, true)
  barWrap?.addEventListener('click', stopEvent, true)
  speedSetting?.addEventListener('click', stopEvent, true)
  speedItems.forEach((item) => item.addEventListener('click', stopEvent, true))

  const blockMemberHotkeys = (event: KeyboardEvent) => {
    if (event.altKey || event.ctrlKey || event.metaKey || event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return
    if (event.key !== ' ' && event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    event.stopPropagation()
    showMemberNotice('跟随房主播放；进度由房主统一控制', 1500)
  }
  document.addEventListener('keydown', blockMemberHotkeys, true)
  removeMemberHotkeyBlocker = () => document.removeEventListener('keydown', blockMemberHotkeys, true)
}

function showMemberPlaybackBadge() {
  if (!containerRef.value || room.isHost) return
  const existing = containerRef.value.querySelector('.wt-member-badge')
  if (existing) return

  const rightIcons = containerRef.value.querySelector('.dplayer-icons-right')
  if (!rightIcons) return

  const badge = document.createElement('div')
  badge.className = 'wt-member-badge'
  badge.innerHTML = `
    <span class="wt-member-badge-lock">跟随房主播放</span>
    <span class="wt-member-badge-sep">|</span>
    <span class="wt-member-badge-volume">音量和全屏可自行调整</span>
  `
  rightIcons.prepend(badge)
}

function bindFullscreenControls() {
  if (!containerRef.value || !dp) return

  const browserFullButton = containerRef.value.querySelector('.dplayer-full-icon') as HTMLButtonElement | null
  const webFullButton = containerRef.value.querySelector('.dplayer-full-in-icon') as HTMLButtonElement | null

  browserFullButton?.addEventListener('click', async (event) => {
    event.preventDefault()
    event.stopPropagation()
    const fullscreen = await WindowIsFullscreen()
    if (fullscreen) {
      room.setPlayerFullscreen(false)
      await WindowUnfullscreen()
    } else {
      room.setPlayerFullscreen(true)
      await WindowFullscreen()
    }
    queueFullscreenResync()
  }, true)

  webFullButton?.addEventListener('click', async (event) => {
    event.preventDefault()
    event.stopPropagation()
    const wrapper = containerRef.value
    if (!wrapper) return
    if (isPlayerElementFullscreen(wrapper)) {
      room.setPlayerFullscreen(false)
      await document.exitFullscreen()
    } else {
      room.setPlayerFullscreen(true)
      await wrapper.requestFullscreen()
    }
    queueFullscreenResync()
  }, true)
}

async function syncFullscreenState() {
  const fullscreen = await WindowIsFullscreen().catch(() => false)
  room.setPlayerFullscreen(fullscreen || isPlayerElementFullscreen(containerRef.value))
}

async function exitAllFullscreenModes() {
  const playerElementFullscreen = isPlayerElementFullscreen(containerRef.value)
  if (playerElementFullscreen && document.fullscreenElement) {
    try {
      await document.exitFullscreen()
    } catch {
      // Ignore teardown failures during route/room reset.
    }
  }

  const windowFullscreen = await WindowIsFullscreen().catch(() => false)
  if (windowFullscreen) {
    try {
      await WindowUnfullscreen()
    } catch {
      // Ignore teardown failures during route/room reset.
    }
  }

  room.setPlayerFullscreen(false)
}

function isPlayerElementFullscreen(container?: HTMLDivElement) {
  const fullscreenElement = document.fullscreenElement
  if (!container || !fullscreenElement) return false
  return fullscreenElement === container ||
    container.contains(fullscreenElement) ||
    fullscreenElement.contains(container)
}

function beginRemoteGuard(durationMs = 300) {
  applyingRemote = true
  remoteGuardUntil = Math.max(remoteGuardUntil, performance.now() + durationMs)
  if (remoteGuardTimer) {
    clearTimeout(remoteGuardTimer)
  }
  removeMemberHotkeyBlocker?.()
  removeMemberHotkeyBlocker = null
  remoteGuardTimer = setTimeout(() => {
    if (performance.now() >= remoteGuardUntil) {
      applyingRemote = false
    }
  }, durationMs + 30)
}

function isRemoteGuardActive() {
  return applyingRemote && performance.now() < remoteGuardUntil
}

function showMemberNotice(message: string, durationMs: number) {
  if (!dp) return
  const now = Date.now()
  if (now - lastMemberNoticeAt < 1200) return
  lastMemberNoticeAt = now
  dp.notice(message, durationMs)
}

function queueFullscreenResync() {
  for (const timer of fullscreenSyncTimers) {
    clearTimeout(timer)
  }
  fullscreenSyncTimers = [
    setTimeout(() => { void syncFullscreenState() }, 50),
    setTimeout(() => { void syncFullscreenState() }, 180),
  ]
}

// --- Chunk progress bar rendering ---
function getSeekBarRect(): DOMRect | null {
  if (!containerRef.value) return null
  const seekBar = containerRef.value.querySelector('.dplayer-bar-wrap') as HTMLElement
  if (!seekBar) return null
  return seekBar.getBoundingClientRect()
}

function getContainerRect(): DOMRect | null {
  return containerRef.value?.getBoundingClientRect() ?? null
}

function drawChunkProgress() {
  if (!chunkProgressRef.value || !dp?.video || !showChunkProgress.value) return
  const canvas = chunkProgressRef.value
  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const containerRect = getContainerRect()
  const seekBarRect = getSeekBarRect()
  if (!containerRect || !seekBarRect) return

  // Size canvas to match seek bar
  const dpr = window.devicePixelRatio || 1
  const width = seekBarRect.width
  const canvasHeight = 4 // px, thin bar

  canvas.width = width * dpr
  canvas.height = canvasHeight * dpr
  canvas.style.width = width + 'px'
  canvas.style.height = canvasHeight + 'px'
  canvas.style.position = 'absolute'
  canvas.style.left = (seekBarRect.left - containerRect.left) + 'px'
  canvas.style.bottom = (containerRect.bottom - seekBarRect.bottom + 2) + 'px'
  canvas.style.pointerEvents = 'none'
  canvas.style.zIndex = '10'
  ctx.scale(dpr, dpr)

  const duration = dp.video.duration
  const manifest = p2pChunkManager.getManifest()
  const chunks = manifest?.chunks ?? []
  const totalChunks = chunks.filter((chunk) => !chunk.isInit).length

  if (totalChunks === 0 || duration <= 0) {
    ctx.clearRect(0, 0, width, canvasHeight)
    return
  }

  // Background (undownloaded)
  ctx.fillStyle = 'rgba(255,255,255,0.15)'
  ctx.fillRect(0, 0, width, canvasHeight)

  // Draw each owned chunk as a colored segment
  const chunkColorMap = p2pChunkManager.chunkColorMap
  for (const [chunkIdx, color] of chunkColorMap) {
    const chunk = chunks.find((item) => item.index === chunkIdx)
    if (!chunk || chunk.isInit) continue

    const startTime = chunk.startTime
    const endTime = Math.min(startTime + chunk.duration, duration)
    const xStart = (startTime / duration) * width
    const xEnd = (endTime / duration) * width

    ctx.fillStyle = color
    ctx.fillRect(xStart, 0, Math.max(xEnd - xStart, 1), canvasHeight)
  }

  // Draw playhead position
  const currentTime = dp.video.currentTime
  const playX = (currentTime / duration) * width
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(playX - 0.5, 0, 1.5, canvasHeight)
}

// Throttled redraw (max 10fps during download)
let lastDrawTime = 0
function throttledDraw() {
  const now = Date.now()
  if (now - lastDrawTime < 100) return
  lastDrawTime = now
  drawChunkProgress()
}

// Watch chunk color map changes (reactive)
watch(() => p2pChunkManager.chunkColorMap.size, () => {
  drawChunkProgress()
})

// Watch video timeupdate for playhead
watch(() => room.videoState.currentTime, () => {
  throttledDraw()
})

// Watch source changes — re-init player
watch(
  () => [resolvedPlaybackSource.value, resolvedPlaybackSourceType.value],
  () => {
    if (shouldInitPlayer.value) {
      nextTick(() => initPlayer())
    } else if (dp) {
      dp.destroy()
      dp = null
    }
  }
)

// Watch playing state (members follow host)
watch(() => room.videoState.playing, (playing) => {
  if (room.isHost || !dp) return
  beginRemoteGuard(500)
  if (playing) {
    dp.play().catch(() => {})
  } else {
    dp.pause()
  }
})

// Requests from the room readiness panel are deliberately handled by the
// active player instance, so the normal DPlayer event path still broadcasts
// the play state to every member.
watch(() => room.playbackRequest, () => {
  if (!room.isHost || !dp) return
  dp.play().catch(() => {})
})

// Watch currentTime (members follow host seek)
watch(() => room.videoState.currentTime, (time) => {
  if (room.isHost || !dp) return
  const diff = Math.abs(dp.video.currentTime - time)
  if (diff > 0.5) {
    beginRemoteGuard(500)
    dp.seek(time)
  }
})

// Watch speed (members follow host)
watch(() => room.videoState.speed, (speed) => {
  if (room.isHost || !dp) return
  beginRemoteGuard(250)
  dp.video.playbackRate = speed
})

// Watch danmaku triggers
watch(() => room.danmakuTrigger, (trigger) => {
  if (!trigger || !dp) return
  try {
    dp.danmaku?.play?.()
    dp.danmaku.draw({
      text: trigger.text,
      color: trigger.color,
      type: 'right',
    })
  } catch {
    // Danmaku module not ready
  }
})

onMounted(() => {
  if (shouldInitPlayer.value) {
    nextTick(() => initPlayer())
  }
  // Redraw chunk progress on resize
  window.addEventListener('resize', drawChunkProgress)
  const onDocumentFullscreenChange = () => {
    syncFullscreenState()
  }
  document.addEventListener('fullscreenchange', onDocumentFullscreenChange)
  removeDocumentFullscreenListener = () => {
    document.removeEventListener('fullscreenchange', onDocumentFullscreenChange)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', drawChunkProgress)
  removeDocumentFullscreenListener?.()
  if (remoteGuardTimer) {
    clearTimeout(remoteGuardTimer)
    remoteGuardTimer = null
  }
  for (const timer of fullscreenSyncTimers) {
    clearTimeout(timer)
  }
  fullscreenSyncTimers = []
  if (dp) {
    dp.destroy()
    dp = null
  }
  void exitAllFullscreenModes()
})
</script>

<template>
  <div class="relative h-full w-full flex items-center justify-center bg-bg-sunken">
    <!-- DPlayer container -->
    <div
      v-show="shouldInitPlayer"
      ref="containerRef"
      class="w-full h-full"
    />

    <!-- Empty state -->
    <div
      v-if="!shouldInitPlayer"
      class="flex flex-col items-center gap-4 text-fg-subtle"
    >
      <Monitor :size="48" :stroke-width="1" />
      <p class="text-sm">
        {{
          room.magnetPreparing
            ? (room.magnetStatusText || '正在获取磁力元数据')
            : room.videoState.sourceType === 'magnet' && room.magnetFiles.length > 1
              ? (room.isHost ? '请选择要播放的视频' : '等待房主选择要播放的视频')
              : room.isHost ? '请在右侧管理面板设置视频源' : '等待房主设置视频源'
        }}
      </p>
      <MagnetFileSelector
        v-if="room.videoState.sourceType === 'magnet' && room.magnetFiles.length > 1"
        :files="room.magnetFiles"
        :disabled="!room.isHost"
        @select="room.selectMagnetFile"
      />
      <button
        v-if="room.isHost"
        class="btn-ghost"
        @click="room.activeTab = 'host'"
      >
        <Upload :size="16" />
        前往设置
      </button>
    </div>

    <!-- P2P download progress -->
    <div
      v-if="canShowProgressOverlay"
      class="absolute top-3 right-3 flex flex-col gap-1 text-xs text-white/80 px-3 py-2 rounded bg-black/50 pointer-events-none min-w-[140px]"
    >
      <div class="flex items-center gap-2">
        <Loader2 :size="12" class="animate-spin" />
        {{ progressLabel }}<template v-if="!room.magnetPreparing"> {{ progressPercent }}%</template>
      </div>
      <div class="h-1.5 rounded bg-white/15 overflow-hidden">
        <div
          class="h-full bg-white/80 transition-all duration-200"
          :style="{ width: `${Math.max(0, Math.min(100, progressPercent))}%` }"
        />
      </div>
      <div class="text-white/50 text-2xs">
        {{ progressDetail }}
      </div>
      <div
        v-if="!room.localFilePreparing && !room.magnetPreparing"
        class="flex items-center gap-1 text-white/55 text-2xs"
      >
        <Gauge :size="11" />
        下载速度 {{ downloadSpeedText }}
      </div>
    </div>

    <!-- P2P ready stats -->
    <div
      v-if="hasChunkDistribution && p2pChunkManager.status.value === 'ready' && p2pChunkManager.p2pRatio.value > 0"
      class="absolute top-3 right-3 text-2xs text-white/60 px-2 py-1 rounded bg-black/40 pointer-events-none"
    >
      P2P 分担 {{ p2pChunkManager.p2pRatio.value }}%
    </div>

    <!-- Chunk progress overlay (follows DPlayer seek bar) -->
    <canvas
      v-show="showChunkProgress && shouldInitPlayer"
      ref="chunkProgressRef"
      class="chunk-progress-canvas"
    />
  </div>
</template>

<style scoped>
:deep(.dplayer) {
  width: 100% !important;
  height: 100% !important;
}

:deep(.dplayer-video) {
  object-fit: contain !important;
  width: 100% !important;
  height: 100% !important;
}

:deep(.dplayer-comment) {
  display: none !important;
}

:deep(.dplayer-controller-mask) {
  display: none !important;
}

:deep(.dplayer-comment-setting-box),
:deep(.dplayer-comment-input),
:deep(.dplayer-send-icon) {
  display: none !important;
}

:deep(.dplayer-controller) {
  background: linear-gradient(transparent, rgba(0, 0, 0, 0.6)) !important;
}

:deep(.dplayer-danloading) {
  display: none !important;
}

:deep(.wt-member-badge) {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  margin-right: auto;
  margin-left: 0.75rem;
  color: rgba(255, 255, 255, 0.72);
  font-size: 11px;
  pointer-events: none;
}

:deep(.wt-member-badge-sep) {
  color: rgba(255, 255, 255, 0.3);
}

:deep(.wt-member-badge-lock),
:deep(.wt-member-badge-volume) {
  white-space: nowrap;
}

:deep(.wt-member-control-locked) {
  cursor: not-allowed !important;
  opacity: 0.48;
}

:deep(.dplayer-bar-wrap.wt-member-control-locked),
:deep(.dplayer-setting-speed.wt-member-control-locked) {
  filter: grayscale(0.75);
}

:deep(.dplayer-video-wrap.wt-member-control-locked),
:deep(.dplayer-bezel.wt-member-control-locked) {
  cursor: default;
}

 :deep(.dplayer-mask) {
   display: none !important;
 }
</style>
