package net

import (
	"fmt"
	"net"
	"sync"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Client dials the Windows host (message 15101 + clipboard 15100).
type Client struct {
	log    *util.Logger
	sender *Sender
	dedup  *Dedup

	mu      sync.Mutex
	msgConn *mwbcrypto.SecureConn
	magic   uint32
	pool    *Pool
	matrix  protocol.Matrix
	selfID  uint32
	self    string
}

// NewClient creates a client.
func NewClient(log *util.Logger) *Client {
	return &Client{log: log, sender: NewSender(0), dedup: &Dedup{}, pool: NewPool(log)}
}

// ConnectAuto probes [current, legacy] on fresh TCP conns (8s each),
// running noise + 10-round handshake, using whichever completes.
func (c *Client) ConnectAuto(host string, msgPort int, key, selfName string, pinned protocol.ProtocolVersion) (protocol.ProtocolVersion, error) {
	c.magic = mwbcrypto.Magic24(key)
	cands := []protocol.ProtocolVersion{protocol.ProtoCurrent, protocol.ProtoLegacy}
	if pinned == protocol.ProtoCurrent || pinned == protocol.ProtoLegacy {
		cands = []protocol.ProtocolVersion{pinned}
	}
	var lastErr error
	for _, v := range cands {
		sc, err := Dial(DialOption{Version: v, Host: host, MsgPort: msgPort, Key: key, Timeout: protocol.ConnectAttemptTimeout})
		if err != nil {
			lastErr = err
			continue
		}
		done := make(chan error, 1)
		var peer string
		go func() {
			p, err := ClientHandshake(sc, c.magic, c.sender, 0, selfName)
			peer = p
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				sc.Close()
				lastErr = err
				continue
			}
		case <-time.After(protocol.HandshakeAttemptTimeout):
			sc.Close()
			lastErr = fmt.Errorf("net: %s handshake timeout", v)
			continue
		}
		c.mu.Lock()
		c.msgConn = sc
		c.self = selfName
		c.mu.Unlock()
		c.log.Infof("connected via %s (peer %q)", v, peer)
		return v, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("net: no protocol candidate succeeded")
	}
	return "", lastErr
}

// Close proactively tears down sockets (sleep/shutdown parity: never leave
// zombie TCP claiming Connected==true across suspend).
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.msgConn != nil {
		c.msgConn.Close()
		c.msgConn = nil
	}
}

// Send fans one ID-assigned packet over the message leg (M1 single-leg;
// M4 mesh fans over every leg with the SAME id).
func (c *Client) Send(p *protocol.Packet) error {
	c.mu.Lock()
	sc := c.msgConn
	c.mu.Unlock()
	if sc == nil {
		return fmt.Errorf("net: not connected")
	}
	p.ID = c.sender.Next()
	wire, err := p.Encode(c.magic)
	if err != nil {
		return err
	}
	return sc.WritePacket(wire)
}

// SendHeartbeat broadcasts Heartbeat/Awake presence (ID.ALL).
func (c *Client) SendHeartbeat(awake bool, src uint32, name string) error {
	t := protocol.PtHeartbeat
	if awake {
		t = protocol.PtAwake
	}
	return c.Send(&protocol.Packet{Type: t, Src: src, Des: protocol.IDAll, HasName: true, MachineName: name})
}

// SendMatrix bursts 4 matrix packets (Src=slot 1..4); never broadcast empty.
func (c *Client) SendMatrix(m protocol.Matrix) error {
	if m.IsEmpty() {
		return fmt.Errorf("net: refuse to broadcast empty matrix (anti-clobber)")
	}
	t := m.TypeByte()
	for i := 0; i < protocol.MaxMachine; i++ {
		if err := c.Send(&protocol.Packet{Type: t, Src: uint32(i + 1), Des: protocol.IDAll, HasName: true, MachineName: m.Slots[i]}); err != nil {
			return err
		}
	}
	return nil
}

// ResolveHost maps an address field to dialable hosts: bare names try
// <name>.local (mDNS, Windows 10 1809+ answers) then the bare name;
// dotted names resolve as-is.
func ResolveHost(addr string) []string {
	if isIP(addr) {
		return []string{addr}
	}
	if !containsDot(addr) {
		return []string{addr + ".local", addr}
	}
	return []string{addr}
}

func isIP(s string) bool { return net.ParseIP(s) != nil }

func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
