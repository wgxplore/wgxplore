//go:build gui

// gui.go — the native wgxplore window (Fyne), sibling to zxplore's GUI.
//
// A themed console, not a stock-widget form: dark slate chassis, the brand
// teal/amber of the wgxplore mark, and colour used for SEPARATION — health
// states (alive/quiet/stale), the undeclared-peer alarm, and the summary
// chips each own a colour so the eye finds trouble before reading a word.
// Layout is the same two-pane shape as the TUI and as zxplore: estate tree
// on the left, full dossier on the right, interface cards across the top.
//
// Read-only. Mutations stay behind the engine's explicit verbs so every
// change is a visible wg/ip command.
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
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ─── palette ─────────────────────────────────────────────────────────────
// One source of truth, shared by the theme and every hand-drawn element.
// Brand colours match the wgxplore mark (teal glyph, amber hub) and the
// health colours match the TUI so the two consoles read as one product.
var (
	palBg     = color.NRGBA{0x10, 0x16, 0x1d, 0xff} // window
	palPanel  = color.NRGBA{0x17, 0x1f, 0x28, 0xff} // cards, inputs, buttons
	palRaised = color.NRGBA{0x1d, 0x27, 0x31, 0xff} // hovered / chip face
	palLine   = color.NRGBA{0x24, 0x30, 0x3b, 0xff} // separators, borders
	palFg     = color.NRGBA{0xdc, 0xe6, 0xee, 0xff} // primary text
	palDim    = color.NRGBA{0x7d, 0x8d, 0x9b, 0xff} // secondary text
	palTeal   = color.NRGBA{0x49, 0xc7, 0xc0, 0xff} // brand / focus / links
	palAmber  = color.NRGBA{0xf0, 0xc6, 0x74, 0xff} // hub accent / warnings
	palAlive  = color.NRGBA{0x4c, 0xb9, 0x8a, 0xff} // handshake < 3m
	palQuiet  = color.NRGBA{0xe6, 0xa5, 0x5f, 0xff} // handshake < 30m
	palStale  = color.NRGBA{0xdc, 0x48, 0x48, 0xff} // stale / unreachable
	palAlarm  = color.NRGBA{0xff, 0x5c, 0xd6, 0xff} // UNDECLARED — the alarm
	palSelect = color.NRGBA{0x1b, 0x3a, 0x3e, 0xff} // selection wash (teal-dark)
)

func healthColor(h string) color.NRGBA {
	switch h {
	case "alive":
		return palAlive
	case "quiet":
		return palQuiet
	}
	return palStale
}

// ─── theme ───────────────────────────────────────────────────────────────
// wgxTheme forces the console's own dark palette regardless of the desktop
// variant — the estate view is a night console, and health colours are
// calibrated against this background. Fonts/icons/sizes delegate to the
// default theme so text metrics stay platform-correct.
type wgxTheme struct{}

func (wgxTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return palBg
	case theme.ColorNameForeground:
		return palFg
	case theme.ColorNamePrimary, theme.ColorNameFocus, theme.ColorNameHyperlink:
		return palTeal
	case theme.ColorNameButton, theme.ColorNameInputBackground:
		return palPanel
	case theme.ColorNameHover:
		// Hover/pressed are OVERLAYS Fyne paints on top of the widget —
		// they must be translucent. An opaque hover here blacked out the
		// teal Refresh button on mouse-over (seen .119 2026-08-03).
		return color.NRGBA{0xff, 0xff, 0xff, 0x14}
	case theme.ColorNamePressed:
		return color.NRGBA{0xff, 0xff, 0xff, 0x28}
	case theme.ColorNameSelection:
		return palSelect
	case theme.ColorNameSeparator, theme.ColorNameInputBorder:
		return palLine
	case theme.ColorNamePlaceHolder, theme.ColorNameDisabled:
		return palDim
	case theme.ColorNameScrollBar:
		return palLine
	case theme.ColorNameSuccess:
		return palAlive
	case theme.ColorNameWarning:
		return palQuiet
	case theme.ColorNameError:
		return palStale
	case theme.ColorNameHeaderBackground, theme.ColorNameMenuBackground,
		theme.ColorNameOverlayBackground:
		return palPanel
	case theme.ColorNameForegroundOnPrimary:
		return palBg
	case theme.ColorNameShadow:
		return color.NRGBA{0, 0, 0, 0x66}
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

func (wgxTheme) Font(s fyne.TextStyle) fyne.Resource { return theme.DefaultTheme().Font(s) }
func (wgxTheme) Icon(n fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(n)
}
func (wgxTheme) Size(n fyne.ThemeSizeName) float32 { return theme.DefaultTheme().Size(n) }

// ─── small drawing helpers ───────────────────────────────────────────────

func txt(s string, c color.NRGBA, size float32, style fyne.TextStyle) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle = style
	return t
}

