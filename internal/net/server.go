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

// Server is the standalone listener (ServerMode): binds 15101 message +
// 15100 clipboard with no outbound dial. Mirrors TcpServer rebind policy
// (6x500ms) and MainTCPRoutine trust + presence + matrix burst.
type Server struct {
	log      *util.Logger
	sender   *Sender
	dedup    *Dedup
	pool     *Pool
	msgPort  int
	clipPort int
	key      string
	version  protocol.ProtocolVersion
	self     string

	mu       sync.Mutex
	msgLn    net.Listener
	clipLn   net.Listener
	legs     map[string]*legEntry
	dialing  map[string]bool
	matrix   protocol.Matrix
	stopCh   chan struct{}
	stopOnce sync.Once

	// OnClipboardConn, when set, owns validated 15100 legs (the daemon
	// wires the clipboard Manager Serve path here). Otherwise legs are
	// staged under "clip:"+name. Args: peer name, leg, peerPush (peer
	// sent Push=79 and will send payload), peer post-action.
	OnClipboardConn func(peer string, sc *mwbcrypto.SecureConn, peerPush bool, postAction int32)
}

// legEntry is one peer leg; outbound marks legs we dialed (mesh parity:
// UpdateTCPClients dials every matrix machine we lack a client leg to).
type legEntry struct {
	sc       *mwbcrypto.SecureConn
	outbound bool
}

// NewServer creates a server (version must be current|legacy, never auto).
func NewServer(log *util.Logger, msgPort, clipPort int, key, self string, v protocol.ProtocolVersion) *Server {
	if v == protocol.ProtoAuto {
		v = protocol.ProtoCurrent
	}
	return &Server{log: log, sender: NewSender(0), dedup: &Dedup{}, pool: NewPool(log),
		msgPort: msgPort, clipPort: clipPort, key: key, self: self, version: v,
		legs: map[string]*legEntry{}, dialing: map[string]bool{}}
}

// Listen binds both ports with 6x500ms rebind retries (WSAEADDRINUSE parity).
func (s *Server) Listen() error {
	var err error
	s.msgLn, err = bindWithRetry(s.msgPort)
	if err != nil {
		return fmt.Errorf("bind 15101: %w", err)
	}
	s.clipLn, err = bindWithRetry(s.clipPort)
	if err != nil {
		s.msgLn.Close()
		return fmt.Errorf("bind 15100: %w", err)
	}
	s.stopCh = make(chan struct{})
	go s.acceptLoop(s.msgLn, true)
	go s.acceptLoop(s.clipLn, false)
	return nil
}

func bindWithRetry(port int) (net.Listener, error) {
	var ln net.Listener
	var err error
	for i := 0; i < protocol.RebindRetries; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			return ln, nil
		}
		time.Sleep(protocol.RebindDelay)
	}
	return nil, err
}

func (s *Server) acceptLoop(ln net.Listener, isMsg bool) {
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
				continue
			}
		}
		go s.handleInbound(c, isMsg)
	}
}

func (s *Server) handshakeFor(c net.Conn) (*mwbcrypto.SecureConn, error) {
	if s.version == protocol.ProtoLegacy {
		return mwbcrypto.HandshakeLegacy(c, s.key)
	}
	return mwbcrypto.HandshakeCurrent(c, s.key)
}

func (s *Server) handleInbound(c net.Conn, isMsg bool) {
	peerIP := remoteIP(c)
	sc, err := s.handshakeFor(c)
	if err != nil {
		c.Close()
		return
	}
	magic := mwbcrypto.Magic24(s.key)
	if !isMsg {
		// Clipboard port: trust is inherited from the message leg —
		// the Push/Clipboard header name must map to a known ID with a
		// live message leg, otherwise the socket is dropped.
		s.handleClipboardLeg(sc, magic)
		return
	}
	peer, err := ServerHandshake(sc, magic, s.sender, 0, s.self)
	if err != nil {
		sc.Close()
		return
	}
	s.trustPeer(sc, peer, peerIP, magic)
}

// remoteIP extracts the inbound peer IP for mesh dial-back.
func remoteIP(c net.Conn) string {
	addr, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok || addr == nil {
		return ""
	}
	return addr.IP.String()
}

// handleClipboardLeg validates the 15100 Push/Clipboard header against the
// pool (name must be learned with a live message leg) and stages the leg.
func (s *Server) handleClipboardLeg(sc *mwbcrypto.SecureConn, magic uint32) {
	raw, err := sc.ReadPacket(true)
	if err != nil {
		sc.Close()
		return
	}
	p, err := protocol.Decode(raw, magic)
	if err != nil {
		sc.Close()
		return
	}
	if p.Type != protocol.PtClipboardPush && p.Type != protocol.PtClipboard {
		sc.Close()
		return
	}
	name := p.MachineName
	s.mu.Lock()
	_, hasLeg := s.legs[name]
	cb := s.OnClipboardConn
	s.mu.Unlock()
	if name == "" || s.pool.IDOf(name) == 0 || !hasLeg {
		s.log.Warnf("clipboard leg from unknown %q rejected (no message leg)", name)
		sc.Close()
		return
	}
	if cb != nil {
		cb(name, sc, p.Type == protocol.PtClipboardPush, p.GetPostAction())
		return
	}
	s.mu.Lock()
	s.legs["clip:"+name] = &legEntry{sc: sc}
	s.mu.Unlock()
	s.log.Infof("clipboard leg from %q staged", name)
}

