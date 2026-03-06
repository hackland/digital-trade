import { ref } from 'vue'
import type { WsMessage } from '@/types/ws'

type Callback = (data: any) => void

class WsManager {
  private ws: WebSocket | null = null
  private subscribers = new Map<string, Set<Callback>>()
  private reconnectDelay = 1000
  private maxDelay = 30000
  private url: string

  connected = ref(false)

  constructor() {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    this.url = `${proto}//${window.location.host}/api/v1/ws`
  }

  connect() {
    if (this.ws?.readyState === WebSocket.OPEN) return

    this.ws = new WebSocket(this.url)

    this.ws.onopen = () => {
      this.connected.value = true
      this.reconnectDelay = 1000
      // Re-subscribe existing channels
      const channels = Array.from(this.subscribers.keys())
      if (channels.length > 0) {
        this.ws?.send(JSON.stringify({ action: 'subscribe', channels }))
      }
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WsMessage = JSON.parse(event.data)
        this.dispatch(msg.type, msg.data)
      } catch { /* ignore */ }
    }

    this.ws.onclose = () => {
      this.connected.value = false
      setTimeout(() => this.connect(), this.reconnectDelay)
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxDelay)
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  subscribe(channel: string, cb: Callback) {
    if (!this.subscribers.has(channel)) {
      this.subscribers.set(channel, new Set())
      // Send subscribe if already connected
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ action: 'subscribe', channels: [channel] }))
      }
    }
    this.subscribers.get(channel)!.add(cb)
    return () => this.unsubscribe(channel, cb)
  }

  unsubscribe(channel: string, cb: Callback) {
    const subs = this.subscribers.get(channel)
    if (subs) {
      subs.delete(cb)
      if (subs.size === 0) {
        this.subscribers.delete(channel)
        if (this.ws?.readyState === WebSocket.OPEN) {
          this.ws.send(JSON.stringify({ action: 'unsubscribe', channels: [channel] }))
        }
      }
    }
  }

  private dispatch(type: string, data: any) {
    // Dispatch to type-based subscribers
    this.subscribers.get(type)?.forEach((cb) => cb(data))
    // Dispatch to channel-based (e.g., "kline:BTCUSDT:5m")
    if (data?.symbol && data?.interval) {
      const channel = `kline:${data.symbol}:${data.interval}`
      this.subscribers.get(channel)?.forEach((cb) => cb(data))
    }
  }

  disconnect() {
    this.ws?.close()
  }
}

// Singleton
let instance: WsManager | null = null

export function useWebSocket() {
  if (!instance) {
    instance = new WsManager()
    instance.connect()
  }
  return instance
}
