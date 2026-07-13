import { defineStore } from 'pinia'
import { ref, computed, reactive } from 'vue'
import type {
  View, UserInfo, VideoState, WSMessage, VideoSourceType, SidebarTab, ChunkingProgress, IPv6AddrInfo, MagnetStreamStatus, LANRoomInfo, ChunkManifest, RoomPasscode, SecureInvite, P2PDownloadStatus,
} from '@/types'
import { wsClient } from '@/composables/useWebSocket'
import { webrtcManager } from '@/composables/useWebRTC'
import { p2pChunkManager } from '@/composables/useP2PChunk'
import { useChatStore } from './chat'
import { useSettingsStore } from './settings'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { buildHttpURL, buildWsURL, parseHostPortInput } from '@/utils/network'
import { pickPreferredHost, probeHosts } from '@/utils/hostProbe'
import { createSecureInvite, isSecureInvite } from '@/utils/secureInvite'

export const useRoomStore = defineStore('room', () => {
  const chatStore = useChatStore()
  const settings = useSettingsStore()

  // ---- State ----
  const view = ref<View>('home')
  const activeTab = ref<SidebarTab>('chat')

  const isHost = ref(false)
  const hostIp = ref('localhost')
  const serverPort = ref<number | null>(null)
  const localServerPort = ref<number | null>(null)
  const isDefaultPort = ref(true)
  const wsConnected = ref(false)
  const webrtcConnected = ref(false)
  const connectionError = ref<string | null>(null)
  const isConnecting = ref(false)

  const roomId = ref<string | null>(null)
  const userId = ref<string | null>(null)
  const hostId = ref<string | null>(null)
  const users = ref<UserInfo[]>([])
  const passcodes = ref<RoomPasscode[]>([])
  const localIPs = ref<string[]>([])
  const localIPv6s = ref<string[]>([])
  const ipv6Addresses = ref<IPv6AddrInfo[]>([])
  const hostCandidates = ref<string[]>([])
  const lanRooms = ref<LANRoomInfo[]>([])
  const lanDiscovering = ref(false)
  const preferredShareIp = ref('')
  const localFilePreparing = ref(false)
  const localFileProgress = ref<ChunkingProgress | null>(null)
  const magnetPreparing = ref(false)
  const magnetStatusText = ref('')
  const playerFullscreen = ref(false)
  const secureInvite = ref<SecureInvite | null>(null)
  const p2pDownloadStatuses = reactive(new Map<string, P2PDownloadStatus>())

  const videoState = ref<VideoState>({
    source: '',
    sourceType: 'url',
    playing: false,
    currentTime: 0,
    speed: 1,
  })

  const danmakuTrigger = ref<{ id: number; text: string; color: string } | null>(null)
  let danmakuSeq = 0
  let lastAppliedSource = ''
  let wasHost = false
  let suppressCloseHandling = false
  let probeGeneration = 0
  let magnetSessionSeq = 0
  let signalingRelayUrl = ''
  let lastChunkManifestPayload: { path: string; manifest: ChunkManifest } | null = null
  let pendingSecureInvite = ''
  let p2pStatusTimer: ReturnType<typeof setInterval> | null = null

  // ---- Init ----
  let initialized = false

  function init() {
    if (initialized) return
    initialized = true

    wsClient.on('open', () => {
      wsConnected.value = true
      connectionError.value = null
    })
    wsClient.on('close', () => {
      wsConnected.value = false
      if (!suppressCloseHandling && view.value === 'room') {
        resetRoomState('连接已断开，已退出房间', false)
      }
    })
    wsClient.on('error', () => {
      if (isConnecting.value) {
        connectionError.value = '连接失败，请检查地址或口令'
        isConnecting.value = false
      }
    })
    wsClient.on('message', handleWSMessage)
    EventsOn('chunking:progress', (payload: ChunkingProgress) => {
      localFileProgress.value = payload
      localFilePreparing.value = payload.stage !== 'complete' && payload.stage !== 'error'
    })

    webrtcManager.on('ice', ({ targetId, candidate }: { targetId: string; candidate: RTCIceCandidateInit }) => {
      wsClient.send({ type: 'webrtc_ice', target: targetId, candidate })
    })
    webrtcManager.on('data', ({ targetId, msg }: { targetId: string; msg: WSMessage }) => {
      handleWebRTCData(targetId, msg)
    })
    webrtcManager.on('open', ({ targetId }: { targetId: string }) => {
      p2pChunkManager.addPeer(targetId)
      syncWebRTCStatus()
    })
    webrtcManager.on('close', ({ targetId }: { targetId: string }) => {
      p2pChunkManager.removePeer(targetId)
      syncWebRTCStatus()
    })
    webrtcManager.on('state', () => syncWebRTCStatus())
    webrtcManager.on('route', (info) => {
      console.info('[room] webrtc route update', info)
    })
    webrtcManager.on('chunk', (data: { peerId: string; chunkIndex: number; data: Uint8Array }) => {
      p2pChunkManager.receiveChunkFromPeer(data.chunkIndex, data.data)
    })
  }

  function syncWebRTCStatus() {
    webrtcConnected.value = webrtcManager.connectedPeerIds.length > 0
  }

  function setP2PDownloadStatus(memberID: string, status: P2PDownloadStatus) {
    if (memberID) p2pDownloadStatuses.set(memberID, { ...status })
  }

  function getP2PDownloadStatus(memberID: string): P2PDownloadStatus | undefined {
    return p2pDownloadStatuses.get(memberID)
  }

  function publishLocalP2PStatus(override?: P2PDownloadStatus) {
    if (!userId.value) return
    const status = override || p2pChunkManager.getDownloadStatus()
    setP2PDownloadStatus(userId.value, status)
    wsClient.send({ type: 'p2p_download_status', p2pStatus: status })
  }

  function startP2PStatusReporter() {
    stopP2PStatusReporter(false)
    publishLocalP2PStatus()
    p2pStatusTimer = setInterval(() => publishLocalP2PStatus(), 750)
  }

  function stopP2PStatusReporter(publishFinal: boolean) {
    if (p2pStatusTimer) {
      clearInterval(p2pStatusTimer)
      p2pStatusTimer = null
    }
    if (publishFinal) publishLocalP2PStatus()
  }

  function triggerDanmaku(text: string, isSelf: boolean) {
    danmakuTrigger.value = { id: ++danmakuSeq, text, color: isSelf ? '#aaaaaa' : '#ffffff' }
  }

  function setPlayerFullscreen(fullscreen: boolean) {
    playerFullscreen.value = fullscreen
  }

  function setPreferredShareIp(ip: string) {
    preferredShareIp.value = ip
  }

  function passcodePriority(item: RoomPasscode) {
    if (item.isIPv6Public && item.isIPv6Temporary) return 0
    if (item.isIPv6Public) return 1
    if (!item.isIPv6Public && !item.isIPv6ULA) return 2
    if (item.isIPv6ULA) return 3
    return 4
  }

  function sortPasscodes(items: RoomPasscode[]) {
    return [...items].sort((a, b) => {
      const priorityDiff = passcodePriority(a) - passcodePriority(b)
      if (priorityDiff !== 0) return priorityDiff
      return a.ip.localeCompare(b.ip)
    })
  }

  function pickDefaultSharePasscode(items: RoomPasscode[]) {
    return items.find(item => item.isIPv6Public && item.isIPv6Temporary)
      || items.find(item => item.isIPv6Public)
      || items.find(item => !item.isIPv6Public && !item.isIPv6ULA)
      || items[0]
  }

  function resolveShareIp() {
    if (preferredShareIp.value) return preferredShareIp.value
    if (passcodes.value.length > 0) return passcodes.value[0].ip
    if (localIPv6s.value.length > 0) return localIPv6s.value[0]
    if (localIPs.value.length > 0) return localIPs.value[0]
    return hostIp.value === 'localhost' ? 'localhost' : hostIp.value
  }

  function isBuiltinMediaPath(value: string) {
    return value.startsWith('/video')
  }

  function canonicalizeMediaRef(value: string) {
    const trimmed = value.trim()
    if (!trimmed) return ''
    if (isBuiltinMediaPath(trimmed)) return trimmed

    try {
      const parsed = new URL(trimmed)
      if (isBuiltinMediaPath(parsed.pathname)) {
        return `${parsed.pathname}${parsed.search}`
      }
    } catch {
      // Keep non-URL values unchanged.
    }

    return trimmed
  }

  function resolveMediaRef(value: string, preferredHost?: string) {
    const canonical = canonicalizeMediaRef(value)
    if (!canonical) return ''
    if (!isBuiltinMediaPath(canonical)) return canonical

    const port = serverPort.value || 55511
    const host = preferredHost || hostIp.value || 'localhost'
    return appendMediaAccessToken(buildHttpURL(host, port, canonical))
  }

  function appendMediaAccessToken(value: string) {
    const token = secureInvite.value?.code
    if (!token || !value.startsWith('http')) return value
    const url = new URL(value)
    url.searchParams.set('access_token', token)
    return url.toString()
  }

  async function ensureLocalServerStarted() {
    if (localServerPort.value) return localServerPort.value
    const wails = window.go.main.App
    const port = await wails.StartServer()
    localServerPort.value = port
    return port
  }

  function setPlaybackOverride(url: string, sourceType: VideoSourceType) {
    videoState.value.localBlobUrl = url
    videoState.value.localBlobSourceType = sourceType
  }

  function clearPlaybackOverride() {
    videoState.value.localBlobUrl = undefined
    videoState.value.localBlobSourceType = undefined
  }

  function resetMagnetState() {
    magnetPreparing.value = false
    magnetStatusText.value = ''
  }

  function formatErrorMessage(err: any, fallback = '未知错误') {
    if (!err) return fallback
    if (typeof err === 'string') return err
    if (err.message) return String(err.message)
    try {
      const json = JSON.stringify(err)
      if (json && json !== '{}') return json
    } catch {
      // fall through
    }
    return String(err) || fallback
  }

  function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB']
    let next = value
    let idx = 0
    while (next >= 1024 && idx < units.length - 1) {
      next /= 1024
      idx++
    }
    return `${next.toFixed(idx === 0 ? 0 : 1)} ${units[idx]}`
  }

  function formatMagnetStatus(payload: MagnetStreamStatus) {
    const base = payload.statusText || '正在获取磁力元数据'
    const stats = payload.stream?.peerStats
    if (!stats) return base
    return `${base} · peer ${stats.activePeers}/${stats.totalPeers} · 连接 ${stats.peerConns} · 已读 ${formatBytes(stats.bytesReadUsefulData)}`
  }

  function shouldPreferP2PPlayback() {
    return !isHost.value &&
      videoState.value.sourceType === 'hls' &&
      isBuiltinMediaPath(videoState.value.source) &&
      !!videoState.value.chunkManifest
  }

  async function startMagnetPlayback(magnetURI: string) {
    const trimmed = magnetURI.trim()
    if (!trimmed) return

    const sessionSeq = ++magnetSessionSeq
    clearPlaybackOverride()
    magnetPreparing.value = true
    magnetStatusText.value = '正在启动磁力下载器'
    connectionError.value = null

    try {
      const streamPath = await window.go.main.App.ServeMagnetVideo(trimmed)
      const playbackUrl =
        streamPath.startsWith('http://') || streamPath.startsWith('https://')
          ? streamPath
          : buildHttpURL('localhost', localServerPort.value || serverPort.value || 55511, streamPath)
      const statusUrl = new URL(playbackUrl)
      statusUrl.pathname = '/status'

      const deadline = Date.now() + 150000
      while (sessionSeq === magnetSessionSeq) {
        let payload: MagnetStreamStatus | null = null
        try {
          const response = await fetch(statusUrl.toString(), { cache: 'no-store' })
          if (response.ok) {
            payload = await response.json() as MagnetStreamStatus
          }
        } catch {
          payload = null
        }

        if (sessionSeq !== magnetSessionSeq) return

        if (payload) {
          if (payload.ready) {
            setPlaybackOverride(playbackUrl, 'url')
            magnetPreparing.value = false
            magnetStatusText.value = payload.stream?.fileName
              ? `已就绪: ${payload.stream.fileName}`
              : '磁力视频已就绪'
            return
          }

          magnetStatusText.value = formatMagnetStatus(payload)
          if (payload.error) {
            throw new Error(payload.error)
          }
        }

        if (Date.now() >= deadline) {
          throw new Error('等待磁力元数据超时')
        }

        await new Promise((resolve) => setTimeout(resolve, 1200))
      }
    } catch (err: any) {
      clearPlaybackOverride()
      magnetPreparing.value = false
      connectionError.value = '磁力视频启动失败: ' + formatErrorMessage(err)
    }
  }

  function resetChunkPlaybackState() {
    stopP2PStatusReporter(false)
    p2pChunkManager.destroy()
    clearPlaybackOverride()
    resetMagnetState()
    videoState.value.chunkManifest = undefined
    p2pDownloadStatuses.clear()
  }

  function uniqueHostCandidates(candidates: string[]) {
    return [...new Set(candidates.filter(Boolean))]
  }

  function getShareCandidateIPs() {
    return uniqueHostCandidates([
      ...ipv6Addresses.value.map((info) => info.address),
      ...localIPs.value,
    ])
  }

  function refreshHostCandidates(candidates: string[], preferredHost?: string) {
    const merged = uniqueHostCandidates([
      preferredHost || '',
      ...candidates,
      hostIp.value,
    ])
    if (merged.length === 0) return

    hostCandidates.value = merged
    if (serverPort.value) {
      p2pChunkManager.setHostCandidates(merged, serverPort.value, preferredHost || hostIp.value)
    }
  }

  async function probeAndSelectHost(reason: string) {
    if (!serverPort.value || hostCandidates.value.length === 0 || isHost.value) return

    const generation = ++probeGeneration
    const candidates = [...hostCandidates.value]
    const currentHost = hostIp.value
    const results = await probeHosts(candidates, serverPort.value)
    if (generation !== probeGeneration) return

    console.info('[room] host probe results', { reason, results })

    const preferred = pickPreferredHost(results, currentHost)
    refreshHostCandidates(candidates, preferred?.host || currentHost)
    if (!preferred) return

    if (preferred.host !== hostIp.value) {
      console.info('[room] switched preferred host route', {
        reason,
        from: hostIp.value,
        to: preferred.host,
        family: preferred.family,
        latencyMs: preferred.latencyMs,
      })
      hostIp.value = preferred.host
    }
    p2pChunkManager.setPreferredHost(preferred.host, preferred.latencyMs)
  }

  function broadcastHostNetworkInfo() {
    if (!isHost.value || !wsConnected.value) return
    const candidates = hostCandidates.value.length > 0 ? hostCandidates.value : getShareCandidateIPs()
    if (candidates.length === 0) return

    wsClient.send({
      type: 'host_network_info',
      hostCandidates: candidates,
      preferredHostIp: resolveShareIp(),
    })
  }

  function applyVideoSource(source: string, sourceType: VideoSourceType, chunkManifest?: string) {
    resetChunkPlaybackState()
    videoState.value.source = canonicalizeMediaRef(source)
    videoState.value.sourceType = sourceType
    videoState.value.chunkManifest = chunkManifest ? canonicalizeMediaRef(chunkManifest) : undefined
  }

  // ---- WS Message Handler ----
  function handleWSMessage(msg: WSMessage) {
    console.debug('[room] handleWSMessage', msg)
    switch (msg.type) {
      case 'room_created': handleRoomCreated(msg); break
      case 'room_joined': handleRoomJoined(msg); break
      case 'room_error':
        connectionError.value = msg.message || '未知错误'
        isConnecting.value = false
        break
      case 'room_closed':
        resetRoomState(msg.message || '房间已关闭', false)
        break
      case 'user_joined': handleUserJoined(msg); break
      case 'user_left': handleUserLeft(msg); break
      case 'host_changed': handleHostChanged(msg); break
      case 'host_network_info': handleHostNetworkInfo(msg); break
      case 'p2p_manifest': handleP2PManifest(msg); break
      case 'chat': handleIncomingChat(msg); break
      case 'video_source': handleVideoSourceMsg(msg); break
      case 'video_play':
        videoState.value.playing = msg.playing ?? false
        videoState.value.currentTime = msg.currentTime ?? 0
        break
      case 'video_seek':
        videoState.value.currentTime = msg.currentTime ?? 0
        break
      case 'video_speed':
        videoState.value.speed = msg.speed ?? 1
        break
      case 'webrtc_offer': handleWebRTCOffer(msg); break
      case 'webrtc_answer':
        webrtcManager.handleAnswer(msg.from!, { type: 'answer' as RTCSdpType, sdp: msg.sdp! })
        break
      case 'webrtc_ice':
        if (msg.candidate) webrtcManager.handleICE(msg.from!, msg.candidate)
        break
      case 'p2p_chunk_offer':
        handleP2POffer(msg)
        break
      case 'p2p_chunk_request':
        handleP2PRequest(msg)
        break
      case 'p2p_download_status':
        if (msg.userId && msg.p2pStatus) setP2PDownloadStatus(msg.userId, msg.p2pStatus)
        break
      // p2p_chunk_data is now delivered via WebRTC DataChannel binary (handled via 'chunk' event)
    }
  }

  function handleRoomCreated(msg: WSMessage) {
    roomId.value = pendingSecureInvite || msg.roomId!
    userId.value = msg.userId!
    hostId.value = msg.userId!
    isHost.value = true
    wasHost = true
    hostIp.value = 'localhost'
    hostCandidates.value = ['localhost']
    users.value = [{
      id: msg.userId!,
      name: settings.username,
      isHost: true,
    }]
    p2pChunkManager.setLocalPeerID(msg.userId!)
    isConnecting.value = false
    view.value = 'room'
    pendingSecureInvite = ''
    void generatePasscodes().then(() => {
      refreshHostCandidates(getShareCandidateIPs(), resolveShareIp())
      broadcastHostNetworkInfo()
      startLANBroadcast()
    })
  }

  function handleRoomJoined(msg: WSMessage) {
    roomId.value = msg.roomId!
    userId.value = msg.userId!
    hostId.value = msg.hostId!
    isHost.value = false
    wasHost = false
    isConnecting.value = false
    users.value = msg.users || []
    p2pChunkManager.setLocalPeerID(msg.userId!)
    videoState.value = {
      source: canonicalizeMediaRef(msg.source || ''),
      sourceType: (msg.sourceType as VideoSourceType) || 'url',
      chunkManifest: msg.chunkManifest ? canonicalizeMediaRef(msg.chunkManifest) : undefined,
      playing: msg.playing ?? false,
      currentTime: msg.currentTime ?? 0,
      speed: msg.speed ?? 1,
    }
    lastAppliedSource = canonicalizeMediaRef(msg.source || '')
    view.value = 'room'
    refreshHostCandidates(hostCandidates.value.length > 0 ? hostCandidates.value : [hostIp.value], hostIp.value)
    if (videoState.value.sourceType === 'magnet' && videoState.value.source) {
      void startMagnetPlayback(videoState.value.source)
      return
    }
    if (msg.chunkManifest && !isHost.value) {
      startP2PDownload(msg.chunkManifest)
    }
  }

  function handleUserJoined(msg: WSMessage) {
    if (msg.userId === userId.value) return
    users.value.push({
      id: msg.userId!,
      name: msg.username!,
      isHost: msg.userId === hostId.value,
    })
    p2pDownloadStatuses.set(msg.userId!, {
      state: 'idle', progress: 0, bytesPerSecond: 0, bufferedSeconds: 0, downloaded: 0, total: 0,
    })
    // All existing members initiate WebRTC with the newcomer (not just host),
    // forming a full-mesh topology. The newcomer only responds to offers.
    initiateWebRTC(msg.userId!)
    if (isHost.value) {
      broadcastHostNetworkInfo()
      rebroadcastP2PManifest()
    }
  }

  function handleUserLeft(msg: WSMessage) {
    users.value = users.value.filter(u => u.id !== msg.userId)
    p2pDownloadStatuses.delete(msg.userId!)
    webrtcManager.closePeer(msg.userId!)
  }

  function handleHostChanged(msg: WSMessage) {
    hostId.value = msg.hostId!
    users.value = msg.users || []
    isHost.value = msg.hostId === userId.value
    if (isHost.value) wasHost = true
  }

  function handleHostNetworkInfo(msg: WSMessage) {
    if (isHost.value) return
    const candidates = msg.hostCandidates || []
    if (candidates.length === 0) return

    refreshHostCandidates(candidates, msg.preferredHostIp || hostIp.value)
    void probeAndSelectHost('host_network_info')
  }

  function handleIncomingChat(msg: WSMessage) {
    if (msg.userId === userId.value) return
    chatStore.addMessage({
      userId: msg.userId!,
      username: msg.username!,
      text: msg.text!,
      timestamp: msg.timestamp!,
      isSelf: false,
    })
    triggerDanmaku(msg.text!, false)
  }

  function handleVideoSourceMsg(msg: WSMessage) {
    const canonicalSource = canonicalizeMediaRef(msg.source || '')
    const canonicalManifest = msg.chunkManifest ? canonicalizeMediaRef(msg.chunkManifest) : undefined

    if (canonicalSource === lastAppliedSource && canonicalManifest === videoState.value.chunkManifest && webrtcConnected.value) return
    const samePlaybackSource =
      canonicalSource === lastAppliedSource &&
      ((msg.sourceType as VideoSourceType) || 'url') === videoState.value.sourceType

    if (samePlaybackSource) {
      videoState.value.chunkManifest = canonicalManifest
      if (((msg.sourceType as VideoSourceType) || 'url') === 'magnet' && canonicalSource && !videoState.value.localBlobUrl) {
        void startMagnetPlayback(canonicalSource)
      }
      if (msg.chunkManifest && !isHost.value) {
        startP2PDownload(msg.chunkManifest)
      }
      return
    }

    lastAppliedSource = canonicalSource
    applyVideoSource(canonicalSource, (msg.sourceType as VideoSourceType) || 'url', canonicalManifest)

    if (((msg.sourceType as VideoSourceType) || 'url') === 'magnet' && canonicalSource) {
      void startMagnetPlayback(canonicalSource)
      return
    }

    if (msg.chunkManifest && !isHost.value) {
      startP2PDownload(msg.chunkManifest)
    }
  }

  async function startP2PDownload(manifestRef: string) {
    try {
      const resolvedManifestUrl = resolveMediaRef(manifestRef)
      const url = new URL(resolvedManifestUrl)
      const hostPort = parseInt(url.port) || serverPort.value || 55511

      p2pChunkManager.destroy()
      if (!isHost.value && videoState.value.localBlobSourceType === 'url') {
        clearPlaybackOverride()
      }
      p2pChunkManager.setHost(url.hostname, hostPort)
      p2pChunkManager.setMediaAccessToken(secureInvite.value?.code || '')
      if (hostCandidates.value.length > 0) {
        refreshHostCandidates(hostCandidates.value, hostIp.value || url.hostname)
      } else {
        refreshHostCandidates([url.hostname], url.hostname)
      }
      try {
        await p2pChunkManager.fetchManifest()
      } catch (err) {
        console.warn('[p2p] manifest HTTP fetch failed, waiting for signaling manifest', err)
        await p2pChunkManager.waitForManifest()
      }

      // Add already-connected peers
      for (const peerId of webrtcManager.connectedPeerIds) {
        p2pChunkManager.addPeer(peerId)
      }

      startP2PStatusReporter()

      const blobUrl = await p2pChunkManager.downloadAll(
        () => {
          // After each chunk completes, broadcast what we have
          // regardless of the source. Otherwise chunks acquired from another
          // peer cannot fan out until the member has completely downloaded.
          const owned = p2pChunkManager.getOwnedChunks()
          if (owned.length % 5 === 0 || owned.length <= 10) {
            broadcastChunkOffer(owned)
          }
        },
        (streamUrl) => {
          // For local-file sharing on members, promote the first available P2P
          // stream to the primary playback path while keeping HLS as fallback.
          if (shouldPreferP2PPlayback()) {
            setPlaybackOverride(streamUrl, 'url')
          } else if (!videoState.value.source && !videoState.value.localBlobUrl) {
            setPlaybackOverride(streamUrl, 'url')
          }
        }
      )
      // Final broadcast of all owned chunks
      broadcastChunkOffer(p2pChunkManager.getOwnedChunks())
      // Blob mode: only use the assembled URL if no direct playback URL is active.
      if (!videoState.value.source && !videoState.value.localBlobUrl) {
        setPlaybackOverride(blobUrl, 'url')
      }
      stopP2PStatusReporter(true)
    } catch (err: any) {
      stopP2PStatusReporter(true)
      if (!isHost.value && videoState.value.localBlobSourceType === 'url') {
        clearPlaybackOverride()
      }
      connectionError.value = 'P2P 下载失败: ' + (err?.message || '未知错误')
    }
  }

  function broadcastChunkOffer(chunks: number[]) {
    const msg: WSMessage = { type: 'p2p_chunk_offer', chunks }
    webrtcManager.broadcast(JSON.stringify(msg))
    // Also via WebSocket for peers not yet connected via WebRTC
    wsClient.send(msg)
  }

  function broadcastP2PManifest(manifestPath: string, manifest: ChunkManifest) {
    lastChunkManifestPayload = { path: manifestPath, manifest }
    const msg: WSMessage = {
      type: 'p2p_manifest',
      chunkManifest: canonicalizeMediaRef(manifestPath),
      chunkManifestData: JSON.stringify(manifest),
    }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
    publishLocalP2PStatus({
      state: 'host', progress: 100, bytesPerSecond: 0,
      bufferedSeconds: manifest.totalDuration, downloaded: manifest.totalChunks, total: manifest.totalChunks,
    })
  }

  function rebroadcastP2PManifest() {
    if (!lastChunkManifestPayload) return
    broadcastP2PManifest(lastChunkManifestPayload.path, lastChunkManifestPayload.manifest)
  }

  // ---- P2P Chunk Handlers (via WebSocket — relayed by server) ----
  function handleP2POffer(msg: WSMessage) {
    if (msg.userId && msg.chunks) {
      p2pChunkManager.addPeer(msg.userId)
      p2pChunkManager.updatePeerChunks(msg.userId, msg.chunks)
    }
  }

  function handleP2PRequest(msg: WSMessage) {
    // Fallback path: chunk request routed via WebSocket when DataChannel unavailable
    if (!msg.needChunks || !msg.userId) return
    if (isHost.value) {
      void sendHostChunksToPeer(msg.userId, msg.needChunks)
      return
    }
    p2pChunkManager.handleChunkRequest(msg.userId, msg.needChunks)
  }

  async function sendHostChunksToPeer(peerId: string, chunkIndices: number[]) {
    const port = localServerPort.value || serverPort.value || 55511
    for (const idx of chunkIndices) {
      try {
        const response = await fetch(buildHttpURL('localhost', port, `/video/chunk/${idx}`), {
          cache: 'no-store',
          headers: secureInvite.value?.code ? { 'X-WT-Access-Token': secureInvite.value.code } : undefined,
        })
        if (!response.ok) continue
        await webrtcManager.sendChunk(peerId, idx, new Uint8Array(await response.arrayBuffer()))
      } catch (err) {
        console.warn('[p2p] host chunk send failed', { peerId, idx, err })
      }
    }
  }

  function handleP2PManifest(msg: WSMessage) {
    if (!msg.chunkManifestData) return
    try {
      const manifest = JSON.parse(msg.chunkManifestData) as ChunkManifest
      p2pChunkManager.setManifest(manifest)
    } catch (err) {
      console.warn('[p2p] invalid signaling manifest', err)
    }
  }

  // ---- WebRTC ----
  async function initiateWebRTC(targetId: string) {
    try {
      const offer = await webrtcManager.createOffer(targetId)
      wsClient.send({ type: 'webrtc_offer', target: targetId, sdp: offer.sdp })
    } catch (err) {
      console.error('WebRTC offer failed:', err)
    }
  }

  async function handleWebRTCOffer(msg: WSMessage) {
    try {
      const answer = await webrtcManager.handleOffer(msg.from!, {
        type: 'offer' as RTCSdpType,
        sdp: msg.sdp!,
      })
      wsClient.send({ type: 'webrtc_answer', target: msg.from!, sdp: answer.sdp })
    } catch (err) {
      console.error('WebRTC answer failed:', err)
    }
  }

  function handleWebRTCData(_fromId: string, msg: WSMessage) {
    switch (msg.type) {
      case 'video_source':
        handleVideoSourceMsg(msg)
        break
      case 'video_play':
        videoState.value.playing = msg.playing ?? false
        videoState.value.currentTime = msg.currentTime ?? 0
        break
      case 'video_seek':
        videoState.value.currentTime = msg.currentTime ?? 0
        break
      case 'video_speed':
        videoState.value.speed = msg.speed ?? 1
        break
      case 'p2p_manifest':
        handleP2PManifest(msg)
        break
      case 'p2p_chunk_offer':
        if (msg.chunks) {
          p2pChunkManager.addPeer(_fromId)
          p2pChunkManager.updatePeerChunks(_fromId, msg.chunks)
        }
        break
      case 'p2p_chunk_request':
        if (msg.needChunks) {
          if (isHost.value) {
            void sendHostChunksToPeer(_fromId, msg.needChunks)
          } else {
            p2pChunkManager.handleChunkRequest(_fromId, msg.needChunks)
          }
        }
        break
      // p2p_chunk_data is now handled via webrtcManager 'chunk' event (binary)
    }
  }

  // ---- Host: Create Room ----
  async function createRoom(username: string) {
    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = ''
    wsClient.disconnect() // clean up any previous failed connection

    try {
      const wails = window.go.main.App
      const port = await ensureLocalServerStarted()
      serverPort.value = port
      isDefaultPort.value = await wails.IsDefaultPort()
      localIPs.value = await wails.GetLocalIPs()
      localIPv6s.value = await wails.GetLocalIPv6s()
      ipv6Addresses.value = await wails.GetIPv6Addresses()
      refreshHostCandidates(getShareCandidateIPs(), resolveShareIp())
      console.info('[room] createRoom start', {
        username,
        port,
        isDefaultPort: isDefaultPort.value,
        localIPs: localIPs.value,
        localIPv6s: localIPv6s.value,
        ipv6Addresses: ipv6Addresses.value,
      })

      await wsClient.connect(buildWsURL('localhost', port))
      wsClient.send({ type: 'create_room', username })
    } catch (err: any) {
      connectionError.value = err?.message || '创建房间失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  async function createRoomViaSignalingRelay(username: string, relayUrl: string) {
    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = relayUrl.trim()
    pendingSecureInvite = createSecureInvite()
    secureInvite.value = { code: pendingSecureInvite, relayUrl: signalingRelayUrl }
    wsClient.disconnect()

    try {
      const wails = window.go.main.App
      const port = await ensureLocalServerStarted()
      serverPort.value = port
      isDefaultPort.value = await wails.IsDefaultPort()
      localIPs.value = await wails.GetLocalIPs()
      localIPv6s.value = await wails.GetLocalIPv6s()
      ipv6Addresses.value = await wails.GetIPv6Addresses()
      refreshHostCandidates(getShareCandidateIPs(), resolveShareIp())

      await wsClient.connect(normalizeSignalingRelayURL(signalingRelayUrl))
      wsClient.send({
        type: 'create_room',
        roomId: pendingSecureInvite,
        accessToken: pendingSecureInvite,
        username,
      })
      await wails.SetRoomAccessToken(pendingSecureInvite)
    } catch (err: any) {
      signalingRelayUrl = ''
      pendingSecureInvite = ''
      secureInvite.value = null
      await window.go.main.App.ClearRoomAccessToken().catch(() => {})
      connectionError.value = err?.message || '通过信令中继创建房间失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  async function generatePasscodes() {
    if (!roomId.value || !serverPort.value) return
    const wails = window.go.main.App
    const generated: RoomPasscode[] = []

    for (const info of ipv6Addresses.value) {
      const code = await wails.EncodePasscode(info.address, serverPort.value, roomId.value)
      generated.push({
        ip: info.address,
        passcode: code,
        isIPv6Public: info.isPublic,
        isIPv6ULA: info.isUla,
        isIPv6Temporary: info.isTemporary,
      })
    }
    for (const ip of localIPs.value) {
      const code = await wails.EncodePasscode(ip, serverPort.value, roomId.value)
      generated.push({ ip, passcode: code })
    }

    passcodes.value = sortPasscodes(generated)

    const defaultSharePasscode = pickDefaultSharePasscode(passcodes.value)
    if (defaultSharePasscode) {
      hostIp.value = defaultSharePasscode.ip
      preferredShareIp.value = defaultSharePasscode.ip
    }
    refreshHostCandidates(getShareCandidateIPs(), resolveShareIp())
  }

  // ---- Member: Join Room ----
  async function joinRoomByPasscode(code: string, username: string) {
    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = ''
    wsClient.disconnect() // clean up any previous failed connection

    try {
      const wails = window.go.main.App
      await ensureLocalServerStarted()
      const info = await wails.DecodePasscode(code.trim())
      hostIp.value = info.ip
      hostCandidates.value = [info.ip]
      serverPort.value = info.port
      isDefaultPort.value = info.port === 55511
      refreshHostCandidates([info.ip], info.ip)
      console.info('[room] joinRoomByPasscode decoded', {
        username,
        hostIp: hostIp.value,
        port: serverPort.value,
        roomId: info.roomId,
      })

      await wsClient.connect(buildWsURL(info.ip, info.port))
      wsClient.send({ type: 'join_room', roomId: info.roomId, username })
    } catch (err: any) {
      connectionError.value = err?.message || '口令无效或连接失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  async function joinRoomByIP(ipInput: string, username: string) {
    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = ''
    wsClient.disconnect() // clean up any previous failed connection

    const parsed = parseHostPortInput(ipInput, 55511)
    const ip = parsed.host
    const port = parsed.port

    hostIp.value = ip
    hostCandidates.value = [ip]
    serverPort.value = port
    isDefaultPort.value = port === 55511
    refreshHostCandidates([ip], ip)
    console.info('[room] joinRoomByIP parsed', {
      username,
      hostIp: hostIp.value,
      port: serverPort.value,
    })

    try {
      await ensureLocalServerStarted()
      await wsClient.connect(buildWsURL(ip, port))
      wsClient.send({ type: 'join_room', roomId: '', username })
    } catch (err: any) {
      connectionError.value = err?.message || '连接失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  async function joinRoomViaSignalingRelay(relayUrl: string, relayRoomId: string, username: string) {
    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = relayUrl.trim()
    const invite = relayRoomId.trim()
    if (!isSecureInvite(invite)) {
      connectionError.value = '邀请码格式无效'
      isConnecting.value = false
      suppressCloseHandling = false
      return
    }
    secureInvite.value = { code: invite, relayUrl: signalingRelayUrl }
    wsClient.disconnect()

    try {
      await ensureLocalServerStarted()
      hostIp.value = 'relay'
      hostCandidates.value = []
      serverPort.value = null
      isDefaultPort.value = false

      await wsClient.connect(normalizeSignalingRelayURL(signalingRelayUrl))
      wsClient.send({ type: 'join_room', roomId: invite, accessToken: invite, username })
    } catch (err: any) {
      signalingRelayUrl = ''
      secureInvite.value = null
      connectionError.value = err?.message || '信令中继连接失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  async function discoverLANRooms(timeoutMs = 1800) {
    init()
    lanDiscovering.value = true
    connectionError.value = null
    try {
      lanRooms.value = await window.go.main.App.DiscoverLANRooms(timeoutMs)
    } catch (err: any) {
      connectionError.value = err?.message || '局域网发现失败'
    } finally {
      lanDiscovering.value = false
    }
  }

  async function joinLANRoom(room: LANRoomInfo, username: string) {
    const host = room.ips[0] || room.from
    if (!host) {
      connectionError.value = '这个局域网房间没有可用地址'
      return
    }

    init()
    settings.setUsername(username)
    connectionError.value = null
    isConnecting.value = true
    suppressCloseHandling = true
    signalingRelayUrl = ''
    pendingSecureInvite = ''
    secureInvite.value = null
    wsClient.disconnect()

    try {
      await ensureLocalServerStarted()
      hostIp.value = host
      hostCandidates.value = room.ips.length > 0 ? room.ips : [host]
      serverPort.value = room.port
      isDefaultPort.value = room.port === 55511
      refreshHostCandidates(hostCandidates.value, host)

      await wsClient.connect(buildWsURL(host, room.port))
      wsClient.send({ type: 'join_room', roomId: room.roomId, username })
    } catch (err: any) {
      connectionError.value = err?.message || '局域网房间连接失败'
      isConnecting.value = false
      wsClient.disconnect()
    } finally {
      suppressCloseHandling = false
    }
  }

  function normalizeSignalingRelayURL(value: string) {
    const trimmed = value.trim()
    if (!trimmed) throw new Error('缺少信令中继地址')
    if (trimmed.startsWith('ws://') || trimmed.startsWith('wss://')) return trimmed
    if (trimmed.startsWith('http://')) return `ws://${trimmed.slice('http://'.length)}`
    if (trimmed.startsWith('https://')) return `wss://${trimmed.slice('https://'.length)}`
    return `wss://${trimmed}`
  }

  function startLANBroadcast() {
    if (!isHost.value || !roomId.value || !serverPort.value || signalingRelayUrl) return
    const username = settings.username || users.value.find((u) => u.id === userId.value)?.name || ''
    void window.go.main.App.StartLANRoomBroadcast(roomId.value, serverPort.value, username)
      .catch((err: any) => {
        console.warn('[room] LAN broadcast failed', err)
      })
  }

  // ---- Video Actions (Host only) ----
  function setVideoSource(source: string, sourceType: VideoSourceType, chunkManifest?: string) {
    if (!isHost.value) return
    const canonicalSource = canonicalizeMediaRef(source)
    const canonicalManifest = chunkManifest ? canonicalizeMediaRef(chunkManifest) : undefined

    lastAppliedSource = canonicalSource
    applyVideoSource(canonicalSource, sourceType, canonicalManifest)
    videoState.value.playing = false
    videoState.value.currentTime = 0

    const msg: WSMessage = { type: 'video_source', source: canonicalSource, sourceType, chunkManifest: canonicalManifest }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
    const syncMsg: WSMessage = { type: 'video_play', playing: false, currentTime: 0 }
    webrtcManager.broadcast(JSON.stringify(syncMsg))
    wsClient.send(syncMsg)
  }

  function attachChunkManifest(chunkManifest: string) {
    if (!isHost.value || !videoState.value.source) return
    const canonicalManifest = canonicalizeMediaRef(chunkManifest)
    videoState.value.chunkManifest = canonicalManifest

    const msg: WSMessage = {
      type: 'video_source',
      source: videoState.value.source,
      sourceType: videoState.value.sourceType,
      chunkManifest: canonicalManifest,
    }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
  }

  function sendVideoPlay(playing: boolean, currentTime: number) {
    if (!isHost.value) return
    videoState.value.playing = playing
    videoState.value.currentTime = currentTime

    const msg: WSMessage = { type: 'video_play', playing, currentTime }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
  }

  function sendVideoSeek(currentTime: number) {
    if (!isHost.value) return
    videoState.value.currentTime = currentTime

    const msg: WSMessage = { type: 'video_seek', currentTime }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
  }

  function sendVideoSpeed(speed: number) {
    if (!isHost.value) return
    videoState.value.speed = speed

    const msg: WSMessage = { type: 'video_speed', speed }
    webrtcManager.broadcast(JSON.stringify(msg))
    wsClient.send(msg)
  }

  async function selectLocalVideoFile() {
    if (!isHost.value) return
    try {
      await window.go.main.App.StopVideoServe().catch(() => {})
      resetChunkPlaybackState()
      lastChunkManifestPayload = null
      localFilePreparing.value = true
      localFileProgress.value = {
        stage: 'starting',
        current: 0,
        total: 0,
        percent: 0,
      }
      const wails = window.go.main.App
      const filePath = await wails.SelectVideoFile()
      if (!filePath) {
        localFilePreparing.value = false
        localFileProgress.value = null
        return
      }

      const port = localServerPort.value || serverPort.value || 55511
      const hlsPath = await wails.ServeVideoFileSegmented(filePath)
      const localPlaybackUrl = appendMediaAccessToken(buildHttpURL('localhost', port, hlsPath))

      setVideoSource(hlsPath, 'hls')
      setPlaybackOverride(localPlaybackUrl, 'hls')
      localFilePreparing.value = false

      void wails.ServeVideoFileChunked(filePath)
        .then((manifestPath) => {
          attachChunkManifest(manifestPath)
          return fetch(resolveMediaRef(manifestPath), { cache: 'no-store' })
            .then((response) => {
              if (!response.ok) throw new Error(`manifest ${response.status}`)
              return response.json() as Promise<ChunkManifest>
            })
            .then((manifest) => broadcastP2PManifest(manifestPath, manifest))
        })
        .catch((err: any) => {
          connectionError.value = 'P2P 分发预处理失败: ' + (err?.message || '未知错误')
        })
    } catch (err: any) {
      connectionError.value = err?.message || '无法加载视频文件'
      localFilePreparing.value = false
    }
  }

  async function setVideoURL(url: string) {
    if (!isHost.value || !url.trim()) return
    await window.go.main.App.StopVideoServe().catch(() => {})
    let sourceType: VideoSourceType = 'url'
    if (url.startsWith('magnet:?')) sourceType = 'magnet'
    else if (url.includes('.m3u8')) sourceType = 'hls'
    else if (url.includes('.flv')) sourceType = 'flv'
    else if (url.includes('.mpd')) sourceType = 'dash'
    const trimmed = url.trim()
    setVideoSource(trimmed, sourceType)
    if (sourceType === 'magnet') {
      await startMagnetPlayback(trimmed)
    }
  }

  // ---- Chat ----
  function sendChat(text: string) {
    const trimmed = text.trim()
    if (!trimmed) return

    const ts = Date.now()
    chatStore.addMessage({
      userId: userId.value!,
      username: settings.username,
      text: trimmed,
      timestamp: ts,
      isSelf: true,
    })
    triggerDanmaku(trimmed, true)

    const msg: WSMessage = { type: 'chat', text: trimmed, timestamp: ts }
    wsClient.send(msg)
  }

  // ---- Leave ----
  function resetRoomState(errorMessage: string | null = null, notifyServer = true) {
    const wasHostLocal = wasHost
    const hadLocalServer = !!localServerPort.value
    suppressCloseHandling = true
    magnetSessionSeq++

    if (notifyServer) {
      wsClient.send({ type: 'leave_room' })
    }
    wsClient.disconnect()
    webrtcManager.closeAll()
    stopP2PStatusReporter(false)
    p2pChunkManager.destroy()

    view.value = 'home'
    isHost.value = false
    wasHost = false
    playerFullscreen.value = false
    roomId.value = null
    userId.value = null
    hostId.value = null
    users.value = []
    p2pDownloadStatuses.clear()
    passcodes.value = []
    localIPv6s.value = []
    ipv6Addresses.value = []
    hostCandidates.value = []
    lanRooms.value = []
    lanDiscovering.value = false
    preferredShareIp.value = ''
    localServerPort.value = null
    videoState.value = {
      source: '', sourceType: 'url', playing: false, currentTime: 0, speed: 1,
    }
    localFilePreparing.value = false
    localFileProgress.value = null
    resetMagnetState()
    wsConnected.value = false
    webrtcConnected.value = false
    connectionError.value = errorMessage
    isConnecting.value = false
    signalingRelayUrl = ''
    lastChunkManifestPayload = null
    lastAppliedSource = ''
    chatStore.clear()

    try { window.go.main.App.StopVideoServe() } catch {}
    try { window.go.main.App.ClearRoomAccessToken() } catch {}
    try { window.go.main.App.StopLANRoomBroadcast() } catch {}
    if (hadLocalServer || wasHostLocal) {
      try { window.go.main.App.StopServer() } catch {}
    }

    suppressCloseHandling = false
  }

  function leaveRoom() {
    resetRoomState(null, true)
  }

  // ---- Computed ----
  const currentUser = computed(() =>
    users.value.find(u => u.id === userId.value)
  )

  const userCount = computed(() => users.value.length)

  return {
    view, activeTab,
    isHost, hostIp, serverPort, isDefaultPort,
    wsConnected, webrtcConnected, connectionError, isConnecting,
    roomId, userId, hostId, users, passcodes, localIPs, localIPv6s, ipv6Addresses, hostCandidates,
    lanRooms, lanDiscovering,
    preferredShareIp,
    localFilePreparing, localFileProgress, magnetPreparing, magnetStatusText, secureInvite, p2pDownloadStatuses,
    videoState, danmakuTrigger, playerFullscreen,
    currentUser, userCount,
    init, createRoom, createRoomViaSignalingRelay, joinRoomByPasscode, joinRoomByIP, joinRoomViaSignalingRelay,
    discoverLANRooms, joinLANRoom,
    leaveRoom, setVideoSource, setVideoURL, selectLocalVideoFile, getP2PDownloadStatus,
    sendVideoPlay, sendVideoSeek, sendVideoSpeed, sendChat,
    setPreferredShareIp, setPlayerFullscreen, resolveMediaRef, clearPlaybackOverride,
  }
})
