import type { WebRTCRouteInfo, WSMessage } from '@/types'
import { loadRelayConfig } from '@/utils/relayConfig'

type EventHandler = (data: any) => void

function buildIceConfig(): RTCConfiguration {
  const relay = loadRelayConfig()
  const iceServers: RTCIceServer[] = []

  if (relay.stunUrls.length > 0) {
    iceServers.push({ urls: relay.stunUrls })
  }

  if (relay.enabled && relay.turnUrls.length > 0) {
    iceServers.push({
      urls: relay.turnUrls,
      username: relay.username,
      credential: relay.credential,
    })
  }

  // ICE configuration hints for better IPv6 connectivity:
  // - iceTransportPolicy: 'all' allows host/srflx/relay candidates together.
  // - bundlePolicy: 'max-bundle' reduces the number of ICE candidates needed.
  // - rtcpMuxPolicy: 'require' is the modern default.
  return {
    iceServers,
    iceTransportPolicy: 'all',
    bundlePolicy: 'max-bundle',
    rtcpMuxPolicy: 'require',
  }
}

const CHUNK_FRAGMENT_SIZE = 64 * 1024 // 64KB per fragment
const HEADER_SIZE = 17 // 1 + 4 + 4 + 4 + 4 bytes
const MSG_TYPE_FRAGMENT = 0x01

// Per-fragment high-water mark before we wait for bufferedamountlow
const SEND_HIGH_WATER = 1 * 1024 * 1024 // 1MB

// Binary message format (little-endian):
// byte 0:    message type (0x01 = fragment)
// bytes 1-4: chunkIndex (uint32)
// bytes 5-8: fragmentIndex (uint32)
// bytes 9-12: totalFragments (uint32)
// bytes 13-16: dataLength in this fragment (uint32)
// bytes 17+: binary fragment data

interface IncomingChunkMeta {
  totalFragments: number
  chunkIndex: number
}

class WebRTCManager {
  private peers = new Map<string, RTCPeerConnection>()
  private channels = new Map<string, RTCDataChannel>()
  private handlers = new Map<string, Set<EventHandler>>()
  private statsPollers = new Map<string, ReturnType<typeof setInterval>>()
  private lastRouteSignature = new Map<string, string>()

  // ICE candidate queue: holds candidates that arrive before setRemoteDescription
  private pendingCandidates = new Map<string, RTCIceCandidateInit[]>()
  private remoteDescSet = new Set<string>()

  // Incoming fragment reassembly: peerId -> chunkIndex -> fragmentIndex -> buffer
  private incomingFragments = new Map<string, Map<number, Map<number, ArrayBuffer>>>()
  private incomingMeta = new Map<string, Map<number, IncomingChunkMeta>>()

  get connectedPeerIds(): string[] {
    const ids: string[] = []
    for (const [id, ch] of this.channels) {
      if (ch.readyState === 'open') ids.push(id)
    }
    return ids
  }

  hasPeer(id: string): boolean {
    return this.peers.has(id)
  }

  isChannelOpen(id: string): boolean {
    const ch = this.channels.get(id)
    return !!ch && ch.readyState === 'open'
  }

  getBufferAmount(id: string): number {
    return this.channels.get(id)?.bufferedAmount ?? 0
  }

  async createOffer(targetId: string): Promise<RTCSessionDescriptionInit> {
    const pc = this.createPeerConnection(targetId)
    const channel = pc.createDataChannel('data', {
      ordered: false,
      maxRetransmits: 0,
    })
    this.setupDataChannel(targetId, channel)

    const offer = await pc.createOffer()
    await pc.setLocalDescription(offer)
    return offer
  }

  async handleOffer(
    targetId: string,
    sdp: RTCSessionDescriptionInit
  ): Promise<RTCSessionDescriptionInit> {
    let pc = this.peers.get(targetId)
    if (!pc) {
      pc = this.createPeerConnection(targetId)
      pc.ondatachannel = (e) => {
        this.setupDataChannel(targetId, e.channel)
      }
    }

    await pc.setRemoteDescription(new RTCSessionDescription(sdp))
    this.remoteDescSet.add(targetId)
    await this.drainPendingCandidates(targetId, pc)

    const answer = await pc.createAnswer()
    await pc.setLocalDescription(answer)
    return answer
  }