func (s *Server) trustPeer(sc *mwbcrypto.SecureConn, peer, peerIP string, magic uint32) {
	s.mu.Lock()
	s.legs[peer] = &legEntry{sc: sc}
	// Anti-clobber: fresh server adopts [self, peer] before broadcasting.
	empty := s.matrix.IsEmpty()
	if empty {
		s.matrix = protocol.AdoptFresh(s.self, peer)
	}
	m := s.matrix
	slot := m.SlotOf(peer)
	selfSlot := m.SlotOf(s.self)
	if selfSlot == 0 {
		selfSlot = 1
	}
	s.mu.Unlock()
	if slot != 0 {
		s.pool.Learn(peer, slot)
	}
	s.log.Infof("trusted peer %q (fresh-adopt=%v slot=%d)", peer, empty, slot)
	// Presence + matrix burst so the newcomer learns name/layout immediately.
	if err := s.sendPresence(sc, magic, selfSlot); err != nil {
		s.dropLeg(peer)
		sc.Close()
		return
	}
	if err := s.sendMatrixBurst(sc, magic, m); err != nil {
		s.dropLeg(peer)
		sc.Close()
		return
	}
	// Mesh dial-back (UpdateTCPClients parity): one outbound leg per peer,
	// attempted once; the inbound leg already carries traffic if it fails.
	s.maybeDialBack(peer, peerIP)
}

// sendPresence emits Heartbeat_ex on the new leg.
func (s *Server) sendPresence(sc *mwbcrypto.SecureConn, magic uint32, selfSlot uint32) error {
	p := &protocol.Packet{Type: protocol.PtHeartbeatEx, ID: s.sender.Next(),
		Src: selfSlot, Des: protocol.IDAll, HasName: true, MachineName: s.self}
	wire, err := p.Encode(magic)
	if err != nil {
		return err
	}
	return sc.WritePacket(wire)
}

// sendMatrixBurst emits the 4 layout packets (Src 1..4) on the new leg.
func (s *Server) sendMatrixBurst(sc *mwbcrypto.SecureConn, magic uint32, m protocol.Matrix) error {
	t := m.TypeByte()
	for i := 0; i < protocol.MaxMachine; i++ {
		p := &protocol.Packet{Type: t, ID: s.sender.Next(), Src: uint32(i + 1),
			Des: protocol.IDAll, HasName: true, MachineName: m.Slots[i]}
		wire, err := p.Encode(magic)
		if err != nil {
			return err
		}
		if err := sc.WritePacket(wire); err != nil {
			return err
		}
	}
	return nil
}

// maybeDialBack opens the outbound mesh leg unless one exists or a dial is
// already in flight.
func (s *Server) maybeDialBack(peer, peerIP string) {
	if peer == "" || peerIP == "" || peer == s.self {
		return
	}
	s.mu.Lock()
	if _, ok := s.legs[peer+"#out"]; ok {
		s.mu.Unlock()
		return
	}
	if s.dialing[peer] {
		s.mu.Unlock()
		return
	}
	s.dialing[peer] = true
	s.mu.Unlock()
	go s.dialBack(peer, peerIP)
}

// dialBack runs one outbound handshake to the peer message port.
func (s *Server) dialBack(peer, peerIP string) {
	defer func() {
		s.mu.Lock()
		delete(s.dialing, peer)
		s.mu.Unlock()
	}()
	sc, err := Dial(DialOption{Version: s.version, Host: peerIP, MsgPort: s.msgPort,
		Key: s.key, Timeout: protocol.ConnectAttemptTimeout})
	if err != nil {
		s.log.Warnf("mesh dial-back to %q: %v", peer, err)
		return
	}
	magic := mwbcrypto.Magic24(s.key)
	if _, err := ClientHandshake(sc, magic, s.sender, 0, s.self); err != nil {
		s.log.Warnf("mesh dial-back handshake to %q: %v", peer, err)
		sc.Close()
		return
	}
	s.mu.Lock()
	s.legs[peer+"#out"] = &legEntry{sc: sc, outbound: true}
	s.mu.Unlock()
	s.log.Infof("mesh dial-back to %q established", peer)
}

func (s *Server) dropLeg(name string) {
	s.mu.Lock()
	delete(s.legs, name)
	s.mu.Unlock()
}

// Stop closes listeners and all legs.
func (s *Server) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msgLn != nil {
		s.msgLn.Close()
	}
	if s.clipLn != nil {
		s.clipLn.Close()
	}
	for _, e := range s.legs {
		e.sc.Close()
	}
	s.legs = map[string]*legEntry{}
}

// MsgAddr returns the bound message listener address (port 0 → ephemeral).
func (s *Server) MsgAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.msgLn == nil {
		return nil
	}
	return s.msgLn.Addr()
}

// ClipAddr returns the bound clipboard listener address.
func (s *Server) ClipAddr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clipLn == nil {
		return nil
	}
	return s.clipLn.Addr()
}
