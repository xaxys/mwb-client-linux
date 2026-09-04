// Package util holds logging, screen bounds, and LAN helpers.
package util

import (
	"fmt"
	"log"
	"net"
	"os"
)

// Logger is a thin leveled wrapper (stdlib only, M0).
type Logger struct {
	prefix string
	l      *log.Logger
}

// NewLogger creates a logger writing to stderr.
func NewLogger(prefix string) *Logger {
	return &Logger{prefix: prefix, l: log.New(os.Stderr, "["+prefix+"] ", log.LstdFlags)}
}

func (l *Logger) Infof(f string, a ...any)  { l.l.Printf("INFO "+f, a...) }
func (l *Logger) Warnf(f string, a ...any)  { l.l.Printf("WARN "+f, a...) }
func (l *Logger) Errorf(f string, a ...any) { l.l.Printf("ERROR "+f, a...) }

// Rect is a screen/desktop bounding box in pixels.
type Rect struct {
	Left, Top, Right, Bottom int
}

func (r Rect) Width() int  { return r.Right - r.Left }
func (r Rect) Height() int { return r.Bottom - r.Top }

// Normalize maps a pixel p in [lo,hi) to 0..65535.
func Normalize(p, lo, hi int) int {
	if hi <= lo {
		return 0
	}
	if p < lo {
		p = lo
	}
	if p >= hi {
		p = hi - 1
	}
	return (p - lo) * 65535 / (hi - lo)
}

// Denormalize maps 0..65535 back to pixels in [lo,hi).
func Denormalize(v, lo, hi int) int {
	if v < 0 {
		v = 0
	}
	if v > 65535 {
		v = 65535
	}
	return lo + v*(hi-lo)/65535
}

// LocalIPv4s enumerates local IPv4 addresses (for subnet scanning).
func LocalIPv4s() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				out = append(out, v4)
			}
		}
	}
	return out
}

// SubnetHosts expands an IP+mask to candidate host IPs (cap 1022).
func SubnetHosts(ip net.IP, mask net.IPMask) []net.IP {
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	m := net.IPv4Mask(mask[0], mask[1], mask[2], mask[3])
	network := v4.Mask(m)
	ones, bits := m.Size()
	if bits != 32 {
		return nil
	}
	total := 1 << (32 - ones)
	if total > 1024 {
		total = 1024 // cap per spec (1022 probed hosts)
	}
	var out []net.IP
	base := uint32(network[0])<<24 | uint32(network[1])<<16 | uint32(network[2])<<8 | uint32(network[3])
	for i := uint32(1); i < uint32(total)-1; i++ {
		n := base + i
		out = append(out, net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)))
		if len(out) >= 1022 {
			break
		}
	}
	return out
}

// CandidatesForLAN returns /24 candidates for each local IPv4 (M0 fallback).
func CandidatesForLAN() []string {
	var out []string
	for _, ip := range LocalIPv4s() {
		base := ip.Mask(net.CIDRMask(24, 32))
		b := base.To4()
		if b == nil {
			continue
		}
		for i := 1; i < 255; i++ {
			out = append(out, fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], i))
			if len(out) >= 1022 {
				return out
			}
		}
	}
	return out
}
