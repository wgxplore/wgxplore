//go:build gui

// gui.go — the native wgxplore window (Fyne), sibling to zxplore's GUI.
//
// Same two-pane shape as the TUI and as zxplore: the estate on the left, a
// full dossier on the right. Read-only; mutations stay behind the engine's
// explicit verbs so every change is a visible wg/ip command.
//
// Window title is EXACTLY the app name: GLFW derives the X11 WM_CLASS from
// the title at window creation, and the shell maps WM_CLASS → .desktop for
// the dock icon. zxplore shipped a pretty title once and every desktop
// showed a generic fallback icon (fixed there 2026-08-02, e6c6e0f); do not
// repeat it here. The launcher's GenericName carries the descriptive text.
package main

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var (
	colAlive = color.NRGBA{0x4c, 0xb9, 0x8a, 0xff}
	colQuiet = color.NRGBA{0xe6, 0xa5, 0x5f, 0xff}
	colStale = color.NRGBA{0xdc, 0x48, 0x48, 0xff}
	colAlarm = color.NRGBA{0xff, 0x5c, 0xd6, 0xff}
)

type nodeKind int

const (
	nHost nodeKind = iota
	nIface
	nPeer
)

// RunGUI opens the console window: an expandable estate tree on the left
// (host → interface → peer, the way an explorer trees folders), a full
// dossier on the right.
func RunGUI() error {
	a := app.NewWithID("ca.wgxplore")
	a.SetIcon(theme.ComputerIcon())
	w := a.NewWindow("wgxplore")
	_ = versionFull() // build stamp shown in the summary line below
	w.Resize(fyne.NewSize(1150, 720))

	var (
		devs    []Device
		hosts   = sshHosts()
		summary = widget.NewLabel("scanning estate…")
		// Devices bar — the analogue of zxplore's zpool overview: one line
		// per WireGuard interface so the top of the window answers "what
		// have I got, is it healthy" before you touch the tree.
		devices = widget.NewLabel("… scanning interfaces")
		dossier = widget.NewRichTextFromMarkdown("")
		// tree index, rebuilt on every load
		kids = map[string][]string{}
		kind = map[string]nodeKind{}
		ref  = map[string][2]int{} // uid → {device idx, peer idx(-1)}
	)
	dossier.Wrapping = fyne.TextWrapWord
	devices.TextStyle = fyne.TextStyle{Monospace: true}

	reindex := func() {
		kids = map[string][]string{}
		kind = map[string]nodeKind{}
		ref = map[string][2]int{}
		hostSeen := map[string]bool{}
		for di, d := range devs {
			h := d.Host
			if h == "" {
				h = "local"
			}
			hu := "h:" + h
			if !hostSeen[h] {
				hostSeen[h] = true
				kids[""] = append(kids[""], hu)
				kind[hu] = nHost
				ref[hu] = [2]int{di, -1}
			}
			if d.Err != "" {
				continue
			}
			iu := fmt.Sprintf("i:%d", di)
			kids[hu] = append(kids[hu], iu)
			kind[iu] = nIface
			ref[iu] = [2]int{di, -1}
			for pi := range d.Peers {
				pu := fmt.Sprintf("p:%d:%d", di, pi)
				kids[iu] = append(kids[iu], pu)
				kind[pu] = nPeer
				ref[pu] = [2]int{di, pi}
			}
		}
	}

	tree := widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID { return kids[uid] },
		func(uid widget.TreeNodeID) bool { return len(kids[uid]) > 0 },
		func(branch bool) fyne.CanvasObject { return widget.NewLabel("node") },
		func(uid widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
			lbl := o.(*widget.Label)
			r, ok := ref[uid]
			if !ok {
				lbl.SetText(uid)
				return
			}
			d := devs[r[0]]
			switch kind[uid] {
			case nHost:
				h := d.Host
				if h == "" {
					h = "local"
				}
				lbl.TextStyle = fyne.TextStyle{Bold: true}
				if d.Err != "" {
					lbl.SetText("🖧 " + h + "  — unreachable")
				} else {
					lbl.SetText("🖧 " + h)
				}
			case nIface:
				lbl.TextStyle = fyne.TextStyle{Bold: false}
				lbl.SetText(fmt.Sprintf("⇄ %s   %s   (%d peers)",
					d.Name, ifaceSubnet(d), len(d.Peers)))
			case nPeer:
				p := d.Peers[r[1]]
				dot := map[string]string{"alive": "●", "quiet": "●", "stale": "○", "never": "○"}[p.Health()]
				flag := ""
				if !p.Declared {
					flag = "  ⚠"
				}
				lbl.TextStyle = fyne.TextStyle{Monospace: true}
				lbl.SetText(fmt.Sprintf("%s %-18s %-22s %s%s",
					dot, firstIP(p.AllowedIPs), orDash(p.Endpoint), p.Age(), flag))
			}
		},
	)

	tree.OnSelected = func(uid widget.TreeNodeID) {
		r, ok := ref[uid]
		if !ok {
			return
		}
		d := devs[r[0]]
		if kind[uid] == nPeer {
			dossier.ParseMarkdown(guiPeerDossier(d, d.Peers[r[1]]))
			return
		}
		if kind[uid] == nHost {
			dossier.ParseMarkdown(guiHostDossier(devs, d.Host))
			return
		}
		dossier.ParseMarkdown(guiDeviceDossier(d))
	}

	reload := func() {
		summary.SetText("scanning estate…")
		go func() {
			d := CollectEstate(hosts)
			MarkDeclared(d)
			fyne.Do(func() {
				devs = d
				reindex()
				h, i, p, al, un := Totals(devs)
				s := fmt.Sprintf("wgxplore %s   ·   %d hosts · %d interfaces · %d peers · %d alive",
					versionFull(), h, i, p, al)
				if un > 0 {
					s += fmt.Sprintf("   ⚠ %d UNDECLARED", un)
				}
				summary.SetText(s)
				devices.SetText(DevicesOverview(devs))
				tree.Refresh()
				for _, hu := range kids[""] {
					tree.OpenBranch(hu) // hosts open by default; interfaces expand on click
				}
			})
		}()
	}

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), reload)
	expand := widget.NewButtonWithIcon("Expand all", theme.MenuDropDownIcon(), func() {
		for uid := range kids {
			tree.OpenBranch(uid)
		}
	})
	head := container.NewBorder(nil, nil, nil, container.NewHBox(expand, refresh), summary)
	top := container.NewVBox(head, widget.NewSeparator(), devices, widget.NewSeparator())
	split := container.NewHSplit(tree, container.NewVScroll(dossier))
	split.Offset = 0.46
	w.SetContent(container.NewBorder(top, nil, nil, nil, split))

	reload()
	go func() {
		for range time.Tick(30 * time.Second) {
			reload()
		}
	}()

	w.ShowAndRun()
	return nil
}

