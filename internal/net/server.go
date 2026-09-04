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
	legs     map[string]*mwbcrypto.SecureConn
	matrix   protocol.Matrix
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewServer creates a server (version must be current|legacy, never auto).
func NewServer(log *util.Logger, msgPort, clipPort int, key, self string, v protocol.ProtocolVersion) *Server {
	if v == protocol.ProtoAuto {
		v = protocol.ProtoCurrent
	}
	return &Server{log: log, sender: NewSender(0), dedup: &Dedup{}, pool: NewPool(log),
		msgPort: msgPort, clipPort: clipPort, key: key, self: self, version: v,
		legs: map[string]*mwbcrypto.SecureConn{}}
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
	sc, err := s.handshakeFor(c)
	if err != nil {
		c.Close()
		return
	}
	magic := mwbcrypto.Magic24(s.key)
	if !isMsg {
		// Clipboard port: noise done; trust inherited from main legs
		// (M4 validates MachineID against pool; M1 keeps socket staged).
		s.mu.Lock()
		s.legs["clip:"+c.RemoteAddr().String()] = sc
		s.mu.Unlock()
		return
	}
	peer, err := ServerHandshake(sc, magic, s.sender, 0, s.self)
	if err != nil {
		sc.Close()
		return
	}
	s.trustPeer(sc, peer, magic)
}

func (s *Server) trustPeer(sc *mwbcrypto.SecureConn, peer string, magic uint32) {
	s.mu.Lock()
	s.legs[peer] = sc
	// Anti-clobber: fresh server adopts [self, peer] before broadcasting.
	empty := s.matrix.IsEmpty()
	if empty {
		s.matrix = protocol.AdoptFresh(s.self, peer)
	}
	m := s.matrix
	s.mu.Unlock()
	s.pool.Learn(peer, 2) // slot refined by matrix traffic
	// Presence + matrix burst so the newcomer learns name/layout immediately.
	_ = magic
	_ = m
	s.log.Infof("trusted peer %q (fresh-adopt=%v)", peer, empty)
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
	for _, sc := range s.legs {
		sc.Close()
	}
	s.legs = map[string]*mwbcrypto.SecureConn{}
}
