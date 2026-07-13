import { ref, reactive } from 'vue'
import type { ChunkManifest, P2PDownloadStatus } from '@/types'
import { webrtcManager } from '@/composables/useWebRTC'
import { buildHttpURL } from '@/utils/network'

const PEER_CHUNK_TIMEOUT = 8000
const HOST_REQUEST_TIMEOUT = 6000
const MANIFEST_REQUEST_TIMEOUT = 3000
const MANIFEST_SIGNAL_TIMEOUT = 12000
const MAX_BUFFER_PER_PEER = 4 * 1024 * 1024
const MAX_PENDING_PER_PEER = 4
const MAX_PENDING_HOST = 4
const MAX_DOWNLOAD_WORKERS = 16
const RACE_CHUNK_COUNT = 3
const INITIAL_BUFFER_SECONDS = 90
const SEEK_BUFFER_SECONDS = 45
const DISTRIBUTION_LANES = 8

const PEER_COLORS = [
  '#4ade80', '#60a5fa', '#f472b6', '#fbbf24', '#a78bfa',
  '#fb923c', '#2dd4bf', '#f87171', '#38bdf8', '#c084fc',
  '#a3e635', '#facc15',
]

class MSEStreamer {
  private ms: MediaSource
  private sb: SourceBuffer | null = null
  readonly url: string

  private nextChunkToAppend = 0
  private pendingChunks = new Map<number, Uint8Array>()
  private appendQueue: Uint8Array[] = []
  private finalizeCalled = false
  hasFailed = false

  constructor(private readonly mimeCodec: string) {
    this.ms = new MediaSource()
    this.url = URL.createObjectURL(this.ms)
  }

  open(): Promise<boolean> {
    return new Promise((resolve) => {
      this.ms.addEventListener('sourceopen', () => {
        try {
          this.sb = this.ms.addSourceBuffer(this.mimeCodec)
          this.sb.mode = 'segments'
          this.sb.addEventListener('updateend', () => this.onUpdateEnd())
          this.sb.addEventListener('error', () => { this.hasFailed = true })
          resolve(true)
        } catch (err) {
          console.error('[p2p] source buffer init failed', err)
          this.hasFailed = true
          resolve(false)
        }
      }, { once: true })
    })
  }

  onChunkReady(chunkIndex: number, data: Uint8Array) {
    if (this.hasFailed) return
    this.pendingChunks.set(chunkIndex, data)
    this.drainPending()
  }

  finalize() {
    this.finalizeCalled = true
    this.tryEndOfStream()
  }

  private drainPending() {
    while (this.pendingChunks.has(this.nextChunkToAppend)) {
      const data = this.pendingChunks.get(this.nextChunkToAppend)!
      this.pendingChunks.delete(this.nextChunkToAppend)
      this.appendQueue.push(data)
      this.nextChunkToAppend++
    }
    this.flush()
  }

  private flush() {
    if (!this.sb || this.sb.updating || this.appendQueue.length === 0 || this.hasFailed) return
    const data = this.appendQueue.shift()!
    try {
      this.sb.appendBuffer(data.slice().buffer as ArrayBuffer)
    } catch (err) {
      console.error('[p2p] append failed', err)
      this.hasFailed = true
    }
  }

  private onUpdateEnd() {
    this.flush()
    this.tryEndOfStream()
  }

  private tryEndOfStream() {
    if (
      this.finalizeCalled &&
      !this.hasFailed &&
      this.appendQueue.length === 0 &&
      !this.sb?.updating &&
      this.ms.readyState === 'open'
    ) {
      try { this.ms.endOfStream() } catch {}
    }
  }

  destroy() {
    if (this.ms.readyState === 'open') {
      try { this.ms.endOfStream() } catch {}
    }
    URL.revokeObjectURL(this.url)
  }
}

interface PeerInfo {
  id: string
  color: string
  ownedChunks: Set<number>
  lastSeen: number
  speedBytesPerMs: number
  pendingRequests: number
}

