// estate.go — the inventory layer: what WireGuard actually exists, everywhere.
//
// One estate is many hosts. Each host is reached exactly the way zxplore
// reaches its servers: plain ssh, no agent on the far side, ~/.ssh/config as
// the machine inventory. On every host we run `wg show all dump` and fold the
// result into one list of Devices (interfaces) and Peers.
//
// The reconciliation this enables is the point (docs/WG-NETWORKS-DESIGN.md):
//
//	declared + live   → healthy, coloured by handshake age
//	declared, absent  → never came up / host down
//	live, UNdeclared  → nobody added this peer: alarm
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Peer is one cryptokey-routed counterpart on some Device.
type Peer struct {
	PublicKey  string
	Endpoint   string
	AllowedIPs string
	Handshake  time.Time // zero = never
	RxBytes    int64
	TxBytes    int64
	Keepalive  string
	Declared   bool // matched a member in a declaration
}

// Device is one WireGuard interface on one host.
type Device struct {
	Host       string // "" = local
	Name       string // wg-mgmt, wg-k8s, wgx-lab …
	PublicKey  string
	ListenPort string
	Peers      []Peer
	Err        string // populated when the host could not be reached
}

// Health buckets a peer by handshake age — the "is this tunnel alive?"
// question that `wg show` answers only as a raw timestamp.
func (p Peer) Health() string {
	if p.Handshake.IsZero() {
		return "never"
	}
	switch d := time.Since(p.Handshake); {
	case d < 3*time.Minute:
		return "alive"
	case d < 30*time.Minute:
		return "quiet"
	default:
		return "stale"
	}
}

// Age renders the handshake age compactly ("42s", "6m12s", "never").
func (p Peer) Age() string {
	if p.Handshake.IsZero() {
		return "never"
	}
	return time.Since(p.Handshake).Round(time.Second).String()
}