// dot renders a fixed-size status circle — the smallest possible unit of
// colour separation; the tree and the cards both lead with one.
func dot(c color.NRGBA) fyne.CanvasObject {
	ci := canvas.NewCircle(c)
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), ci))
}

// card wraps content on the raised panel colour with rounded corners.
func card(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(palPanel)
	bg.CornerRadius = 8
	return container.NewStack(bg, container.NewPadded(content))
}

// chip is a compact "value + label" summary tile for the header row.
func chip(value, label string, c color.NRGBA) fyne.CanvasObject {
	bg := canvas.NewRectangle(palRaised)
	bg.CornerRadius = 8
	return container.NewStack(bg, container.NewPadded(container.NewHBox(
		txt(value, c, 14, fyne.TextStyle{Bold: true}),
		txt(label, palDim, 12, fyne.TextStyle{}),
	)))
}

func kv(rows ...[2]string) fyne.CanvasObject {
	var objs []fyne.CanvasObject
	for _, r := range rows {
		objs = append(objs,
			txt(r[0], palDim, 13, fyne.TextStyle{}),
			txt(r[1], palFg, 13, fyne.TextStyle{Monospace: true}))
	}
	return container.New(layout.NewFormLayout(), objs...)
}

type nodeKind int

const (
	nHost nodeKind = iota
	nIface
	nPeer
)

// ─── the window ──────────────────────────────────────────────────────────

