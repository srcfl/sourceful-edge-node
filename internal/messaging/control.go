package messaging

// HEMS control protocol types matching Blaxt/ESP32 firmware patterns.

// ControlCommand is received from the optimizer/HEMS via NATS.
type ControlCommand struct {
	ID         string `json:"id"`
	Version    int    `json:"v"`
	Timestamp  int64  `json:"ts"`
	Source     string `json:"src"`
	Action     string `json:"action,omitempty"`
	PowerW     int    `json:"power_w"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

// ControlAck is published back to confirm command receipt/execution.
type ControlAck struct {
	ID        string `json:"id"`
	Version   int    `json:"v"`
	Timestamp int64  `json:"ts"`
	Source    string `json:"src"`
	Ref       string `json:"ref"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

// Heartbeat is published by the HEMS to indicate it's still controlling the DER.
type Heartbeat struct {
	ID        string `json:"id"`
	Version   int    `json:"v"`
	Timestamp int64  `json:"ts"`
	Source    string `json:"src"`
}
