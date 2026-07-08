export interface UserInfo {
  id: string
  name: string
  isHost: boolean
}

export interface VideoState {
  source: string
  sourceType: VideoSourceType
  playing: boolean
  currentTime: number
  speed: number
  localBlobUrl?: string // local override URL for playback
  localBlobSourceType?: VideoSourceType
  chunkManifest?: string
}

export interface ChunkingProgress {
  stage: string
  current: number
  total: number
  percent: number
}

export interface DownloadStats {
  bytesPerSecond: number
  fromHostBytes: number
  fromPeerBytes: number
}

export interface MagnetStreamStatus {
  state: 'starting' | 'fetching_metadata' | 'buffering_initial' | 'ready' | 'error'
  ready: boolean
  error?: string
  statusText?: string
  magnetUri?: string
  stream?: {
    magnetUri: string
    fileName: string
    fileSize: number
    bytesCompleted: number
    progress: number
    ready: boolean
    peerStats?: MagnetPeerStats
  }
}

export interface MagnetPeerStats {
  totalPeers: number
  pendingPeers: number
  activePeers: number
  connectedSeeders: number
  halfOpenPeers: number
  peerConns: number
  knownSwarmPeers: number
  piecesComplete: number
  bytesReadData: number
  bytesReadUsefulData: number
  metadataChunksRead: number
}

export type VideoSourceType = 'url' | 'hls' | 'flv' | 'dash' | 'file' | 'magnet'

export interface ChatMessage {
  userId: string
  username: string
  text: string
  timestamp: number
  isSelf?: boolean
}

export interface ConnectionInfo {
  ip: string
  port: number
  roomId: string
}

export interface IPv6AddrInfo {
  address: string
  isPublic: boolean
  isUla: boolean
  isTemporary: boolean
  type: 'public' | 'ula'
}

export interface RoomPasscode {
  ip: string
  passcode: string
  isIPv6Public?: boolean
  isIPv6ULA?: boolean
  isIPv6Temporary?: boolean
}

export interface HostProbeResult {
  host: string
  family: 'ipv4' | 'ipv6'
  ok: boolean
  latencyMs?: number
  error?: string
}

export interface WebRTCRouteInfo {
  targetId: string
  state: string
  protocol?: string
  localCandidateType?: string
  remoteCandidateType?: string
  localAddress?: string
  remoteAddress?: string
  localFamily?: 'ipv4' | 'ipv6' | 'unknown'
  remoteFamily?: 'ipv4' | 'ipv6' | 'unknown'
  currentRoundTripTimeMs?: number
  availableOutgoingBitrate?: number
}

export interface RelayConfig {
  enabled: boolean
  stunUrls: string[]
  turnUrls: string[]
  username: string
  credential: string
}

export interface LANRoomInfo {
  roomId: string
  username: string
  port: number
  ips: string[]
  from: string
  ageMs: number
}

export type WSMessageType =
  | 'create_room'
  | 'join_room'
  | 'leave_room'
  | 'room_created'
  | 'room_joined'
  | 'room_error'
  | 'room_closed'
  | 'user_joined'
  | 'user_left'
  | 'host_changed'
  | 'host_network_info'
  | 'chat'
  | 'video_source'
  | 'video_play'
  | 'video_seek'
  | 'video_speed'
  | 'webrtc_offer'
  | 'webrtc_answer'
  | 'webrtc_ice'
  | 'webrtc_request'
  | 'p2p_manifest'
  | 'p2p_chunk_offer'
  | 'p2p_chunk_request'
  | 'p2p_chunk_data'

export interface WSMessage {
  type: WSMessageType
  roomId?: string
  username?: string
  userId?: string
  text?: string
  timestamp?: number
  source?: string
  sourceType?: string
  chunkManifest?: string
  playing?: boolean
  currentTime?: number
  speed?: number
  target?: string
  from?: string
  sdp?: string
  candidate?: RTCIceCandidateInit
  users?: UserInfo[]
  hostId?: string
  message?: string
  hostCandidates?: string[]
  preferredHostIp?: string
  chunkManifestData?: string
  // P2P chunk fields
  chunks?: number[]
  needChunks?: number[]
  chunkIndex?: number
  chunkData?: string
}

// P2P Chunk types
export interface ChunkManifest {
  fileName: string
  mimeCodec: string
  segmentTime: number
  totalDuration: number
  totalChunks: number
  complete: boolean
  chunks: Array<{
    index: number
    path: string
    duration: number
    startTime: number
    size: number
    isInit?: boolean
  }>
}

export interface P2PChunkMessage {
  type: 'p2p_chunk_offer' | 'p2p_chunk_request' | 'p2p_chunk_data'
  // offer: which chunks this peer has (bitfield or array)
  chunks?: number[]
  // request: which chunks are requested
  needChunks?: number[]
  // data: chunk index + base64 data
  chunkIndex?: number
  chunkData?: string
  chunkHash?: string
}

export type Theme = 'light' | 'dark'

export type View = 'home' | 'room'

export type SidebarTab = 'chat' | 'users' | 'host'
