package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// SecureConn wraps a TCP conn in the MWB encrypted stream.
//
// Wire format per direction (current):
// [32B cleartext header (salt+IV)][16B ciphertext noise][packets...]
// Legacy omits the 32B header; both sides share the fixed key/IV but keep
// independent CBC chaining, and still exchange the 16B noise.
//
// CBC chaining continues across the whole stream lifetime.
type SecureConn struct {
	conn net.Conn
	enc  cipher.BlockMode // CBC encrypter (outbound chaining)
	dec  cipher.BlockMode // CBC decrypter (inbound chaining)
	muW  sync.Mutex
	muR  sync.Mutex
}

// HandshakeCurrent performs the current-generation setup on an open TCP
// conn: send our 32B header, read peer header, derive directional keys,
// exchange 16B noise. Both sides run the same steps symmetrically.
func HandshakeCurrent(conn net.Conn, securityKey string) (*SecureConn, error) {
	salt := make([]byte, SaltLen)
	iv := make([]byte, IVLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	header := append(append([]byte{}, salt...), iv...)
	if _, err := conn.Write(header); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	peer := make([]byte, HeaderLen)
	if _, err := io.ReadFull(conn, peer); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	return finishHandshake(conn, securityKey, salt, iv, peer[:SaltLen], peer[SaltLen:])
}

// HandshakeLegacy performs the legacy setup: no header, fixed key/IV,
// then the 16B noise exchange.
func HandshakeLegacy(conn net.Conn, securityKey string) (*SecureConn, error) {
	key := DeriveLegacy(securityKey)
	iv := LegacyFixedIV()
	return finishHandshakeWithKeys(conn, key, iv, key, iv)
}

func finishHandshake(conn net.Conn, key string, mySalt, myIV, peerSalt, peerIV []byte) (*SecureConn, error) {
	encKey := DeriveCurrent(key, mySalt)
	decKey := DeriveCurrent(key, peerSalt)
	return finishHandshakeWithKeys(conn, encKey, myIV, decKey, peerIV)
}

func finishHandshakeWithKeys(conn net.Conn, encKey, encIV, decKey, decIV []byte) (*SecureConn, error) {
	encBlock, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	decBlock, err := aes.NewCipher(decKey)
	if err != nil {
		return nil, err
	}
	sc := &SecureConn{
		conn: conn,
		enc:  cipher.NewCBCEncrypter(encBlock, append([]byte{}, encIV...)),
		dec:  cipher.NewCBCDecrypter(decBlock, append([]byte{}, decIV...)),
	}
	// CBC shift: both sides write 16B random then read 16B.
	noise := make([]byte, NoiseLen)
	if _, err := io.ReadFull(rand.Reader, noise); err != nil {
		return nil, err
	}
	if err := sc.WriteRaw(noise); err != nil {
		return nil, fmt.Errorf("write noise: %w", err)
	}
	discard := make([]byte, NoiseLen)
	if err := sc.ReadRaw(discard); err != nil {
		return nil, fmt.Errorf("read noise: %w", err)
	}
	return sc, nil
}

// WriteRaw encrypts len%16==0 plaintext and writes ciphertext.
func (s *SecureConn) WriteRaw(plain []byte) error {
	if len(plain)%aes.BlockSize != 0 {
		return fmt.Errorf("crypto: unaligned write %d", len(plain))
	}
	s.muW.Lock()
	defer s.muW.Unlock()
	out := make([]byte, len(plain))
	s.enc.CryptBlocks(out, plain)
	_, err := s.conn.Write(out)
	return err
}

// ReadRaw reads exactly len(buf) decrypted bytes (len%16==0).
func (s *SecureConn) ReadRaw(buf []byte) error {
	if len(buf)%aes.BlockSize != 0 {
		return fmt.Errorf("crypto: unaligned read %d", len(buf))
	}
	s.muR.Lock()
	defer s.muR.Unlock()
	tmp := make([]byte, len(buf))
	if _, err := io.ReadFull(s.conn, tmp); err != nil {
		return err
	}
	s.dec.CryptBlocks(buf, tmp)
	return nil
}

// WritePacket encrypts one 32/64B protocol packet.
func (s *SecureConn) WritePacket(wire []byte) error {
	return s.WriteRaw(wire)
}

// ReadPacket reads one packet; extended reports whether to read 64B.
// Caller peeks the type first via ReadPacketWithPeek or knows the context
// (handshake/matrix are always 64B). Use ReadPacketAuto for dispatch loop.
func (s *SecureConn) ReadPacket(extended bool) ([]byte, error) {
	if extended {
		buf := make([]byte, 64)
		if err := s.ReadRaw(buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	buf := make([]byte, 32)
	if err := s.ReadRaw(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// Close closes the underlying conn.
func (s *SecureConn) Close() error { return s.conn.Close() }

// SetReadDeadline passthrough (clipboard legs use handshake timeouts).
func (s *SecureConn) SetReadDeadline(t time.Time) error {
	return s.conn.SetReadDeadline(t)
}

// SetWriteDeadline passthrough (bounds large secondary transfers).
func (s *SecureConn) SetWriteDeadline(t time.Time) error {
	return s.conn.SetWriteDeadline(t)
}

// RemoteAddr passthrough.
func (s *SecureConn) RemoteAddr() net.Addr { return s.conn.RemoteAddr() }