interface ChunkOwner {
  source: 'host' | 'peer' | 'self'
  peerId?: string
  peerColor?: string
}

interface HostEndpoint {
  host: string
  port: number
  latencyMs?: number
  failCount: number
}

interface SourceCandidate {
  kind: 'peer' | 'host'
  key: string
  peer?: PeerInfo
  sameGroupLoads: number
  pendingRequests: number
  speedBytesPerMs: number
  latencyMs?: number
}

class P2PChunkManager {
  private manifest: ChunkManifest | null = null
  private downloadedChunks = new Map<number, Uint8Array>()
  private chunkOwners = new Map<number, ChunkOwner>()
  private hostIp = ''
  private hostPort = 0
  private mediaAccessToken = ''
  private hostEndpoints: HostEndpoint[] = []
  private hostPendingRequests = 0
  private hostSpeedBytesPerMs = 0
  private colorIndex = 0
  private inFlightChunks = new Set<number>()
  private sourceGroupPending = new Map<string, Map<number, number>>()
  private mseStreamer: MSEStreamer | null = null
  private urgentQueue: number[] = []
  private seekChunk = 1
  private localPeerID = ''
  private peers = new Map<string, PeerInfo>()
  private speedWindow: Array<{ time: number; bytes: number }> = []

  public isDownloading = ref(false)
  public progress = ref(0)
  public status = ref<'idle' | 'downloading' | 'assembling' | 'ready' | 'error'>('idle')
  public errorMessage = ref('')
  public p2pStats = ref({ fromHost: 0, fromPeers: 0, uploadedToPeers: 0 })
  public downloadStats = ref({ bytesPerSecond: 0, fromHostBytes: 0, fromPeerBytes: 0 })
  public peerCount = ref(0)
  public p2pRatio = ref(0)
  public peerList = reactive<Array<{ id: string; color: string }>>([])
  public chunkColorMap = reactive<Map<number, string>>(new Map())

  private getPeerColor(peerId: string): string {
    const existing = this.peers.get(peerId)
    if (existing) return existing.color
    const color = PEER_COLORS[this.colorIndex % PEER_COLORS.length]
    this.colorIndex++
    return color
  }

  setHost(ip: string, port: number) {
    this.hostIp = ip
    this.hostPort = port
    this.setHostCandidates([ip], port, ip)
  }

  setMediaAccessToken(token: string) {
    this.mediaAccessToken = token
  }

  setLocalPeerID(peerID: string) {
    this.localPeerID = peerID
  }

  setHostCandidates(hosts: string[], port: number, preferredHost?: string) {
    const uniqueHosts = [...new Set(hosts.filter(Boolean))]
    if (uniqueHosts.length === 0) return

    const previous = new Map(this.hostEndpoints.map((endpoint) => [endpoint.host, endpoint]))
    this.hostEndpoints = uniqueHosts.map((host) => {
      const prev = previous.get(host)
      return {
        host,
        port,
        latencyMs: prev?.latencyMs,
        failCount: prev?.failCount ?? 0,
      }
    })

    this.hostPort = port
    this.hostIp =
      preferredHost && uniqueHosts.includes(preferredHost)
        ? preferredHost
        : this.hostIp && uniqueHosts.includes(this.hostIp)
          ? this.hostIp
          : uniqueHosts[0]
  }

  setPreferredHost(host: string, latencyMs?: number) {
    const endpoint = this.hostEndpoints.find((candidate) => candidate.host === host)
    if (!endpoint) return
    this.hostIp = host
    if (latencyMs != null) {
      endpoint.latencyMs = latencyMs
    }
    endpoint.failCount = 0
  }

  getHostCandidates(): string[] {
    return this.hostEndpoints.map((endpoint) => endpoint.host)
  }

  async fetchManifest(): Promise<ChunkManifest> {
    const { response } = await this.fetchFromHostPath('/video/manifest', MANIFEST_REQUEST_TIMEOUT)
    if (!response.ok) throw new Error(`manifest ${response.status}`)
    const nextManifest = await response.json() as ChunkManifest
    this.mergeManifest(nextManifest)
    return this.manifest!
  }