// RunGUI opens the console: summary chips and one card per interface across
// the top, the expandable estate tree on the left (host → interface → peer),
// a full dossier for the selection on the right.
func RunGUI() error {
	a := app.NewWithID("ca.wgxplore")
	a.Settings().SetTheme(wgxTheme{})
	a.SetIcon(theme.ComputerIcon())
	w := a.NewWindow("wgxplore")
	w.Resize(fyne.NewSize(1180, 740))

	var (
		devs  []Device
		hosts = sshHosts()
		// header: brand + build left, summary chips right — rebuilt per load
		chips   = container.NewHBox()
		status  = txt("scanning estate…", palDim, 13, fyne.TextStyle{Italic: true})
		cards   = container.NewVBox() // one card per interface
		dossier = container.NewVBox() // right pane, rebuilt on select
		kids    = map[string][]string{}
		kind    = map[string]nodeKind{}
		ref     = map[string][2]int{} // uid → {device idx, peer idx(-1)}
	)

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

	// ── tree: every row is dot + name + detail + alarm badge ─────────────
	tree := widget.NewTree(
		func(uid widget.TreeNodeID) []widget.TreeNodeID { return kids[uid] },
		func(uid widget.TreeNodeID) bool { return len(kids[uid]) > 0 },
		func(branch bool) fyne.CanvasObject {
			return container.NewHBox(
				dot(palDim),
				txt("node", palFg, 13, fyne.TextStyle{}),
				txt("", palDim, 12, fyne.TextStyle{}),
				txt("", palAlarm, 12, fyne.TextStyle{Bold: true}),
			)
		},
		func(uid widget.TreeNodeID, branch bool, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			mark := box.Objects[0].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*canvas.Circle)
			name := box.Objects[1].(*canvas.Text)
			det := box.Objects[2].(*canvas.Text)
			badge := box.Objects[3].(*canvas.Text)
			badge.Text = ""
			r, ok := ref[uid]
			if !ok {
				name.Text = uid
				name.Refresh()
				return
			}
			d := devs[r[0]]
			switch kind[uid] {
			case nHost:
				h := d.Host
				if h == "" {
					h = "local"
				}
				name.Text = h
				name.Color = palTeal
				name.TextStyle = fyne.TextStyle{Bold: true}
				if d.Err != "" {
					mark.FillColor = palStale
					det.Text = "unreachable"
					det.Color = palStale
				} else {
					mark.FillColor = palTeal
					det.Text = hostIdentity(d, h)
					det.Color = palDim
				}
			case nIface:
				name.Text = d.Name
				name.Color = palFg
				name.TextStyle = fyne.TextStyle{Bold: true}
				mark.FillColor = palAmber
				det.Text = fmt.Sprintf("%s · %d peers", ifaceAddr(d), len(d.Peers))
				det.Color = palDim
			case nPeer:
				p := d.Peers[r[1]]
				mark.FillColor = healthColor(p.Health())
				key := p.PublicKey
				if len(key) > 12 {
					key = key[:12] + "…"
				}
				if p.Label != "" {
					key = p.Label // declared peers show as network/member
				}
				name.Text = key
				name.Color = palFg
				name.TextStyle = fyne.TextStyle{Monospace: true}
				det.Text = firstIP(p.AllowedIPs) + " · " + p.Age()
				det.Color = palDim
				if !p.Declared && d.Managed {
					badge.Text = "⚠"
				}
			}
			mark.Refresh()
			name.Refresh()
			det.Refresh()
			badge.Refresh()
		},
	)

	setDossier := func(objs ...fyne.CanvasObject) {
		dossier.Objects = objs
		dossier.Refresh()
	}

	tree.OnSelected = func(uid widget.TreeNodeID) {
		r, ok := ref[uid]
		if !ok {
			return
		}
		d := devs[r[0]]
		switch kind[uid] {
		case nPeer:
			setDossier(guiPeerDossier(d, d.Peers[r[1]])...)
		case nHost:
			setDossier(guiHostDossier(devs, d.Host)...)
		default:
			setDossier(guiDeviceDossier(d)...)
		}
	}

	// ── interface cards — the zpool-bar analogue, one card per interface ─
	rebuildCards := func() {
		cards.Objects = nil
		for _, d := range devs {
			if d.Err != "" {
				cards.Objects = append(cards.Objects, card(container.NewHBox(
					dot(palStale),
					txt(hostLabel(d.Host), palFg, 13, fyne.TextStyle{Bold: true}),
					txt("unreachable: "+d.Err, palStale, 12, fyne.TextStyle{}),
				)))
				continue
			}
			var alive, undecl int
			var rx, tx int64
			for _, p := range d.Peers {
				if p.Health() == "alive" {
					alive++
				}
				if d.Managed && !p.Declared {
					undecl++
				}
				rx += p.RxBytes
				tx += p.TxBytes
			}
			mark := palAlive
			if len(d.Peers) > 0 && alive == 0 {
				mark = palQuiet
			}
			name := d.Name
			if d.Host != "" {
				name = d.Host + ":" + d.Name
			}
			aliveCol := palAlive
			if alive == 0 {
				aliveCol = palDim
			}
			row := container.NewHBox(
				dot(mark),
				txt(name, palFg, 13, fyne.TextStyle{Bold: true}),
				txt(ifaceAddr(d), palTeal, 12, fyne.TextStyle{Monospace: true}),
				layout.NewSpacer(),
				txt(fmt.Sprintf("%d peers", len(d.Peers)), palDim, 12, fyne.TextStyle{}),
				txt(fmt.Sprintf("%d alive", alive), aliveCol, 12, fyne.TextStyle{Bold: true}),
				txt("port "+orDash(d.ListenPort), palDim, 12, fyne.TextStyle{Monospace: true}),
				txt("rx "+human(rx)+" · tx "+human(tx), palDim, 12, fyne.TextStyle{Monospace: true}),
			)
			if undecl > 0 {
				row.Add(txt(fmt.Sprintf("⚠ %d undeclared", undecl), palAlarm, 12,
					fyne.TextStyle{Bold: true}))
			}
			cards.Objects = append(cards.Objects, card(row))
		}
		if len(cards.Objects) == 0 {
			cards.Objects = []fyne.CanvasObject{card(
				txt("no WireGuard interfaces found", palDim, 13, fyne.TextStyle{Italic: true}))}
		}
		cards.Refresh()
	}

	rebuildChips := func() {
		h, i, p, alive, undeclared := Totals(devs)
		chips.Objects = []fyne.CanvasObject{
			chip(fmt.Sprint(h), "hosts", palFg),
			chip(fmt.Sprint(i), "interfaces", palFg),
			chip(fmt.Sprint(p), "peers", palFg),
			chip(fmt.Sprint(alive), "alive", palAlive),
		}
		if undeclared > 0 {
			chips.Objects = append(chips.Objects,
				chip(fmt.Sprint(undeclared), "UNDECLARED", palAlarm))
		}
		chips.Refresh()
	}

	reload := func() {
		status.Text = "scanning estate…"
		status.Refresh()
		go func() {
			d := CollectEstate(hosts)
			MarkDeclared(d)
			fyne.Do(func() {
				devs = d
				reindex()
				rebuildChips()
				rebuildCards()
				status.Text = fmt.Sprintf("scanned %s", time.Now().Format("15:04:05"))
				status.Refresh()
				tree.Refresh()
				for _, hu := range kids[""] {
					tree.OpenBranch(hu) // hosts open; interfaces expand on click
				}
			})
		}()
	}

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), reload)
	refresh.Importance = widget.HighImportance
	expand := widget.NewButtonWithIcon("Expand all", theme.MenuDropDownIcon(), func() {
		for uid := range kids {
			tree.OpenBranch(uid)
		}
	})

	brand := container.NewHBox(
		txt("wgxplore", palTeal, 20, fyne.TextStyle{Bold: true}),
		txt(versionFull(), palDim, 12, fyne.TextStyle{Monospace: true}),
		txt("·", palLine, 12, fyne.TextStyle{}),
		status,
	)
	head := container.NewBorder(nil, nil, brand,
		container.NewHBox(chips, expand, refresh))
	top := container.NewVBox(container.NewPadded(head), cards, widget.NewSeparator())

	split := container.NewHSplit(tree,
		container.NewVScroll(container.NewPadded(dossier)))
	split.Offset = 0.42
	w.SetContent(container.NewBorder(top, nil, nil, nil, split))

	setDossier(
		txt("estate", palTeal, 16, fyne.TextStyle{Bold: true}),
		txt("select a host, interface or peer", palDim, 13, fyne.TextStyle{Italic: true}),
	)
	reload()
	go func() {
		for range time.Tick(30 * time.Second) {
			fyne.Do(reload)
		}
	}()

	w.ShowAndRun()
	return nil
}

