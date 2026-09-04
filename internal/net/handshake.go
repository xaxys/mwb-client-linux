package net

import (
	"fmt"
	"net"
	"sync"
	"time"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// Conn is one authenticated MWB leg: encrypted stream + magic + dedup.
type Conn struct {
	SC    *mwbcrypto.SecureConn
	Magic uint32
	Dedup *protocol.Dedup

	muW sync.Mutex
}

// DialOption selects the encryption generation for one attempt.
type DialOption struct {
	Version protocol.ProtocolVersion // current or legacy (never auto here)
	Host    string
	MsgPort int
	Key     string
	Timeout time.Duration
}

// Dial opens a fresh TCP connection and runs header+noise setup.
// Each Auto candidate gets its own fresh TCP (a mismatched generation may
// have desynced/closed the stream — never reuse across attempts).
func Dial(opt DialOption) (*mwbcrypto.SecureConn, error) {
	addr := fmt.Sprintf("%s:%d", opt.Host, opt.MsgPort)
	d := net.Dialer{Timeout: opt.Timeout}
	if d.Timeout == 0 {
		d.Timeout = protocol.ConnectAttemptTimeout
	}
	c, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	switch opt.Version {
	case protocol.ProtoLegacy:
		sc, err := mwbcrypto.HandshakeLegacy(c, opt.Key)
		if err != nil {
			c.Close()
			return nil, err
		}
		return sc, nil
	default:
		sc, err := mwbcrypto.HandshakeCurrent(c, opt.Key)
		if err != nil {
			c.Close()
			return nil, err
		}
		return sc, nil
	}
}

// ClientHandshake runs the 10-round mutual challenge/response as the dialer.
// It sends 10 Handshake packets and expects 10 HandshakeAcks verifying ~challenge.
// ownName is our machine name; returns peer name from the last ack.
func ClientHandshake(sc *mwbcrypto.SecureConn, magic uint32, sender *Sender, selfID uint32, ownName string) (string, error) {
	type pending struct {
		ch []byte
	}
	// Send all 10 challenges first (both sides act simultaneously; the peer's
	// MainTCPRoutine also sends 10 while reading — full-duplex).
	challenges := make([][]byte, 0, protocol.HandshakeRounds)
	for i := 0; i < protocol.HandshakeRounds; i++ {
		p, ch, err := protocol.NewChallenge(sender.Next(), selfID, ownName)
		if err != nil {
			return "", err
		}
		wire, err := p.Encode(magic)
		if err != nil {
			return "", err
		}
		if err := sc.WritePacket(wire); err != nil {
			return "", err
		}
		challenges = append(challenges, ch)
	}
	// Read loop: answer peer Handshakes with Acks, collect our Acks.
	acked := make([]bool, protocol.HandshakeRounds)
	verified := 0
	peerName := ""
	deadline := time.Now().Add(protocol.HandshakeAttemptTimeout)
	_ = deadline
	for verified < protocol.HandshakeRounds {
		if err := setReadDeadline(sc, protocol.HandshakeAttemptTimeout); err != nil {
			return "", err
		}
		raw, err := sc.ReadPacket(true) // handshake/ack are 64B
		if err != nil {
			return "", err
		}
		pkt, err := protocol.Decode(raw, magic)
		if err != nil {
			return "", err
		}
		switch pkt.Type {
		case protocol.PtHandshake:
			ack := protocol.AckChallenge(pkt, ownName, sender.Next())
			wire, _ := ack.Encode(magic)
			if err := sc.WritePacket(wire); err != nil {
				return "", err
			}
			if peerName == "" {
				peerName = pkt.MachineName
			}
		case protocol.PtHandshakeAck:
			// verify against any outstanding challenge (ordered in practice)
			matched := false
			for i, ch := range challenges {
				if !acked[i] && protocol.VerifyAck(pkt, ch) {
					acked[i] = true
					matched = true
					verified++
					break
				}
			}
			if !matched {
				return "", fmt.Errorf("net: handshake ack mismatch")
			}
			if peerName == "" {
				peerName = pkt.MachineName
			}
		default:
			return "", fmt.Errorf("net: unexpected %d during handshake", byte(pkt.Type))
		}
	}
	return peerName, nil
}

// ServerHandshake runs the acceptor side: identical symmetric logic.
// Reads peer challenges (replying Ack) while sending our own 10 challenges.
func ServerHandshake(sc *mwbcrypto.SecureConn, magic uint32, sender *Sender, selfID uint32, ownName string) (string, error) {
	return ClientHandshake(sc, magic, sender, selfID, ownName)
}

func setReadDeadline(sc *mwbcrypto.SecureConn, d time.Duration) error {
	// SecureConn hides net.Conn; deadline support staged for M1 (mock uses
	// net.Pipe without deadlines). No-op here — outer attempt timeout applies.
	return nil
}