// sshHosts returns the machines to scan.
//
// Source of truth, in order:
//  1. an explicit inventory — ~/.config/wgx/hosts or /etc/wgx/hosts, one
//     ssh target per line (# comments ok). If present, ONLY these are used.
//  2. otherwise ~/.ssh/config Host aliases, minus the ones that obviously
//     are not machines.
//
// That subtraction matters: an ssh config is full of git remotes. Scanning
// them produced a tree full of "github.com — unreachable", "gh-brain —
// unreachable" (onyx, 2026-08-03). A forge is identified by `User git`,
// by a known forge hostname, or by the gh-* alias convention.
func sshHosts() []string {
	if h := inventoryFile(); len(h) > 0 {
		return h
	}
	f, err := os.Open(filepath.Join(os.Getenv("HOME"), ".ssh", "config"))
	if err != nil {
		return nil
	}
	defer f.Close()

	forge := map[string]bool{
		"github.com": true, "gitlab.com": true, "bitbucket.org": true,
		"codeberg.org": true, "git.sr.ht": true, "ssh.dev.azure.com": true,
		"git.launchpad.net": true, "gitea.com": true,
	}
	type entry struct {
		aliases  []string
		hostName string
		user     string
	}
	var entries []entry
	var cur *entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch strings.ToLower(fields[0]) {
		case "host":
			entries = append(entries, entry{aliases: fields[1:]})
			cur = &entries[len(entries)-1]
		case "hostname":
			if cur != nil && len(fields) > 1 {
				cur.hostName = fields[1]
			}
		case "user":
			if cur != nil && len(fields) > 1 {
				cur.user = fields[1]
			}
		}
	}

	var out []string
	for _, e := range entries {
		if e.user == "git" || forge[strings.ToLower(e.hostName)] {
			continue // a git remote, not a machine
		}
		for _, a := range e.aliases {
			if strings.ContainsAny(a, "*?!") { // pattern, not a host
				continue
			}
			if forge[strings.ToLower(a)] || strings.HasPrefix(a, "gh-") {
				continue
			}
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// inventoryFile reads an explicit host list, if the operator wrote one.
// One ssh target per line; blank lines and # comments ignored.
func inventoryFile() []string {
	for _, p := range []string{
		filepath.Join(os.Getenv("HOME"), ".config", "wgx", "hosts"),
		"/etc/wgx/hosts",
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var out []string
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			out = append(out, l)
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// parseDump folds `wg show all dump` into Devices. Format per line:
//
//	interface: name privkey pubkey port fwmark
//	peer:      name pubkey psk endpoint allowed-ips handshake rx tx keepalive
func parseDump(host, dump string) []Device {
	var devs []Device
	byName := map[string]*Device{}
	for _, line := range strings.Split(strings.TrimSpace(dump), "\n") {
		f := strings.Split(line, "\t")
		switch len(f) {
		case 5: // interface
			d := Device{Host: host, Name: f[0], PublicKey: f[2], ListenPort: f[3]}
			devs = append(devs, d)
			byName[f[0]] = &devs[len(devs)-1]
		case 9: // peer
			d, ok := byName[f[0]]
			if !ok {
				continue
			}
			p := Peer{
				PublicKey:  f[1],
				Endpoint:   f[3],
				AllowedIPs: f[4],
				Keepalive:  f[8],
			}
			if hs, _ := strconv.ParseInt(f[5], 10, 64); hs > 0 {
				p.Handshake = time.Unix(hs, 0)
			}
			p.RxBytes, _ = strconv.ParseInt(f[6], 10, 64)
			p.TxBytes, _ = strconv.ParseInt(f[7], 10, 64)
			d.Peers = append(d.Peers, p)
		}
	}
	return devs
}

// CollectEstate gathers devices from the local host and every ssh alias, in
// parallel. Unreachable hosts become a Device carrying Err rather than
// vanishing — "the box is down" is inventory information, not an error.
func CollectEstate(hosts []string) []Device {
	var (
		mu  sync.Mutex
		all []Device
		wg  sync.WaitGroup
	)

	collect := func(host string) {
		defer wg.Done()
		var out []byte
		var err error
		if host == "" {
			out, err = localDump()
		} else {
			out, err = exec.Command("ssh",
				"-o", "BatchMode=yes",
				"-o", "StrictHostKeyChecking=accept-new",
				"-o", "ConnectTimeout=6",
				host, "wg show all dump").Output()
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			all = append(all, Device{Host: host, Name: "—", Err: shortErr(err)})
			return
		}
		all = append(all, parseDump(host, string(out))...)
	}

	wg.Add(1)
	go collect("")
	for _, h := range hosts {
		wg.Add(1)
		go collect(h)
	}
	wg.Wait()

	sort.Slice(all, func(i, j int) bool {
		if all[i].Host != all[j].Host {
			return all[i].Host < all[j].Host
		}
		return all[i].Name < all[j].Name
	})
	return all
}

// localDump reads this host's WireGuard state, elevating if it has to.
//
// `wg show` needs CAP_NET_ADMIN. Unprivileged it does something worse than
// failing: it prints "Operation not permitted" to stderr, writes NOTHING to
// stdout, and exits 0 — so a naive caller sees an empty estate and reports
// the local box as having no interfaces. That is exactly what the desktop
// launcher hit (fiend 2026-08-03): the tile runs as the operator, the GUI
// showed nothing, and "localhost unreachable" was the only clue.
//
// Order: direct (already root) → sudo -n (passwordless sudoers) → pkexec
// (polkit; kldload ships org.kldload.wgxplore.policy so wheel members can
// take this read-only dump without a password prompt on every refresh).
func localDump() ([]byte, error) {
	direct, err := exec.Command("wg", "show", "all", "dump").Output()
	if err == nil && len(bytes.TrimSpace(direct)) > 0 {
		return direct, nil
	}
	if os.Geteuid() == 0 {
		return direct, err // root and genuinely empty: no interfaces
	}
	// Nothing readable and we are not root. If the kernel has no WireGuard
	// interfaces at all, empty is the truth — say so without prompting.
	if links, lerr := exec.Command("ip", "-br", "link", "show", "type", "wireguard").Output(); lerr == nil &&
		len(bytes.TrimSpace(links)) == 0 {
		return nil, nil
	}
	self, _ := os.Executable()
	for _, try := range [][]string{
		{"sudo", "-n", "wg", "show", "all", "dump"},
		{"pkexec", self, "dump"},
	} {
		if _, lerr := exec.LookPath(try[0]); lerr != nil {
			continue
		}
		if out, eerr := exec.Command(try[0], try[1:]...).Output(); eerr == nil &&
			len(bytes.TrimSpace(out)) > 0 {
			return out, nil
		}
	}
	return nil, errors.New("needs privileges to read WireGuard state — run `sudo wgx`, or install the polkit rule (kldload ships it)")
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// MarkDeclared flags peers whose public key appears in any declaration under
// /etc/wgx. Everything left unflagged in a network we manage is a peer nobody
// declared — the row worth waking up for.
func MarkDeclared(devs []Device) map[string]string {
	known := map[string]string{} // pubkey → "network/member"
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return known
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		n, err := loadNet(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		for _, m := range n.Members {
			known[m.PublicKey] = n.Name + "/" + m.Name
		}
	}
	for i := range devs {
		for j := range devs[i].Peers {
			if _, ok := known[devs[i].Peers[j].PublicKey]; ok {
				devs[i].Peers[j].Declared = true
			}
		}
	}
	return known
}

// Totals summarises an estate for the header line.
func Totals(devs []Device) (hosts, ifaces, peers, alive, undeclared int) {
	seen := map[string]bool{}
	for _, d := range devs {
		seen[d.Host] = true
		if d.Err != "" {
			continue
		}
		ifaces++
		for _, p := range d.Peers {
			peers++
			if p.Health() == "alive" {
				alive++
			}
			if !p.Declared {
				undeclared++
			}
		}
	}
	return len(seen), ifaces, peers, alive, undeclared
}

// PrintEstate renders the estate as a text tree — the same host → interface
// → peer shape the GUI shows, for terminals, scripts and remote checks.
func PrintEstate() error {
	devs := CollectEstate(sshHosts())
	MarkDeclared(devs)
	h, i, p, alive, undeclared := Totals(devs)
	fmt.Printf("estate: %d hosts · %d interfaces · %d peers · %d alive", h, i, p, alive)
	if undeclared > 0 {
		fmt.Printf("  ⚠ %d UNDECLARED", undeclared)
	}
	fmt.Println()
	lastHost := "\x00"
	for _, d := range devs {
		host := d.Host
		if host == "" {
			host = "local"
		}
		if host != lastHost {
			fmt.Printf("\n%s\n", host)
			lastHost = host
		}
		if d.Err != "" {
			fmt.Printf("  └─ unreachable: %s\n", d.Err)
			continue
		}
		fmt.Printf("  ├─ %s  %s  (%d peers)\n", d.Name, ifaceSubnetText(d), len(d.Peers))
		for j, pr := range d.Peers {
			br := "│  ├─"
			if j == len(d.Peers)-1 {
				br = "│  └─"
			}
			mark := map[string]string{"alive": "●", "quiet": "●", "stale": "○", "never": "○"}[pr.Health()]
			flag := ""
			if !pr.Declared {
				flag = "  ⚠ undeclared"
			}
			fmt.Printf("  %s %s %-18s %-24s %s%s\n", br, mark,
				strings.Split(pr.AllowedIPs, ",")[0], orDash(pr.Endpoint), pr.Age(), flag)
		}
	}
	return nil
}

// ifaceSubnetText mirrors the GUI's subnet summary for the text tree.
func ifaceSubnetText(d Device) string {
	seen := map[string]bool{}
	var nets []string
	for _, p := range d.Peers {
		for _, cidr := range strings.Split(p.AllowedIPs, ",") {
			ip := strings.Split(strings.TrimSpace(cidr), "/")[0]
			parts := strings.Split(ip, ".")
			if len(parts) != 4 {
				continue
			}
			n := fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
			if !seen[n] {
				seen[n] = true
				nets = append(nets, n)
			}
		}
	}
	if len(nets) == 0 {
		return "—"
	}
	return strings.Join(nets, " ")
}

// DevicesOverview renders the top bar: one line per WireGuard interface,
// the way zxplore's pools bar renders one line per zpool. Same shape, same
// glance-value — this is the estate's "what have I got and is it healthy".
//
//	✓  wg-mgmt          10.250.0.0/24     4 peers ·  4 alive · port 51820 · rx 12.3M / tx 8.1M
//	⚠  wg-k8s           10.251.0.0/24     4 peers ·  0 alive · port 51821 · 4 undeclared
func DevicesOverview(devs []Device) string {
	if len(devs) == 0 {
		return "  (no WireGuard interfaces found)"
	}
	var b strings.Builder
	for _, d := range devs {
		if d.Err != "" {
			fmt.Fprintf(&b, "✗  %-16s %s\n", hostLabel(d.Host), "unreachable: "+d.Err)
			continue
		}
		var alive, undecl int
		var rx, tx int64
		for _, p := range d.Peers {
			if p.Health() == "alive" {
				alive++
			}
			if !p.Declared {
				undecl++
			}
			rx += p.RxBytes
			tx += p.TxBytes
		}
		mark := "✓"
		if len(d.Peers) > 0 && alive == 0 {
			mark = "⚠"
		}
		name := d.Name
		if d.Host != "" {
			name = d.Host + ":" + d.Name
		}
		fmt.Fprintf(&b, "%s  %-18s %-17s %2d peers · %2d alive · port %-6s rx %s / tx %s",
			mark, name, ifaceSubnetText(d), len(d.Peers), alive, orDash(d.ListenPort),
			human(rx), human(tx))
		if undecl > 0 {
			fmt.Fprintf(&b, " · ⚠ %d undeclared", undecl)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func hostLabel(h string) string {
	if h == "" {
		return "local"
	}
	return h
}
