package net

import (
	"testing"

	mwbcrypto "github.com/xaxys/mwb-client-linux/internal/crypto"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
)

func TestSendBeatAndAsk(t *testing.T) {
	const key = "beat-test"
	sca, scb := loopbackPair(t, key)
	defer sca.Close()
	defer scb.Close()
	magic := mwbcrypto.Magic24(key)
	c := testClient(t, sca, key)

	if err := c.SendBeat(1, "LINUX", 1); err != nil {
		t.Fatal(err)
	}
	p := readOne(t, scb, magic, true)
	if p.Type != protocol.PtClipboard || p.Des != protocol.IDAll || p.MachineName != "LINUX" {
		t.Fatalf("beat %+v", p)
	}
	if p.GetPostAction() != 1 {
		t.Fatalf("beat postaction %d", p.GetPostAction())
	}

	if err := c.SendAsk(2, 1, "LINUX", 0); err != nil {
		t.Fatal(err)
	}
	p = readOne(t, scb, magic, true)
	if p.Type != protocol.PtClipboardAsk || p.Des != 2 || p.Src != 1 {
		t.Fatalf("ask %+v", p)
	}
}