  async handleAnswer(targetId: string, sdp: RTCSessionDescriptionInit) {
    const pc = this.peers.get(targetId)
    if (pc) {
      await pc.setRemoteDescription(new RTCSessionDescription(sdp))
      this.remoteDescSet.add(targetId)
      await this.drainPendingCandidates(targetId, pc)
    }
  }

  async handleICE(targetId: string, candidate: RTCIceCandidateInit) {
    if (!this.remoteDescSet.has(targetId)) {
      // Queue until setRemoteDescription completes
      const queue = this.pendingCandidates.get(targetId) ?? []
      queue.push(candidate)
      this.pendingCandidates.set(targetId, queue)
      return
    }
    const pc = this.peers.get(targetId)
    if (pc) {
      try {
        await pc.addIceCandidate(new RTCIceCandidate(candidate))
      } catch (err) {
        console.warn('Failed to add ICE candidate:', err)
      }
    }
  }

  private async drainPendingCandidates(targetId: string, pc: RTCPeerConnection) {
    const pending = this.pendingCandidates.get(targetId)
    if (!pending?.length) return
    this.pendingCandidates.delete(targetId)
    for (const candidate of pending) {
      try {
        await pc.addIceCandidate(new RTCIceCandidate(candidate))
      } catch (err) {
        console.warn('Failed to add queued ICE candidate:', err)
      }
    }
  }

  send(targetId: string, data: string): boolean {
    const ch = this.channels.get(targetId)
    if (ch?.readyState === 'open') {
      ch.send(data)
      return true
    }
    return false
  }

  broadcast(data: string) {
    let sent = false
    for (const [, ch] of this.channels) {
      if (ch.readyState === 'open') {
        ch.send(data)
        sent = true
      }
    }
    return sent
  }

  sendBinary(targetId: string, buffer: ArrayBuffer): boolean {
    const ch = this.channels.get(targetId)
    if (ch?.readyState === 'open') {
      try {
        ch.send(buffer)
        return true
      } catch (err) {
        console.warn('sendBinary failed:', err)
        return false
      }
    }
    return false
  }

  // Send a full chunk as fragmented binary messages with per-fragment flow control
  async sendChunk(targetId: string, chunkIndex: number, chunkData: Uint8Array): Promise<boolean> {
    const ch = this.channels.get(targetId)
    if (!ch || ch.readyState !== 'open') return false

    const totalFragments = Math.ceil(chunkData.byteLength / CHUNK_FRAGMENT_SIZE)

    for (let fragIdx = 0; fragIdx < totalFragments; fragIdx++) {
      // Wait if the send buffer is filling up
      if (ch.bufferedAmount > SEND_HIGH_WATER) {
        await new Promise<void>((resolve) => {
          const onLow = () => resolve()
          ch.addEventListener('bufferedamountlow', onLow, { once: true })
          setTimeout(resolve, 200) // fallback in case event misfires
        })
        if (ch.readyState !== 'open') return false
      }

      const fragStart = fragIdx * CHUNK_FRAGMENT_SIZE
      const fragEnd = Math.min(fragStart + CHUNK_FRAGMENT_SIZE, chunkData.byteLength)
      const fragData = chunkData.subarray(fragStart, fragEnd)

      const header = new ArrayBuffer(HEADER_SIZE)
      const view = new Uint8Array(header)
      const dv = new DataView(header)
      view[0] = MSG_TYPE_FRAGMENT
      dv.setUint32(1, chunkIndex, true)
      dv.setUint32(5, fragIdx, true)
      dv.setUint32(9, totalFragments, true)
      dv.setUint32(13, fragData.byteLength, true)

      const packet = new Uint8Array(HEADER_SIZE + fragData.byteLength)
      packet.set(view, 0)
      packet.set(fragData, HEADER_SIZE)

      if (!this.sendBinary(targetId, packet.buffer)) return false
    }
    return true
  }

