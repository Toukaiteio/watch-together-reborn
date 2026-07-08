declare module 'dplayer' {
  interface DPlayerOptions {
    container: HTMLElement | null
    live?: boolean
    autoplay?: boolean
    theme?: string
    loop?: boolean
    lang?: string
    screenshot?: boolean
    hotkey?: boolean
    preload?: 'none' | 'metadata' | 'auto'
    volume?: number
    playbackSpeed?: boolean
    video: {
      url: string
      pic?: string
      type?: string
      customType?: Record<string, (video: HTMLVideoElement, player: any) => void>
      quality?: Array<{ name: string; url: string; type?: string }>
      defaultQuality?: number
    }
    danmaku?: {
      id: string
      api: string
      token?: string
      maximum?: number
      addition?: string[]
      user?: string
      bottom?: string
    }
    apiBackend?: {
      read?: (options: any) => Promise<any[]>
      send?: (options: any) => Promise<void>
    }
    contextmenu?: Array<{ text: string; link?: string; click?: (player: any) => void }>
    mutex?: boolean
  }

  class DPlayer {
    constructor(options: DPlayerOptions)
    play(): void
    pause(): void
    seek(time: number): void
    toggle(): void
    notice(text: string, time?: number, opacity?: number): void
    switchVideo(video: DPlayerOptions['video']): void
    destroy(): void
    on(event: string, handler: (...args: any[]) => void): void
    video: HTMLVideoElement
    container: HTMLElement
  }

  export default DPlayer
}
