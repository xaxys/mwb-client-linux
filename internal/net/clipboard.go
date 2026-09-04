package net

import (
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

// SendBeat announces new local clipboard data (Clipboard 69, ALL broadcast).
// The puller discovers the kind (text/image/file) from the 1024B header on
// the secondary channel, so the beat itself carries no payload besides the
// sender name and post-action.
func (c *Client) SendBeat(src uint32, name string, postAction int32) error {
	p := &protocol.Packet{Type: protocol.PtClipboard, ID: 0,
		Src: src, Des: protocol.IDAll, HasName: true, MachineName: name}
	p.SetPostAction(postAction)
	return c.Send(p)
}

// SendAsk requests a push-back from the data holder (ClipboardAsk 78).
// Used when we have no inbound leg from the holder to pull over.
func (c *Client) SendAsk(dest, src uint32, name string, postAction int32) error {
	p := &protocol.Packet{Type: protocol.PtClipboardAsk,
		Src: src, Des: dest, HasName: true, MachineName: name}
	p.SetPostAction(postAction)
	return c.Send(p)
}
