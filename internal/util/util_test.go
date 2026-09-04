package util_test

import (
	"testing"

	"github.com/xaxys/mwb-client-linux/internal/util"
)

func TestNormalizeRoundTrip(t *testing.T) {
	v := util.Normalize(960, 0, 1920)
	if v < 32700 || v > 32800 {
		t.Fatalf("mid -> %d", v)
	}
	back := util.Denormalize(v, 0, 1920)
	if back < 958 || back > 962 {
		t.Fatalf("back -> %d", back)
	}
}

func TestSubnetCap(t *testing.T) {
	hosts := util.SubnetHosts([]byte{192, 168, 1, 10}, []byte{255, 255, 0, 0})
	if len(hosts) > 1022 {
		t.Fatalf("cap exceeded: %d", len(hosts))
	}
}
