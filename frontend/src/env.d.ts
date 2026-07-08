/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

declare module 'dplayer' {
  export default class DPlayer {
    [key: string]: any
    constructor(options: any)
  }
}

export {}

declare global {
  interface Window {
    go: {
      main: {
        App: {
          StartServer(): Promise<number>
          StopServer(): Promise<void>
          GetServerPort(): Promise<number>
          IsDefaultPort(): Promise<boolean>
          IsServerRunning(): Promise<boolean>
          GetLocalIPs(): Promise<string[]>
          GetLocalIPv6s(): Promise<string[]>
          GetIPv6Addresses(): Promise<Array<{ address: string; isPublic: boolean; isUla: boolean; isTemporary: boolean; type: 'public' | 'ula' }>>
          DecodePasscode(code: string): Promise<{ ip: string; port: number; roomId: string }>
          EncodePasscode(ip: string, port: number, roomID: string): Promise<string>
          StartLANRoomBroadcast(roomID: string, port: number, username: string): Promise<void>
          StopLANRoomBroadcast(): Promise<void>
          DiscoverLANRooms(timeoutMs: number): Promise<Array<{ roomId: string; username: string; port: number; ips: string[]; from: string; ageMs: number }>>
          SelectVideoFile(): Promise<string>
          ServeVideoFile(filePath: string): Promise<string>
          ServeVideoFileSegmented(filePath: string): Promise<string>
          ServeVideoFileChunked(filePath: string): Promise<string>
          ServeMagnetVideo(magnetURI: string): Promise<string>
          StopVideoServe(): Promise<void>
        }
      }
    }
  }
}
