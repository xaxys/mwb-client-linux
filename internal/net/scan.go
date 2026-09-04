package net

import (
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// ScanResult is one live MWB node (15101 open).
type ScanResult struct {
	IP   string
	Name string // best-effort friendly name (NetBIOS137 vs PTR race)
}

// Scan probes TCP 15101 across candidates (0.6s timeout, 64 concurrency).
func Scan(candidates []string, log *util.Logger) []ScanResult {
	sem := make(chan struct{}, protocol.LANScanConcurrency)
	var mu sync.Mutex
	var out []ScanResult
	var wg sync.WaitGroup
	for _, ip := range candidates {
		ip := ip
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			d := net.Dialer{Timeout: protocol.LANScanTimeout}
			c, err := d.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(protocol.MessagePort)))
			if err != nil {
				return
			}
			c.Close()
			name := ResolveName(ip)
			mu.Lock()
			out = append(out, ScanResult{IP: ip, Name: name})
			mu.Unlock()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
	return out
}

// ResolveName races NetBIOS Node Status (UDP 137) against system PTR
// (getnameinfo NI_NAMEREQD incl. mDNS); first success wins. M0 implements
// the PTR leg + persisted-known-host shortcut; NetBIOS leg staged for M1.
func ResolveName(ip string) string {
	if names, err := net.LookupAddr(ip); err == nil && len(names) > 0 {
		n := names[0]
		// strip trailing dot
		for len(n) > 0 && n[len(n)-1] == '.' {
			n = n[:len(n)-1]
		}
		return n
	}
	return ip
}