  setManifest(nextManifest: ChunkManifest) {
    this.mergeManifest(nextManifest)
  }

  getManifest(): ChunkManifest | null {
    return this.manifest
  }

  async waitForManifest(timeoutMs = MANIFEST_SIGNAL_TIMEOUT): Promise<ChunkManifest> {
    if (this.manifest) return this.manifest
    const deadline = Date.now() + timeoutMs
    while (!this.manifest && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 150))
    }
    if (!this.manifest) throw new Error('等待房主分段清单超时')
    return this.manifest
  }

  addPeer(peerId: string) {
    if (this.peers.has(peerId)) return
    const color = this.getPeerColor(peerId)
    this.peers.set(peerId, {
      id: peerId,
      color,
      ownedChunks: new Set(),
      lastSeen: Date.now(),
      speedBytesPerMs: 0,
      pendingRequests: 0,
    })
    this.peerCount.value = this.peers.size
    this.syncPeerList()
  }

  removePeer(peerId: string) {
    this.peers.delete(peerId)
    this.peerCount.value = this.peers.size
    this.syncPeerList()
  }

  private syncPeerList() {
    this.peerList.splice(0, this.peerList.length)
    for (const p of this.peers.values()) {
      this.peerList.push({ id: p.id, color: p.color })
    }
  }

  updatePeerChunks(peerId: string, chunks: number[]) {
    const peer = this.peers.get(peerId)
    if (!peer) return
    peer.ownedChunks = new Set(chunks)
    peer.lastSeen = Date.now()
  }

  setSeekGroup(chunkIndex: number) {
    if (!this.manifest) return
    this.seekChunk = Math.max(1, Math.min(chunkIndex, this.manifest.totalChunks - 1))
    let bufferedSeconds = 0
    for (let idx = this.seekChunk; idx < this.manifest.totalChunks && bufferedSeconds < SEEK_BUFFER_SECONDS; idx++) {
      if (!this.downloadedChunks.has(idx)) {
        this.urgentQueue.push(idx)
      }
      bufferedSeconds += this.manifest.chunks.find((chunk) => chunk.index === idx)?.duration ?? this.manifest.segmentTime
    }
  }

  private mergeManifest(nextManifest: ChunkManifest) {
    if (!this.manifest || nextManifest.totalChunks >= this.manifest.totalChunks) {
      this.manifest = nextManifest
    } else {
      this.manifest = {
        ...this.manifest,
        complete: this.manifest.complete || nextManifest.complete,
      }
    }
  }

  private getChunkStartTime(chunkIndex: number): number {
    return this.manifest?.chunks.find((chunk) => chunk.index === chunkIndex)?.startTime ?? chunkIndex
  }

  private getSourceGroupLoad(sourceKey: string, chunkIndex: number): number {
    return this.sourceGroupPending.get(sourceKey)?.get(chunkIndex) ?? 0
  }

  private trackSourceGroup(sourceKey: string, chunkIndex: number, delta: 1 | -1) {
    const byChunk = this.sourceGroupPending.get(sourceKey) ?? new Map<number, number>()
    const nextValue = (byChunk.get(chunkIndex) ?? 0) + delta
    if (nextValue <= 0) {
      byChunk.delete(chunkIndex)
    } else {
      byChunk.set(chunkIndex, nextValue)
    }
    if (byChunk.size === 0) {
      this.sourceGroupPending.delete(sourceKey)
    } else {
      this.sourceGroupPending.set(sourceKey, byChunk)
    }
  }

  private peersWithChunk(chunkIndex: number): PeerInfo[] {
    const result: PeerInfo[] = []
    const underLimit: PeerInfo[] = []
    for (const peer of this.peers.values()) {
      if (peer.ownedChunks.has(chunkIndex) && webrtcManager.isChannelOpen(peer.id)) {
        result.push(peer)
        if (peer.pendingRequests < MAX_PENDING_PER_PEER) {
          underLimit.push(peer)
        }
      }
    }
    const source = underLimit.length > 0 ? underLimit : result
    source.sort(
      (a, b) =>
        b.speedBytesPerMs / (b.pendingRequests + 1) -
        a.speedBytesPerMs / (a.pendingRequests + 1)
    )
    return source
  }

  private getHostCandidate(chunkIndex: number): SourceCandidate | null {
    if (!this.hostIp && this.hostEndpoints.length === 0) return null
    if (this.hostPendingRequests >= MAX_PENDING_HOST) return null
    const preferredEndpoint = this.getOrderedHostEndpoints()[0]
    return {
      kind: 'host',
      key: 'host',
      sameGroupLoads: this.getSourceGroupLoad('host', chunkIndex),
      pendingRequests: this.hostPendingRequests,
      speedBytesPerMs: this.hostSpeedBytesPerMs,
      latencyMs: preferredEndpoint?.latencyMs,
    }
  }

  private getSourceCandidates(chunkIndex: number): SourceCandidate[] {
    const candidates: SourceCandidate[] = this.peersWithChunk(chunkIndex).map((peer) => ({
      kind: 'peer',
      key: `peer:${peer.id}`,
      peer,
      sameGroupLoads: this.getSourceGroupLoad(`peer:${peer.id}`, chunkIndex),
      pendingRequests: peer.pendingRequests,
      speedBytesPerMs: peer.speedBytesPerMs,
    }))

    const hostCandidate = this.getHostCandidate(chunkIndex)
    if (hostCandidate) {
      candidates.push(hostCandidate)
    }

    candidates.sort((a, b) => {
      if (a.sameGroupLoads !== b.sameGroupLoads) return a.sameGroupLoads - b.sameGroupLoads
      if (a.pendingRequests !== b.pendingRequests) return a.pendingRequests - b.pendingRequests
      if (a.speedBytesPerMs !== b.speedBytesPerMs) return b.speedBytesPerMs - a.speedBytesPerMs
      return (a.latencyMs ?? Number.MAX_SAFE_INTEGER) - (b.latencyMs ?? Number.MAX_SAFE_INTEGER)
    })

    return candidates
  }

  async downloadAll(
    onChunkComplete?: (chunkIndex: number, fromPeer: boolean) => void,
    onStreamReady?: (url: string) => void,
  ): Promise<string> {
    if (!this.manifest) throw new Error('no manifest')
    this.isDownloading.value = true
    this.status.value = 'downloading'
    this.errorMessage.value = ''

    const mimeCodec = this.manifest.mimeCodec || 'video/mp4; codecs="avc1.4d401f,mp4a.40.2"'
    if (typeof MediaSource === 'undefined' || !MediaSource.isTypeSupported(mimeCodec)) {
      this.status.value = 'error'
      this.errorMessage.value = `MSE codec unsupported: ${mimeCodec}`
      this.isDownloading.value = false
      throw new Error(this.errorMessage.value)
    }

    this.mseStreamer = new MSEStreamer(mimeCodec)
    const opened = await this.mseStreamer.open()
    if (!opened) {
      this.status.value = 'error'
      this.errorMessage.value = 'MSE stream init failed'
      this.isDownloading.value = false
      throw new Error(this.errorMessage.value)
    }

    onStreamReady?.(this.mseStreamer.url)

    const processQueue = async () => {
      while (true) {
        const chunkIndex = await this.takeNextChunk()
        if (chunkIndex == null) break

        try {
          const fromPeer = await this.downloadOneChunk(chunkIndex)
          this.updateProgress()
          onChunkComplete?.(chunkIndex, fromPeer)
        } finally {
          this.inFlightChunks.delete(chunkIndex)
        }
      }
    }

    // Keep a broad playable window in flight. The host remains protected by
    // MAX_PENDING_HOST while peers can fan out the later striped chunks.
    const concurrency = Math.min(Math.max(8, 4 + this.peers.size * 2), MAX_DOWNLOAD_WORKERS)

    try {
      await Promise.all(
        Array.from({ length: Math.min(concurrency, Math.max(this.manifest.totalChunks, 1)) }, () => processQueue())
      )
    } catch (err) {
      this.status.value = 'error'
      this.errorMessage.value = err instanceof Error ? err.message : String(err)
      this.isDownloading.value = false
      throw err
    }

    const total = this.p2pStats.value.fromHost + this.p2pStats.value.fromPeers
    this.p2pRatio.value =
      total > 0 ? Math.round((this.p2pStats.value.fromPeers / total) * 100) : 0

    this.mseStreamer.finalize()
    this.status.value = 'ready'
    this.isDownloading.value = false
    this.progress.value = 100
    return this.mseStreamer.url
  }

  private async takeNextChunk(): Promise<number | null> {
    while (true) {
      while (this.urgentQueue.length > 0) {
        const chunkIndex = this.urgentQueue.shift()!
        if (this.downloadedChunks.has(chunkIndex) || this.inFlightChunks.has(chunkIndex)) continue
        this.inFlightChunks.add(chunkIndex)
        return chunkIndex
      }

      const chunkIndex = this.findNextPendingChunk()
      if (chunkIndex != null) {
        this.inFlightChunks.add(chunkIndex)
        return chunkIndex
      }

      if (!this.manifest?.complete) {
        await this.refreshManifestWithBackoff()
        continue
      }

      return null
    }
  }

  private findNextPendingChunk(): number | null {
    if (!this.manifest) return null

    // Every member first fills the same ~90 second playback safety window so
    // a slow connection does not immediately stall video playback.
    const initialEnd = this.initialBufferEnd()
    for (let i = 0; i < initialEnd; i++) {
      if (this.downloadedChunks.has(i) || this.inFlightChunks.has(i)) continue
      return i
    }

    // Once that buffer is safe, spread future chunks across deterministic
    // lanes. Members therefore fetch different later ranges from the host,
    // announce them immediately, and become useful upload sources for each
    // other instead of all duplicating the host's next request.
    const lane = this.distributionLane()
    for (let i = initialEnd + lane; i < this.manifest.totalChunks; i += DISTRIBUTION_LANES) {
      if (this.downloadedChunks.has(i) || this.inFlightChunks.has(i)) continue
      return i
    }

    // Finish any gaps left by peers that joined later or shared a lane.
    for (let i = initialEnd; i < this.manifest.totalChunks; i++) {
      if (this.downloadedChunks.has(i) || this.inFlightChunks.has(i)) continue
      return i
    }
    return null
  }

  private initialBufferEnd(): number {
    if (!this.manifest) return 0
    let seconds = 0
    for (let index = 0; index < this.manifest.chunks.length; index++) {
      seconds += this.manifest.chunks[index].duration
      if (seconds >= INITIAL_BUFFER_SECONDS) return index + 1
    }
    return this.manifest.totalChunks
  }

  private distributionLane(): number {
    let hash = 0
    for (const char of this.localPeerID) {
      hash = ((hash << 5) - hash + char.charCodeAt(0)) | 0
    }
    return Math.abs(hash) % DISTRIBUTION_LANES
  }

  private async refreshManifestWithBackoff() {
    await new Promise((resolve) => setTimeout(resolve, 350))
    await this.fetchManifest()
  }

  private async downloadOneChunk(chunkIndex: number): Promise<boolean> {
    const candidates = this.getSourceCandidates(chunkIndex)
    const canRace = chunkIndex > 0 && chunkIndex <= RACE_CHUNK_COUNT && candidates.length >= 2

    if (canRace) {
      try {
        const raced = await this.raceBestPaths(candidates.slice(0, Math.min(3, candidates.length)), chunkIndex)
        await this.verifyChunk(chunkIndex, raced.data)
        this.storeChunk(chunkIndex, raced.data, raced.winner)
        return raced.winner !== null
      } catch {
        // Fall back to normal iteration.
      }
    }

    for (const candidate of candidates) {
      try {
        const { data, fromPeer, peer } = await this.downloadFromSource(candidate, chunkIndex)
        await this.verifyChunk(chunkIndex, data)
        this.storeChunk(chunkIndex, data, peer)
        return fromPeer
      } catch {
        // Try the next path.
      }
    }

    const data = await this.downloadFromHost(chunkIndex)
    await this.verifyChunk(chunkIndex, data)
    this.storeChunk(chunkIndex, data, null)
    return false
  }

  private async verifyChunk(chunkIndex: number, data: Uint8Array) {
    const descriptor = this.manifest?.chunks.find((chunk) => chunk.index === chunkIndex)
    if (!descriptor) throw new Error(`missing manifest entry for chunk ${chunkIndex}`)
    if (data.byteLength !== descriptor.size) {
      throw new Error(`invalid size for chunk ${chunkIndex}`)
    }
    // Manifests generated by older hosts have no digest. Keep that protocol
    // compatible, but require the digest whenever a current host supplies it.
    if (!descriptor.sha256) return
    const hash = await crypto.subtle.digest('SHA-256', data.slice().buffer)
    const actual = Array.from(new Uint8Array(hash), (byte) => byte.toString(16).padStart(2, '0')).join('')
    if (actual !== descriptor.sha256) {
      throw new Error(`integrity check failed for chunk ${chunkIndex}`)
    }
  }

  private storeChunk(chunkIndex: number, data: Uint8Array, peer: PeerInfo | null) {
    if (this.downloadedChunks.has(chunkIndex)) return
    this.downloadedChunks.set(chunkIndex, data)

    if (peer) {
      this.p2pStats.value.fromPeers++
      this.downloadStats.value.fromPeerBytes += data.byteLength
      this.setChunkOwner(chunkIndex, { source: 'peer', peerId: peer.id, peerColor: peer.color })
    } else {
      this.p2pStats.value.fromHost++
      this.downloadStats.value.fromHostBytes += data.byteLength
      this.setChunkOwner(chunkIndex, { source: 'host' })
    }

    this.recordDownloadBytes(data.byteLength)
    this.mseStreamer?.onChunkReady(chunkIndex, data)
  }

  private recordDownloadBytes(bytes: number) {
    const now = Date.now()
    this.speedWindow.push({ time: now, bytes })
    const cutoff = now - 3000
    while (this.speedWindow.length > 0 && this.speedWindow[0].time < cutoff) {
      this.speedWindow.shift()
    }
    const totalBytes = this.speedWindow.reduce((sum, sample) => sum + sample.bytes, 0)
    const elapsedMs = Math.max(now - (this.speedWindow[0]?.time ?? now), 1)
    this.downloadStats.value.bytesPerSecond = (totalBytes / elapsedMs) * 1000
  }

  private updateProgress() {
    if (!this.manifest || this.manifest.totalChunks <= 0) {
      this.progress.value = 0
      return
    }
    this.progress.value = Math.round((this.downloadedChunks.size / this.manifest.totalChunks) * 100)
  }

  private raceBestPaths(
    candidates: SourceCandidate[],
    chunkIndex: number,
  ): Promise<{ data: Uint8Array; winner: PeerInfo | null }> {
    return new Promise((resolve, reject) => {
      let settled = false
      let failures = 0

      const resolveOnce = (data: Uint8Array, winner: PeerInfo | null) => {
        if (settled) return
        settled = true
        resolve({ data, winner })
      }

      const rejectOnce = () => {
        failures++
        if (failures === candidates.length && !settled) {
          reject(new Error(`all race paths failed for chunk ${chunkIndex}`))
        }
      }

      for (const candidate of candidates) {
        this.downloadFromSource(candidate, chunkIndex)
          .then(({ data, peer }) => resolveOnce(data, peer))
          .catch(rejectOnce)
      }
    })
  }

  private async downloadFromSource(
    candidate: SourceCandidate,
    chunkIndex: number,
  ): Promise<{ data: Uint8Array; fromPeer: boolean; peer: PeerInfo | null }> {
    const sourceKey = candidate.key
    this.trackSourceGroup(sourceKey, chunkIndex, 1)
    try {
      if (candidate.kind === 'peer' && candidate.peer) {
        const data = await this.downloadFromPeer(candidate.peer, chunkIndex)
        return { data, fromPeer: true, peer: candidate.peer }
      }
      const data = await this.downloadFromHost(chunkIndex)
      return { data, fromPeer: false, peer: null }
    } finally {
      this.trackSourceGroup(sourceKey, chunkIndex, -1)
    }
  }

  private downloadFromPeer(peer: PeerInfo, chunkIndex: number): Promise<Uint8Array> {
    return new Promise((resolve, reject) => {
      peer.pendingRequests++
      const startMs = Date.now()

      const timeout = setTimeout(() => {
        peer.pendingRequests = Math.max(0, peer.pendingRequests - 1)
        webrtcManager.off('chunk', onChunk)
        reject(new Error(`timeout chunk ${chunkIndex} from ${peer.id}`))
      }, PEER_CHUNK_TIMEOUT)

      const onChunk = (evt: { peerId: string; chunkIndex: number; data: Uint8Array }) => {
        if (evt.peerId !== peer.id || evt.chunkIndex !== chunkIndex) return
        clearTimeout(timeout)
        peer.pendingRequests = Math.max(0, peer.pendingRequests - 1)
        webrtcManager.off('chunk', onChunk)

        const speed = evt.data.byteLength / Math.max(Date.now() - startMs, 1)
        peer.speedBytesPerMs =
          peer.speedBytesPerMs === 0
            ? speed
            : 0.7 * peer.speedBytesPerMs + 0.3 * speed

        resolve(evt.data)
      }

      webrtcManager.on('chunk', onChunk)
      webrtcManager.send(peer.id, JSON.stringify({
        type: 'p2p_chunk_request',
        needChunks: [chunkIndex],
      }))
    })
  }

  private async downloadFromHost(chunkIndex: number): Promise<Uint8Array> {
    while (this.hostPendingRequests >= MAX_PENDING_HOST) {
      await new Promise((resolve) => setTimeout(resolve, 25))
    }
    this.hostPendingRequests++
    const startedAt = Date.now()
    try {
      const { response } = await this.fetchFromHostPath(`/video/chunk/${chunkIndex}`, HOST_REQUEST_TIMEOUT)
      if (!response.ok) throw new Error(`HTTP ${response.status} chunk ${chunkIndex}`)
      const data = new Uint8Array(await response.arrayBuffer())
      const speed = data.byteLength / Math.max(Date.now() - startedAt, 1)
      this.hostSpeedBytesPerMs =
        this.hostSpeedBytesPerMs === 0
          ? speed
          : 0.7 * this.hostSpeedBytesPerMs + 0.3 * speed
      return data
    } finally {
      this.hostPendingRequests = Math.max(0, this.hostPendingRequests - 1)
    }
  }

  private getOrderedHostEndpoints(): HostEndpoint[] {
    if (this.hostEndpoints.length === 0 && this.hostIp) {
      this.hostEndpoints = [{ host: this.hostIp, port: this.hostPort, failCount: 0 }]
    }
    const activeHost = this.hostIp
    return [...this.hostEndpoints].sort((a, b) => {
      if (a.host === activeHost) return -1
      if (b.host === activeHost) return 1
      if (a.failCount !== b.failCount) return a.failCount - b.failCount
      return (a.latencyMs ?? Number.MAX_SAFE_INTEGER) - (b.latencyMs ?? Number.MAX_SAFE_INTEGER)
    })
  }

  private async fetchFromHostPath(path: string, timeoutMs: number): Promise<{ response: Response; endpoint: HostEndpoint }> {
    let lastError: Error | null = null
    for (const endpoint of this.getOrderedHostEndpoints()) {
      const url = buildHttpURL(endpoint.host, endpoint.port, path)
      const controller = new AbortController()
      const timer = window.setTimeout(() => controller.abort(), timeoutMs)
      const startedAt = performance.now()

      try {
        const response = await fetch(url, {
          cache: 'no-store',
          mode: 'cors',
          signal: controller.signal,
          headers: this.mediaAccessToken ? { 'X-WT-Access-Token': this.mediaAccessToken } : undefined,
        })
        endpoint.failCount = 0
        endpoint.latencyMs = Math.round(performance.now() - startedAt)
        if (this.hostIp !== endpoint.host) {
          console.info('[p2p] switched host endpoint', {
            from: this.hostIp,
            to: endpoint.host,
            path,
            latencyMs: endpoint.latencyMs,
          })
        }
        this.hostIp = endpoint.host
        this.hostPort = endpoint.port
        return { response, endpoint }
      } catch (err) {
        endpoint.failCount++
        lastError = err instanceof Error ? err : new Error(String(err))
      } finally {
        clearTimeout(timer)
      }
    }

    throw lastError ?? new Error(`all host endpoints failed for ${path}`)
  }

  private setChunkOwner(chunkIndex: number, owner: ChunkOwner) {
    this.chunkOwners.set(chunkIndex, owner)
    let color = '#6b6b70'
    if (owner.source === 'peer' && owner.peerColor) color = owner.peerColor
    else if (owner.source === 'self') color = '#4ade80'
    this.chunkColorMap.set(chunkIndex, color)
  }

  receiveChunkFromPeer(_chunkIndex: number, _data: Uint8Array) {
    // Binary peer responses are consumed by the request-specific WebRTC listeners.
    // Keeping this method preserves the existing store wiring without double-storing.
  }

  getOwnedChunks(): number[] {
    return Array.from(this.downloadedChunks.keys())
  }

  getDownloadStatus(): P2PDownloadStatus {
    const total = this.manifest?.totalChunks ?? 0
    let bufferedSeconds = 0
    for (let index = 0; index < total; index++) {
      if (!this.downloadedChunks.has(index)) break
      bufferedSeconds += this.manifest?.chunks.find((chunk) => chunk.index === index)?.duration ?? 0
    }
    return {
      state: this.status.value === 'assembling' ? 'downloading' : this.status.value,
      progress: this.progress.value,
      bytesPerSecond: this.downloadStats.value.bytesPerSecond,
      bufferedSeconds,
      downloaded: this.downloadedChunks.size,
      total,
    }
  }

  async handleChunkRequest(peerId: string, chunkIndices: number[]) {
    for (const idx of chunkIndices) {
      const data = this.downloadedChunks.get(idx)
      if (!data) continue
      while (webrtcManager.getBufferAmount(peerId) > MAX_BUFFER_PER_PEER) {
        await new Promise((resolve) => setTimeout(resolve, 50))
      }
      await webrtcManager.sendChunk(peerId, idx, data)
      this.p2pStats.value.uploadedToPeers++
    }
  }

  getBlobUrl(): string | null {
    return this.mseStreamer?.url ?? null
  }

  destroy() {
    this.mseStreamer?.destroy()
    this.mseStreamer = null
    this.downloadedChunks.clear()
    this.chunkOwners.clear()
    this.chunkColorMap.clear()
    this.peers.clear()
    this.hostEndpoints = []
    this.mediaAccessToken = ''
    this.hostPendingRequests = 0
    this.hostSpeedBytesPerMs = 0
    this.inFlightChunks.clear()
    this.sourceGroupPending.clear()
    this.urgentQueue = []
    this.seekChunk = 1
    this.localPeerID = ''
    this.manifest = null
    this.status.value = 'idle'
    this.progress.value = 0
    this.errorMessage.value = ''
    this.isDownloading.value = false
    this.peerCount.value = 0
    this.p2pRatio.value = 0
    this.p2pStats.value = { fromHost: 0, fromPeers: 0, uploadedToPeers: 0 }
    this.downloadStats.value = { bytesPerSecond: 0, fromHostBytes: 0, fromPeerBytes: 0 }
    this.speedWindow = []
    this.peerList.splice(0, this.peerList.length)
  }
}

export const p2pChunkManager = new P2PChunkManager()
