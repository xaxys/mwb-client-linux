package main

import (
	"fmt"
	"time"

	"github.com/xaxys/mwb-client-linux/internal/clipboard"
	"github.com/xaxys/mwb-client-linux/internal/config"
	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/host"
	"github.com/xaxys/mwb-client-linux/internal/input"
	mwbnet "github.com/xaxys/mwb-client-linux/internal/net"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// runStack wires the full station: input backend + host switch loop +
// dispatch callbacks + clipboard manager on top of the listening server.
// A nil backend means net-only mode (no input hardware usable honestly).
func runStack(s *mwbnet.Server, cfg config.Config, selfName string, key string, log *util.Logger, stop <-chan struct{}) {
	kind := input.Probe(log)
	be, err := openBackend(kind)
	if err != nil {
		log.Warnf("input backend %s unavailable (%v): net-only mode", kind, err)
		fmt.Printf("input: net-only (%v)\n", err)
		return
	}
	defer be.Close()
	fmt.Printf("input: %s\n", be.Name())

	self, m := s.Layout()
	mgr := clipboard.NewManager(selfName, "", log,
		func() {
			if err := s.SendBeat(self, selfName, int32(clipboard.PostOther)); err != nil {
				log.Warnf("beat: %v", err)
			}
		},
		func(dest uint32) {
			if err := s.SendAsk(dest, self, selfName, int32(clipboard.PostOther)); err != nil {
				log.Warnf("ask: %v", err)
			}
		},
	)
	byName := reverseHosts(cfg.KnownHosts)

	h := host.New(be, s, log, self, selfName, m)
	s.Handler = mwbnet.LegHandler{
		OnMatrix: func(mm protocol.Matrix) { h.SetMatrix(mm) },
		OnNextMachine: func(x, y int, dest uint32) {
			log.Infof("switch: focus back entry=(%d,%d) dest=%d", x, y, dest)
			h.OnNextMachine(x, y)
			go pullOnReturn(s, mgr, byName, cfg, selfName, self, key, log)
		},
		OnKey: func(vk, flags int32, src uint32) {
			if h.Current() != self {
				return
			}
			if err := be.Inject(input.Event{Kind: input.KindKey, VK: int(vk), KeyDown: flags == protocol.KeyFlagDown}); err != nil {
				log.Warnf("inject key: %v", err)
			}
		},
		OnMouse: func(mm protocol.MouseEvent, src uint32) {
			if h.Current() != self {
				return
			}
			var ev input.Event
			switch {
			case mm.Flags&0x0800 != 0:
				ev = input.Event{Kind: input.KindMouseWheel, Wheel: int(mm.WheelDelta)}
			case mm.Flags != 0:
				ev = input.Event{Kind: input.KindMouseButton, MouseFlag: mm.Flags}
			default:
				m := mm
				if m.IsRelative() {
					// Relative deltas stay relative for the backend.
					ev = input.Event{Kind: input.KindMouseMove, Rel: true}
					if m.X >= 0 {
						ev.X = int(m.X) - protocol.MoveMouseRelative
					} else {
						ev.X = int(m.X) + protocol.MoveMouseRelative
					}
					if m.Y >= 0 {
						ev.Y = int(m.Y) - protocol.MoveMouseRelative
					} else {
						ev.Y = int(m.Y) + protocol.MoveMouseRelative
					}
				} else {
					b := be.Bounds()
					ev = input.Event{Kind: input.KindMouseMove,
						X: util.Denormalize(int(m.X), b.Left, b.Right),
						Y: util.Denormalize(int(m.Y), b.Top, b.Bottom)}
				}
			}
			if err := be.Inject(ev); err != nil {
				log.Warnf("inject mouse: %v", err)
			}
		},
		OnHideMouse: func() { _ = be.HideCursor() },
		OnBeat: func(src uint32, name string, pa int32) {
			mgr.OnBeat(name, src)
		},
		OnAsk: func(src uint32, name string, pa int32) {
			ip, ok := byName[name]
			if !ok {
				log.Warnf("ask from %q: no known IP", name)
				return
			}
			go pushTo(s, mgr, ip, cfg.ClipboardPort, key, selfName, self, log)
		},
	}
	s.OnClipboardConn = func(peer string, sc *mwbcrypto.SecureConn, peerPush bool, pa int32) {
		defer sc.Close()
		magic := mwbcrypto.Magic24(key)
		provide := func() (clipboard.TransferKind, string, []byte, bool) {
			return mgr.Provide()
		}
		if err := clipboard.Serve(sc, magic, selfName, self, clipboard.PostAction(pa),
			s.NextID(), provide, mgr.Sink, "", 60*time.Second); err != nil {
			log.Warnf("clipboard serve %q: %v", peer, err)
		}
	}

	go func() {
		if err := h.Run(stop); err != nil {
			log.Warnf("host: %v", err)
		}
	}()
	<-stop
}

// pullOnReturn fetches the holder clipboard when focus comes back to us.
func pullOnReturn(s *mwbnet.Server, mgr *clipboard.Manager, byName map[string]string, cfg config.Config, selfName string, self uint32, key string, log *util.Logger) {
	holder, _ := mgr.LastMachine()
	if holder == "" {
		return
	}
	ip, ok := byName[holder]
	if !ok {
		log.Warnf("pull: no known IP for %q", holder)
		return
	}
	sc, err := mwbnet.Dial(mwbnet.DialOption{Version: protoPinned(cfg), Host: ip,
		MsgPort: cfg.ClipboardPort, Key: key, Timeout: protocol.ConnectAttemptTimeout})
	if err != nil {
		log.Warnf("pull dial %q: %v", holder, err)
		return
	}
	defer sc.Close()
	r, err := clipboard.Pull(sc, mwbcrypto.Magic24(key), selfName, self,
		clipboard.PostOther, s.NextID(), "", 60*time.Second)
	if err != nil {
		log.Warnf("pull %q: %v", holder, err)
		return
	}
	if err := mgr.Sink(r); err != nil {
		log.Warnf("pull sink: %v", err)
	}
	log.Infof("pull %q: kind=%d", holder, r.Kind)
}

// pushTo answers an Ask by dialing the requester and serving our data.
func pushTo(s *mwbnet.Server, mgr *clipboard.Manager, ip string, clipPort int, key, selfName string, self uint32, log *util.Logger) {
	kind, name, data, ok := mgr.Provide()
	if !ok {
		log.Warnf("push: nothing to provide")
		return
	}
	sc, err := mwbnet.Dial(mwbnet.DialOption{Version: protocol.ProtoCurrent, Host: ip,
		MsgPort: clipPort, Key: key, Timeout: protocol.ConnectAttemptTimeout})
	if err != nil {
		log.Warnf("push dial: %v", err)
		return
	}
	defer sc.Close()
	magic := mwbcrypto.Magic24(key)
	hdr := clipboard.Header{Push: true, Name: selfName, Src: self}
	if err := clipboard.WriteHeaderPacket(sc, magic, hdr, s.NextID()); err != nil {
		log.Warnf("push header: %v", err)
		return
	}
	_ = kind
	if err := clipboard.SendTransfer(sc, name, data); err != nil {
		log.Warnf("push send: %v", err)
		return
	}
	log.Infof("push %q done", ip)
}

func protoPinned(cfg config.Config) protocol.ProtocolVersion {
	if cfg.ServerProtocol == protocol.ProtoLegacy {
		return protocol.ProtoLegacy
	}
	return protocol.ProtoCurrent
}

// reverseHosts builds name → IP from the known-hosts table.
func reverseHosts(known map[string]string) map[string]string {
	out := map[string]string{}
	for ip, name := range known {
		if _, ok := out[name]; !ok {
			out[name] = ip
		}
	}
	return out
}

// openBackend constructs the probed backend.
func openBackend(kind input.BackendKind) (input.Backend, error) {
	switch kind {
	case input.BackendX11:
		return input.NewX11Backend()
	case input.BackendPortal:
		return input.NewPortalBackend()
	case input.BackendEvdev:
		return input.NewEvdevBackend()
	}
	return nil, fmt.Errorf("unknown session (need X11, portal, or evdev)")
}