// ─── dossiers — structured panels, not markdown ──────────────────────────
// Each dossier is a title, a key/value block, and a coloured verdict card:
// green wash = declared/healthy, pink wash = the undeclared alarm, red =
// unreachable. The verdict carries the "so what", the kv block the facts.

func verdict(c color.NRGBA, lines ...string) fyne.CanvasObject {
	bg := canvas.NewRectangle(color.NRGBA{c.R, c.G, c.B, 0x22})
	bg.CornerRadius = 8
	var objs []fyne.CanvasObject
	for i, l := range lines {
		st := fyne.TextStyle{}
		col := palFg
		if i == 0 {
			st = fyne.TextStyle{Bold: true}
			col = c
		}
		objs = append(objs, txt(l, col, 13, st))
	}
	return container.NewStack(bg, container.NewPadded(container.NewVBox(objs...)))
}

func guiHostDossier(devs []Device, host string) []fyne.CanvasObject {
	var ifaces, peers, alive, undecl int
	for _, d := range devs {
		if d.Host != host {
			continue
		}
		if d.Err != "" {
			return []fyne.CanvasObject{
				txt("host "+hostLabel(host), palTeal, 16, fyne.TextStyle{Bold: true}),
				verdict(palStale, "UNREACHABLE", d.Err),
			}
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
	rows := [][2]string{}
	for _, d := range devs {
		if d.Host == host && d.HostFQDN != "" {
			rows = append(rows, [2]string{"fqdn", d.HostFQDN})
			if d.HostAddr != "" {
				rows = append(rows, [2]string{"reached at", d.HostAddr})
			}
			break
		}
	}
	rows = append(rows,
		[2]string{"interfaces", fmt.Sprint(ifaces)},
		[2]string{"peers", fmt.Sprint(peers)},
		[2]string{"handshaking", fmt.Sprint(alive)},
	)
	objs := []fyne.CanvasObject{
		txt("host "+hostLabel(host), palTeal, 16, fyne.TextStyle{Bold: true}),
		kv(rows...),
	}
	if undecl > 0 {
		objs = append(objs, verdict(palAlarm,
			fmt.Sprintf("⚠ %d peer(s) not in any declaration", undecl),
			"Someone with root added them. Investigate."))
	}
	return objs
}

func guiDeviceDossier(d Device) []fyne.CanvasObject {
	if d.Err != "" {
		return []fyne.CanvasObject{
			txt(hostLabel(d.Host), palTeal, 16, fyne.TextStyle{Bold: true}),
			verdict(palStale, "UNREACHABLE", d.Err),
		}
	}
	var alive, undeclared int
	for _, p := range d.Peers {
		if p.Health() == "alive" {
			alive++
		}
		if d.Managed && !p.Declared {
			undeclared++
		}
	}
	objs := []fyne.CanvasObject{
		txt("interface "+d.Name, palTeal, 16, fyne.TextStyle{Bold: true}),
		kv(
			[2]string{"host", hostLabel(d.Host)},
			[2]string{"address", orDash(d.Addr)},
			[2]string{"public key", d.PublicKey},
			[2]string{"listen port", orDash(d.ListenPort)},
			[2]string{"peers", fmt.Sprintf("%d (%d handshaking)", len(d.Peers), alive)},
			[2]string{"routes", ifaceSubnet(d)},
		),
	}
	switch {
	case !d.Managed:
		objs = append(objs, verdict(palDim,
			"untracked — not managed by any wgxplore declaration",
			"Peers here are shown for inventory, not judged."))
	case undeclared > 0:
		objs = append(objs, verdict(palAlarm,
			fmt.Sprintf("⚠ %d peer(s) not in any declaration", undeclared),
			"Someone with root added them. Investigate."))
	case len(d.Peers) > 0:
		objs = append(objs, verdict(palAlive, "✓ all peers declared"))
	}
	return objs
}

func guiPeerDossier(d Device, p Peer) []fyne.CanvasObject {
	h := p.Health()
	objs := []fyne.CanvasObject{
		container.NewHBox(
			dot(healthColor(h)),
			txt("peer", palTeal, 16, fyne.TextStyle{Bold: true}),
			txt(h+" · "+p.Age(), healthColor(h), 13, fyne.TextStyle{Bold: true}),
		),
		kv(func() [][2]string {
			rows := [][2]string{}
			if p.Label != "" {
				rows = append(rows, [2]string{"declared as", p.Label})
			}
			return append(rows,
				[2]string{"public key", p.PublicKey},
				[2]string{"on", hostLabel(d.Host) + " · " + d.Name},
				[2]string{"endpoint", orDash(p.Endpoint)},
				[2]string{"allowed-ips", p.AllowedIPs},
				[2]string{"traffic", "rx " + human(p.RxBytes) + " · tx " + human(p.TxBytes)},
				[2]string{"keepalive", orDash(p.Keepalive)},
			)
		}()...),
	}
	switch {
	case p.Declared:
		objs = append(objs, verdict(palAlive,
			"✓ declared — this peer is in a wgxplore network"))
	case !d.Managed:
		objs = append(objs, verdict(palDim,
			"untracked — "+d.Name+" is not managed by any declaration",
			"Declare this network to arm the undeclared-peer alarm."))
	default:
		objs = append(objs, verdict(palAlarm,
			"⚠ UNDECLARED — no declaration lists this key",
			"A peer cannot appear by accident: cryptokey routing",
			"means someone with root on "+d.Name+" added it. Investigate."))
	}
	return objs
}

// hostIdentity renders "fqdn · underlay-ip" for a host row, omitting parts
// that are unknown or redundant with the alias itself.
func hostIdentity(d Device, alias string) string {
	var parts []string
	if d.HostFQDN != "" && d.HostFQDN != alias {
		parts = append(parts, d.HostFQDN)
	}
	if d.HostAddr != "" {
		parts = append(parts, d.HostAddr)
	}
	return strings.Join(parts, " · ")
}

// ifaceAddr prefers the interface's OWN address (what you point things at);
// hosts we could not read addresses from fall back to the subnet guess.
func ifaceAddr(d Device) string {
	if d.Addr != "" {
		return d.Addr
	}
	return ifaceSubnet(d)
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
