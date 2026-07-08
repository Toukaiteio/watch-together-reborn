import type { WSMessage } from '@/types'

type EventHandler = (data: any) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers = new Map<string, Set<EventHandler>>()
  private url = ''
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private shouldReconnect = false

  get connected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  get connecting(): boolean {
    return this.ws?.readyState === WebSocket.CONNECTING
  }

  async connect(url: string): Promise<void> {
    this.url = url
    this.shouldReconnect = true
    console.info('[ws] connect requested', { url })
    return this.doConnect()
  }

  private doConnect(): Promise<void> {
    return new Promise((resolve, reject) => {
      if (this.ws) {
        this.ws.close()
        this.ws = null
      }

      const ws = new WebSocket(this.url)
      this.ws = ws
      // Track per-connection whether it ever opened successfully.
      // Only reconnect on drops from established connections, not on initial
      // connect failures — otherwise a bad passcode keeps retrying in the
      // background and corrupts the next createRoom attempt.
      let opened = false
      let settled = false

      ws.onopen = () => {
        opened = true
        settled = true
        console.info('[ws] open', { url: this.url })
        this.emit('open')
        resolve()
      }

      ws.onclose = (event) => {
        console.warn('[ws] close', {
          url: this.url,
          code: event.code,
          reason: event.reason,
          wasClean: event.wasClean,
          opened,
        })
        this.emit('close', event)
        if (!settled) {
          settled = true
          reject(new Error(`WebSocket closed before open (code=${event.code}, reason=${event.reason || 'n/a'})`))
          return
        }
        if (opened && this.shouldReconnect) {
          this.scheduleReconnect()
        }
      }

      ws.onerror = (event) => {
        console.error('[ws] error', { url: this.url, event })
        this.emit('error', event)
        if (!settled) {
          settled = true
          reject(new Error(`WebSocket connection failed: ${this.url}`))
        }
      }

      ws.onmessage = (event) => {
        try {
          const msg: WSMessage = JSON.parse(event.data)
          console.debug('[ws] message', msg)
          this.emit('message', msg)
        } catch (err) {
          console.error('Failed to parse WS message:', err)
        }
      }
    })
  }

  private scheduleReconnect() {
    if (!this.shouldReconnect) return
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.doConnect().catch(() => {})
    }, 2000)
  }

  disconnect() {
    this.shouldReconnect = false
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
  }

  send(msg: WSMessage) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      console.debug('[ws] send', msg)
      this.ws.send(JSON.stringify(msg))
    } else {
      console.warn('[ws] send skipped, socket not open', {
        readyState: this.ws?.readyState,
        msg,
      })
    }
  }

  on(event: string, handler: EventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set())
    }
    this.handlers.get(event)!.add(handler)
  }

  off(event: string, handler: EventHandler) {
    this.handlers.get(event)?.delete(handler)
  }

  private emit(event: string, data?: any) {
    this.handlers.get(event)?.forEach((h) => h(data))
  }
}

export const wsClient = new WebSocketClient()