// ifaceSubnet summarises what an interface routes, e.g. "10.250.0.0/24",
// so a branch reads like a network rather than a device name.
func ifaceSubnet(d Device) string {
	seen := map[string]bool{}
	var nets []string
	for _, p := range d.Peers {
		for _, cidr := range strings.Split(p.AllowedIPs, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			ip := strings.Split(cidr, "/")[0]
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
	if len(nets) > 2 {
		return strings.Join(nets[:2], " ") + fmt.Sprintf(" +%d", len(nets)-2)
	}
	return strings.Join(nets, " ")
}

func firstIP(allowed string) string {
	f := strings.Split(allowed, ",")[0]
	return strings.TrimSpace(f)
}

// guiHostDossier rolls every interface on one host into a summary.
func guiHostDossier(devs []Device, host string) string {
	label := host
	if label == "" {
		label = "local"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## host %s\n\n", label)
	var ifaces, peers, alive, undecl int
	for _, d := range devs {
		if d.Host != host {
			continue
		}
		if d.Err != "" {
			fmt.Fprintf(&b, "**UNREACHABLE**\n\n```\n%s\n```\n", d.Err)
			return b.String()
		}
		ifaces++
		for _, p := range d.Peers {
			peers++
			if p.Health() == "alive" {
				alive++
			}
			if !p.Declared {
				undecl++
			}
		}
	}
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| interfaces | %d |\n", ifaces)
	fmt.Fprintf(&b, "| peers | %d |\n", peers)
	fmt.Fprintf(&b, "| handshaking | %d |\n", alive)
	if undecl > 0 {
		fmt.Fprintf(&b, "\n**⚠ %d peer(s) not in any declaration.**\n", undecl)
	}
	return b.String()
}

func guiDeviceDossier(d Device) string {
	host := d.Host
	if host == "" {
		host = "local"
	}
	if d.Err != "" {
		return fmt.Sprintf("## %s\n\n**UNREACHABLE**\n\n```\n%s\n```\n", host, d.Err)
	}
	var alive, undeclared int
	for _, p := range d.Peers {
		if p.Health() == "alive" {
			alive++
		}
		if !p.Declared {
			undeclared++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## interface %s\n\n", d.Name)
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| host | %s |\n", host)
	fmt.Fprintf(&b, "| public key | `%s` |\n", d.PublicKey)
	fmt.Fprintf(&b, "| listen port | %s |\n", orDash(d.ListenPort))
	fmt.Fprintf(&b, "| peers | %d (%d handshaking) |\n", len(d.Peers), alive)
	if undeclared > 0 {
		fmt.Fprintf(&b, "\n**⚠ %d peer(s) are not in any declaration.**\n", undeclared)
	}
	return b.String()
}

func guiPeerDossier(d Device, p Peer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## peer\n\n")
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| public key | `%s` |\n", p.PublicKey)
	fmt.Fprintf(&b, "| on | %s |\n", d.Name)
	fmt.Fprintf(&b, "| endpoint | %s |\n", orDash(p.Endpoint))
	fmt.Fprintf(&b, "| allowed-ips | `%s` |\n", p.AllowedIPs)
	fmt.Fprintf(&b, "| handshake | **%s** (%s) |\n", p.Age(), p.Health())
	fmt.Fprintf(&b, "| traffic | rx %s / tx %s |\n", human(p.RxBytes), human(p.TxBytes))
	fmt.Fprintf(&b, "| keepalive | %s |\n", orDash(p.Keepalive))
	if p.Declared {
		fmt.Fprintf(&b, "\n✓ **Declared** — this peer belongs to a wgxplore network.\n")
	} else {
		fmt.Fprintf(&b, "\n⚠ **UNDECLARED** — no declaration lists this key.\n\n")
		fmt.Fprintf(&b, "A peer cannot appear by accident: cryptokey routing means "+
			"someone with root on `%s` added it. Investigate.\n", d.Name)
	}
	return b.String()
}
