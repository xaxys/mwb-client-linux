package net

import (
	"fmt"
	"sync"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Pool maps MachineName -> ID (1..4) with liveness timestamps.
// Mirrors MachinePool: alive if socket connected OR heartbeat within window.
type Pool struct {
	mu    sync.Mutex
	ids   map[string]uint32
	times map[string]time.Time
	log   *util.Logger
}

// NewPool creates an empty pool.
func NewPool(log *util.Logger) *Pool {
	return &Pool{ids: map[string]uint32{}, times: map[string]time.Time{}, log: log}
}

// Learn records name->id from an extended packet's MachineName + Src.
func (p *Pool) Learn(name string, id uint32) {
	if name == "" || id == 0 || id > 4 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// One name ↔ one slot; evict stale holders of the same slot.
	for n, v := range p.ids {
		if v == id && n != name {
			delete(p.ids, n)
			delete(p.times, n)
		}
	}
	p.ids[name] = id
	p.times[name] = time.Now()
}

// IDOf resolves a name to its slot ID (0 if unknown).
func (p *Pool) IDOf(name string) uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ids[name]
}

// Touch refreshes liveness.
func (p *Pool) Touch(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.ids[name]; ok {
		p.times[name] = time.Now()
	}
}

// Serialize renders "HostA:1,HostB:2,," persistence form.
func (p *Pool) Serialize() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := ""
	for n, id := range p.ids {
		s += fmt.Sprintf("%s:%d,", n, id)
	}
	return s + ","
}
