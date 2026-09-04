// Package crypto implements MWB stream encryption for both generations:
//
//   - current (PowerToys >= v0.101): per-direction random 16B salt + 16B IV,
//     32B cleartext header, PBKDF2-HMAC-SHA512 100,000 iterations.
//   - legacy (PowerToys < v0.101): fixed salt (UTF-16LE of
//     "18446744073709551615") + fixed IV (ASCII "1844674407370955"),
//     PBKDF2-HMAC-SHA512 50,000 iterations, no header.
//
// Magic (Magic24, 50,000x SHA-512) is framing/identity, unchanged by the
// 2026 rekey (see docs/protocol 02).
package crypto

import (
	"crypto/aes"
	"crypto/rand"
	"crypto/sha512"
	"errors"

	"golang.org/x/crypto/pbkdf2"
)

const (
	CurrentIterations = 100_000
	LegacyIterations  = 50_000
	KeyLen            = 32
	SaltLen           = 16
	IVLen             = 16
	HeaderLen         = 32 // salt + IV cleartext (current only)
	NoiseLen          = 16 // CBC-shift block
)

// LegacyFixedSalt = UTF-16LE bytes of "18446744073709551615".
func LegacyFixedSalt() []byte {
	s := "18446744073709551615"
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i], 0x00)
	}
	return out
}

// LegacyFixedIV = ASCII bytes of "1844674407370955" (16 bytes).
func LegacyFixedIV() []byte { return []byte("1844674407370955") }

// DeriveKey runs PBKDF2-HMAC-SHA512(password UTF-8, salt, iters) → 32B.
func DeriveKey(password string, salt []byte, iterations int) []byte {
	return pbkdf2.Key([]byte(password), salt, iterations, KeyLen, sha512.New)
}

// DeriveCurrent derives the per-direction key for the current protocol.
func DeriveCurrent(password string, salt []byte) []byte {
	return DeriveKey(password, salt, CurrentIterations)
}

// DeriveLegacy derives the shared legacy key (fixed salt).
func DeriveLegacy(password string) []byte {
	return DeriveKey(password, LegacyFixedSalt(), LegacyIterations)
}

// Magic24 derives the 32-bit magic via 50,000x SHA-512 over a 32B buffer
// holding the ASCII security key zero-padded, then:
// magic = hash[0]<<23 + hash[1]<<16 + hash[63]<<8 + hash[2].
func Magic24(securityKey string) uint32 {
	var buf [32]byte
	copy(buf[:], []byte(securityKey))
	h := sha512.Sum512(buf[:])
	for i := 0; i < 50_000; i++ {
		h = sha512.Sum512(h[:])
	}
	return uint32(h[0])<<23 | uint32(h[1])<<16 | uint32(h[63])<<8 | uint32(h[2])
}

// --- CBC helpers (Zeros padding; packets are already 16B-aligned) ---

func cbcEncrypt(key, iv, plain []byte) ([]byte, error) {
	if len(plain)%aes.BlockSize != 0 {
		return nil, errors.New("crypto: plaintext not block aligned")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(plain))
	prev := make([]byte, aes.BlockSize)
	copy(prev, iv)
	var tmp [aes.BlockSize]byte
	for i := 0; i < len(plain); i += aes.BlockSize {
		for j := 0; j < aes.BlockSize; j++ {
			tmp[j] = plain[i+j] ^ prev[j]
		}
		block.Encrypt(out[i:i+aes.BlockSize], tmp[:])
		copy(prev, out[i:i+aes.BlockSize])
	}
	return out, nil
}

func cbcDecrypt(key, iv, cipher []byte) ([]byte, error) {
	if len(cipher)%aes.BlockSize != 0 {
		return nil, errors.New("crypto: ciphertext not block aligned")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(cipher))
	prev := make([]byte, aes.BlockSize)
	copy(prev, iv)
	var tmp [aes.BlockSize]byte
	for i := 0; i < len(cipher); i += aes.BlockSize {
		block.Decrypt(tmp[:], cipher[i:i+aes.BlockSize])
		for j := 0; j < aes.BlockSize; j++ {
			out[i+j] = tmp[j] ^ prev[j]
		}
		copy(prev, cipher[i:i+aes.BlockSize])
	}
	return out, nil
}

// RandomBytes returns n cryptographically random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}
