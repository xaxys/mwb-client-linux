package protocol

import "sync"

// Dedup drops duplicate packets using a 50-entry circular window of IDs.
// Mirrors PowerToys RecentProcessedPackageIDs: SkSend assigns each packet
// its ID ONCE and fans it over every socket, so dedup is ONE process-wide
// window shared by all legs — never per-connection.
type Dedup struct {
	mu   sync.Mutex
	ids  [DedupWindow]int32
	pos  int
	seen int
}

// Seen returns true if id was already processed (duplicate → drop).
// Otherwise it records id and returns false.
func (d *Dedup) Seen(id int32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := d.seen
	if n > DedupWindow {
		n = DedupWindow
	}
	for i := 0; i < n; i++ {
		if d.ids[i] == id {
			return true
		}
	}
	d.ids[d.pos] = id
	d.pos = (d.pos + 1) % DedupWindow
	if d.seen < DedupWindow {
		d.seen++
	}
	return false
}

// Reset clears the window (used only in tests).
func (d *Dedup) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ids = [DedupWindow]int32{}
	d.pos = 0
	d.seen = 0
}
