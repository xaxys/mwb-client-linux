// Package net implements the MWB mesh transport: outbound client dial
// (15101 message + 15100 clipboard), inbound dual listeners, heartbeat,
// matrix sync, LAN scan, and the process-wide 50-entry dedup window.
//
// Mirrors PowerToys SocketStuff/TcpServer/Common + macOS NetworkManager,
// ServerListener, HeartbeatService, LANScanner, NameResolver.
package net

import (
	"sync/atomic"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// Sender assigns each packet its ID ONCE and fans it over every socket
// (SkSend parity). The receiver dedups on ONE process-wide 50-entry window.
type Sender struct {
	next int64
}

// NewSender starts IDs at start (PowerToys uses an incrementing int).
func NewSender(start int32) *Sender { return &Sender{next: int64(start)} }

// Next returns the next sequence ID.
func (s *Sender) Next() int32 { return int32(atomic.AddInt64(&s.next, 1)) }

// Shared dedup across all legs (global per process, like RecentProcessedPackageIDs).
type Dedup = protocol.Dedup
