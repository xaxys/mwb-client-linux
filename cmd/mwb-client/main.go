// Command mwb-client is the MWB Linux daemon + CLI.
//
// Subcommands: status | scan | connect | version.
// connect --host/--key default to the config file values.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/xaxys/mwb-client-linux/internal/config"
	"github.com/xaxys/mwb-client-linux/internal/input"
	mwbnet "github.com/xaxys/mwb-client-linux/internal/net"
	"github.com/xaxys/mwb-client-linux/internal/protocol"
	"github.com/xaxys/mwb-client-linux/internal/ui"
	"github.com/xaxys/mwb-client-linux/internal/util"
)

// Version is the client version (M0 scaffold).
const Version = "v0.1.0-m1"

func selfName(cfg config.Config) string {
	if cfg.MachineName != "" {
		return cfg.MachineName
	}
	h, err := os.Hostname()
	if err == nil && h != "" {
		if i := strings.IndexByte(h, '.'); i > 0 {
			return h[:i]
		}
		return h
	}
	return "LINUX"
}

func cmdStatus() int {
	log := util.NewLogger("mwb")
	k := input.Probe(log)
	fmt.Println(ui.DescribeBackend(k))
	fmt.Printf("session: XDG_SESSION_TYPE=%s XDG_CURRENT_DESKTOP=%s\n",
		os.Getenv("XDG_SESSION_TYPE"), os.Getenv("XDG_CURRENT_DESKTOP"))
	fmt.Printf("config: %s\n", config.Path())
	return 0
}

func cmdScan() int {
	log := util.NewLogger("mwb")
	cands := util.CandidatesForLAN()
	fmt.Printf("scanning %d candidates...\n", len(cands))
	for _, r := range mwbnet.Scan(cands, log) {
		fmt.Printf("%s\t%s\n", r.IP, r.Name)
	}
	return 0
}

func cmdConnect(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: load config: %v\n", err)
		return 1
	}
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	host := fs.String("host", cfg.ClientHost, "host to dial (IP or machine name)")
	key := fs.String("key", cfg.ClientKey, "security key (outbound)")
	proto := fs.String("protocol", string(cfg.Protocol), "protocol: auto|current|legacy")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *host == "" {
		fmt.Fprintln(os.Stderr, "connect: --host required (or set clientHost in config)")
		return 2
	}
	if *key == "" {
		fmt.Fprintln(os.Stderr, "connect: --key required (or set clientKey in config)")
		return 2
	}
	log := util.NewLogger("mwb")
	c := mwbnet.NewClient(log)
	self := selfName(cfg)
	hosts := mwbnet.ResolveHost(*host)
	var ver protocol.ProtocolVersion
	var lastErr error
	for _, h := range hosts {
		ver, lastErr = c.ConnectAuto(h, cfg.MessagePort, *key, self, parseProto(*proto))
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", lastErr)
		return 1
	}
	fmt.Printf("connected via %s (self %q); Ctrl-C to disconnect\n", ver, self)
	defer c.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("disconnected")
	return 0
}

func parseProto(s string) protocol.ProtocolVersion {
	switch protocol.ProtocolVersion(strings.ToLower(s)) {
	case protocol.ProtoCurrent, protocol.ProtoLegacy:
		return protocol.ProtocolVersion(strings.ToLower(s))
	default:
		return protocol.ProtoAuto
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: mwb-client <status|scan|connect|version> [flags]")
	fmt.Fprintln(os.Stderr, "  connect --host NAME --key KEY [--protocol auto|current|legacy]")
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(cmdStatus())
	}
	var code int
	switch os.Args[1] {
	case "status":
		code = cmdStatus()
	case "scan":
		code = cmdScan()
	case "connect":
		code = cmdConnect(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(Version)
	default:
		usage()
		code = 2
	}
	os.Exit(code)
}