  closePeer(targetId: string) {
    this.pendingCandidates.delete(targetId)
    this.remoteDescSet.delete(targetId)
    this.incomingFragments.delete(targetId)
    this.incomingMeta.delete(targetId)
    this.stopStatsPolling(targetId)
    this.lastRouteSignature.delete(targetId)

    const ch = this.channels.get(targetId)
    if (ch) {
      ch.close()
      this.channels.delete(targetId)
    }
    const pc = this.peers.get(targetId)
    if (pc) {
      pc.close()
      this.peers.delete(targetId)
    }
  }

  closeAll() {
    for (const id of [...this.peers.keys()]) {
      this.closePeer(id)
    }
  }

  private createPeerConnection(targetId: string): RTCPeerConnection {
    const iceConfig = buildIceConfig()
    console.info('[webrtc] creating peer connection', {
      targetId,
      relayEnabled: loadRelayConfig().enabled,
      iceServers: iceConfig.iceServers?.map((server) => server.urls),
    })
    const pc = new RTCPeerConnection(iceConfig)

    pc.onicecandidate = (e) => {
      if (e.candidate) {
        this.emit('ice', { targetId, candidate: e.candidate.toJSON() })
      }
    }

    pc.onconnectionstatechange = () => {
      const state = pc.connectionState
      this.emit('state', { targetId, state })
      if (state === 'connected') {
        this.startStatsPolling(targetId, pc)
      }
      if (state === 'failed' || state === 'closed') {
        this.closePeer(targetId)
      }
    }

    pc.oniceconnectionstatechange = () => {
      if (pc.iceConnectionState === 'connected' || pc.iceConnectionState === 'completed') {
        this.logSelectedRoute(targetId, pc)
      }
    }

    this.peers.set(targetId, pc)
    return pc
  }

  private setupDataChannel(targetId: string, channel: RTCDataChannel) {
    channel.binaryType = 'arraybuffer'
    channel.bufferedAmountLowThreshold = SEND_HIGH_WATER

    channel.onopen = () => this.emit('open', { targetId })
    channel.onclose = () => this.emit('close', { targetId })
    channel.onbufferedamountlow = () => this.emit('bufferedLow', { targetId })

    channel.onmessage = (e) => {
      if (e.data instanceof ArrayBuffer) {
        this.handleBinaryMessage(targetId, e.data)
      } else if (typeof e.data === 'string') {
        try {
          const msg: WSMessage = JSON.parse(e.data)
          this.emit('data', { targetId, msg })
        } catch (err) {
          console.error('Failed to parse data channel message:', err)
        }
      }
    }
    this.channels.set(targetId, channel)
  }

  private startStatsPolling(targetId: string, pc: RTCPeerConnection) {
    this.stopStatsPolling(targetId)
    this.logSelectedRoute(targetId, pc)
    const timer = setInterval(() => {
      this.logSelectedRoute(targetId, pc)
    }, 5000)
    this.statsPollers.set(targetId, timer)
  }

  private stopStatsPolling(targetId: string) {
    const timer = this.statsPollers.get(targetId)
    if (timer) {
      clearInterval(timer)
      this.statsPollers.delete(targetId)
    }
  }

  private async logSelectedRoute(targetId: string, pc: RTCPeerConnection) {
    if (pc.connectionState === 'closed') return

    try {
      const stats = await pc.getStats()
      const info = this.extractRouteInfo(targetId, pc, stats)
      if (!info) return

      const signature = JSON.stringify(info)
      if (this.lastRouteSignature.get(targetId) === signature) return
      this.lastRouteSignature.set(targetId, signature)

      console.info('[webrtc] selected route', info)
      this.emit('route', info)
    } catch (err) {
      console.warn('[webrtc] getStats failed', { targetId, err })
    }
  }

