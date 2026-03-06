package ws

// Message is a WebSocket message sent to clients.
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// SubscribeRequest is a client request to subscribe/unsubscribe channels.
type SubscribeRequest struct {
	Action   string   `json:"action"`   // "subscribe" | "unsubscribe"
	Channels []string `json:"channels"` // e.g. ["kline:BTCUSDT:5m", "signal", "order"]
}
