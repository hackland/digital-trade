export interface WsMessage {
  type: string
  data: any
}

export interface WsSubscribeRequest {
  action: 'subscribe' | 'unsubscribe'
  channels: string[]
}