  private extractRouteInfo(
    targetId: string,
    pc: RTCPeerConnection,
    stats: RTCStatsReport,
  ): WebRTCRouteInfo | null {
    const transport = Array.from(stats.values()).find((report: any) =>
      report.type === 'transport' && report.selectedCandidatePairId
    ) as any

    let selectedPair: any = null
    if (transport?.selectedCandidatePairId) {
      selectedPair = stats.get(transport.selectedCandidatePairId as string) as any
    }

    if (!selectedPair) {
      selectedPair = Array.from(stats.values()).find((report: any) =>
        report.type === 'candidate-pair' &&
        (report.selected === true || (report.nominated === true && report.state === 'succeeded'))
      ) as any
    }

    if (!selectedPair) return null

    const localCandidate = stats.get(selectedPair.localCandidateId as string) as any
    const remoteCandidate = stats.get(selectedPair.remoteCandidateId as string) as any
    const localAddress = this.readCandidateAddress(localCandidate)
    const remoteAddress = this.readCandidateAddress(remoteCandidate)

    return {
      targetId,
      state: pc.connectionState,
      protocol: localCandidate?.protocol || remoteCandidate?.protocol,
      localCandidateType: localCandidate?.candidateType,
      remoteCandidateType: remoteCandidate?.candidateType,
      localAddress,
      remoteAddress,
      localFamily: this.detectAddressFamily(localAddress),
      remoteFamily: this.detectAddressFamily(remoteAddress),
      currentRoundTripTimeMs: selectedPair.currentRoundTripTime != null
        ? Math.round(selectedPair.currentRoundTripTime * 1000)
        : undefined,
      availableOutgoingBitrate: typeof selectedPair.availableOutgoingBitrate === 'number'
        ? Math.round(selectedPair.availableOutgoingBitrate)
        : undefined,
    }
  }

  private readCandidateAddress(candidate: any): string | undefined {
    if (!candidate) return undefined
    return candidate.address || candidate.ip || candidate.ipAddress || candidate.relatedAddress
  }

  private detectAddressFamily(address?: string): 'ipv4' | 'ipv6' | 'unknown' {
    if (!address) return 'unknown'
    if (address.includes(':')) return 'ipv6'
    if (address.includes('.')) return 'ipv4'
    return 'unknown'
  }

  private handleBinaryMessage(peerId: string, buffer: ArrayBuffer) {
    if (buffer.byteLength < HEADER_SIZE) return

    const dv = new DataView(buffer)
    if (dv.getUint8(0) !== MSG_TYPE_FRAGMENT) return

    const chunkIndex = dv.getUint32(1, true)
    const fragmentIndex = dv.getUint32(5, true)
    const totalFragments = dv.getUint32(9, true)
    const dataLength = dv.getUint32(13, true)

    if (!this.incomingFragments.has(peerId)) {
      this.incomingFragments.set(peerId, new Map())
    }
    const peerFragments = this.incomingFragments.get(peerId)!

    if (!peerFragments.has(chunkIndex)) {
      peerFragments.set(chunkIndex, new Map())
    }
    const chunkFragments = peerFragments.get(chunkIndex)!

    // Skip duplicate fragments
    if (chunkFragments.has(fragmentIndex)) return

    const fragData = new Uint8Array(buffer, HEADER_SIZE, dataLength)
    const copy = new Uint8Array(dataLength)
    copy.set(fragData)
    chunkFragments.set(fragmentIndex, copy.buffer)

    if (!this.incomingMeta.has(peerId)) {
      this.incomingMeta.set(peerId, new Map())
    }
    this.incomingMeta.get(peerId)!.set(chunkIndex, { totalFragments, chunkIndex })

    if (chunkFragments.size !== totalFragments) return

    // Reassemble
    let totalSize = 0
    for (let i = 0; i < totalFragments; i++) {
      totalSize += chunkFragments.get(i)!.byteLength
    }

    const assembled = new Uint8Array(totalSize)
    let offset = 0
    for (let i = 0; i < totalFragments; i++) {
      const frag = new Uint8Array(chunkFragments.get(i)!)
      assembled.set(frag, offset)
      offset += frag.byteLength
    }

    peerFragments.delete(chunkIndex)
    this.incomingMeta.get(peerId)?.delete(chunkIndex)

    this.emit('chunk', { peerId, chunkIndex, data: assembled })
  }

  on(event: string, handler: EventHandler) {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set())
    this.handlers.get(event)!.add(handler)
  }

  off(event: string, handler: EventHandler) {
    this.handlers.get(event)?.delete(handler)
  }

  private emit(event: string, data?: any) {
    this.handlers.get(event)?.forEach((h) => h(data))
  }
}

export const webrtcManager = new WebRTCManager()
export { CHUNK_FRAGMENT_SIZE }
