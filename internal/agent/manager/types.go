package manager

import "time"

type PeerMetric struct {
	PeerID   string
	RXBytes  int64
	TXBytes  int64
	LastSeen time.Time
	Endpoint string
	IsOnline bool
}
