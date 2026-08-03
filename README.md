<div align="center">

<img src="assets/wgxplore.svg" width="96" alt="wgxplore icon"/>

# wgxplore

**The management layer WireGuard never shipped — in the open, not on top.**

*Every device, every peer, every network — declared in a file, enforced by the kernel, visible at a keypress.*

[![License: BSD-3](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)
![Platform](https://img.shields.io/badge/platform-Linux-brightgreen.svg)
![Built with Go](https://img.shields.io/badge/built%20with-Go-00ADD8.svg)
![WireGuard](https://img.shields.io/badge/datapath-kernel%20WireGuard-88171a.svg)

<img src="docs/screenshots/console-dark.png" width="880" alt="wgxplore console — a live estate: interface cards on top, five hosts with their WireGuard interfaces and peers in the tree, a peer dossier with its verdict on the right"/>

</div>

**What you're looking at** — one hypervisor, photographed from the kernel,
minutes after installing itself:

- **A Kubernetes cluster on an encrypted backplane.** `kldload-cp` and
  workers `w-1/w-2/w-3` are KVM VMs, each on *two* WireGuard planes —
  `wg-k8s` (`10.251.0.0/24`, the data plane the CNI rides) and `wg-mgmt`
  (`10.250.0.0/24`: SSH, kubelet↔API, etcd). Every card top-right shows
  its port and live rx/tx; **40 peers, 40 alive** in the header means the
  full node mesh is handshaking. All k8s traffic between these nodes is
  kernel-encrypted *underneath* the cluster — the CNI never knows.
- **VMs in every state of life.** Two freshly-built template VMs
  (`klab-golden-centos`, `-rocky`) show dim — reachable, *no WireGuard
  yet*, honestly inventoried rather than hidden. One VM mid-provision
  shows red — `unreachable`, at the bottom, because down is information.
- **The dossier explains itself.** A selected peer shows its key,
  endpoint, allowed-ips (the kernel-enforced answer to "what may this key
  reach"), live traffic, and a verdict: this interface is *untracked* —
  declare it, and any peer nobody added starts rendering in alarm pink.
- Hosts are sorted and named by identity, with the ssh target demoted to
  a detail; the console found all of this by reading `wg show` over plain
  ssh — the machines run no agent.

`wgxplore` is a fast, keyboard-driven console for the WireGuard you already
run. Declare networks in a file, attach anything to them — hosts, VMs,
**running containers** — and see your whole estate in one tree: every
machine, every peer, handshake-fresh or stale, declared or **not declared
by anyone**.

Three ideas, no moving parts:

1. **The file is the network.** A network is one JSON file: subnet,
   topology, members. Rendering it produces plain WireGuard configs;
   applying them is plain `wg`/`ip`, echoed before it runs. No daemon, no
   server, no agent — what wgxplore leaves behind is stock WireGuard you
   could have typed yourself.
2. **The kernel is the map.** The estate view is `wg show` read back over
   the ssh you already have — local plus every host in your inventory, in
   parallel. The console can't drift from reality because it *is* a fresh
   read of reality.
3. **The diff is the alarm.** Live state is checked against the
   declarations. A peer on a managed interface that no file lists renders
   **UNDECLARED** in alarm pink — a WireGuard peer cannot appear by
   accident; someone with root added that key. `wgx estate | grep
   UNDECLARED` is a real, cron-able control.

Sister project to [zxplore](https://github.com/zxplore/zxplore) (same
console, one domain over: ZFS) — and when both are present, wgxplore is
the road zxplore's replication travels: kernel-encrypted, to overlay
addresses that never change.

## Sixty seconds, whole network

```
# declare a lab: one reachable hub, spokes join from anywhere
wgx net create lab --subnet 10.99.0.0/24 --topology hub-spoke
wgx net add lab hub --hub --endpoint hub.example.net:51820
wgx net add lab ct1
wgx net render lab                # one plain .conf per member
wgx net up lab hub                # echoes: ip link add · wg setconf · ip addr · ip link set up

# give a RUNNING container exactly one network: yours
podman run -d --network none --name job untrusted-image
wgx attach lab job ct1            # moves a wg interface INTO its netns

# see everything
wgx                               # the console (GUI, or TUI over ssh)
wgx estate                        # the same tree as text, for scripts and cron
```

The attach is the kernel trick worth knowing: a WireGuard interface's
encrypted socket stays anchored in the namespace where it was *created*,
so wgxplore configures it on the host, then moves it into the container —
no restart, no sidecar, no privileges. With `--network none`, the mesh is
the only network the workload has; spoke-to-spoke traffic dies in the
kernel because no key routes there.

More flows — the cross-site backplane, the zero-surface backup box, the
tripwire, joining a phone with its stock app — in
[docs/EXAMPLES.md](docs/EXAMPLES.md).

## Works with any WireGuard device

Interop runs both directions, because there's no agent and no wire
protocol of wgxplore's own:

- **It observes estates it didn't create.** If `ssh <host> wg show all
  dump` works — Linux, BSD, OpenWrt, a hand-rolled `wg0` from 2019 — it's
  in the tree. Hosts with no WireGuard at all still show, dim and honest.
- **Its networks accept any WireGuard implementation.** Rendered configs
  are standard `[Interface]`/`[Peer]`; a phone, MikroTik, or OPNsense box
  joins with its native tooling.

And all of it runs **underneath whatever networking you already have** —
declared subnets ride beside your LAN, beneath your apps and your CNI,
asking nothing else to change. New to WireGuard itself? The five-minute
primer: [docs/WIREGUARD.md](docs/WIREGUARD.md).

## Versus the alternatives

Everyone in this table moves packets as real WireGuard. The differences
are what's wrapped around the tunnel:

| | coordination plane | datapath | container attach | policy | plain-wg peers |
|---|---|---|---|---|---|
| Tailscale | SaaS + DERP relays | userspace | sidecar | ACL service | no |
| NetBird | own server | kernel | sidecar | server-side | partially |
| innernet / dsnet | own server | kernel | — | server-side | partially |
| wesher | gossip daemon per node | kernel | — | — | no |
| **wgxplore** | **none — a file** | **kernel** | **netns move** | **allowed-ips (kernel)** | **yes** |

The one-line map: wgxplore **competes** with the config managers,
**completes** everything that already speaks WireGuard, and **coexists**
with the SaaS meshes. The trade we accept: no CGNAT hole-punching, no
relays — one reachable UDP port runs an estate. The long-form boundaries,
strengths, weaknesses, and the workloads-as-datasets stack thesis:
[docs/POSITIONING.md](docs/POSITIONING.md).

## Scope: one thing, done well

wgxplore does **declared WireGuard, reconciled** — and will never grow a
daemon, database, broker, relay, or CNI. Adjacent wants are composition:
time-boxed access is `cron` + re-render, membership history is `git` on
`/etc/wgx`, alerting is the estate output in the monitor you already run.

Roadmap (only what sharpens the core): per-member key custody · fleet-grade
verbs (deterministic allocation, idempotent `net add`, `--json`) ·
`net adopt` · key rotation · audit log · joined networks.

## wgxplore on kldload

[kldload](https://kldload.com) is the first-party distribution — the
substrate installer bakes wgxplore into every profile: static console on
every install, GUI + desktop tile where there's a screen, a polkit rule
for prompt-free read-only refreshes, the ingested commit recorded in
`/etc/kldload/wgxplore-commit`, and air-gapped builds from a source cache.
A consumer, not a dependency: wgxplore runs on any Linux with kernel
WireGuard.

## Install

```
git clone https://github.com/wgxplore/wgxplore
cd wgxplore
make               # ./wgx (GUI+TUI, cgo/Fyne) and ./wgx-tui (static)
sudo make install
```

| binary | what | needs |
|---|---|---|
| `wgx` | native GUI (light/dark, follows the WM) + TUI + all verbs | cgo, OpenGL, X11/Wayland |
| `wgx-tui` | terminal-only, **fully static** | nothing — `scp` it anywhere |

Or `go install github.com/wgxplore/wgxplore@latest` for the static TUI.

<details>
<summary><b>GUI build dependencies per distro</b></summary>

```
# Fedora / RHEL / Rocky
sudo dnf install -y golang gcc pkgconf-pkg-config mesa-libGL-devel \
  libX11-devel libXcursor-devel libXrandr-devel libXinerama-devel \
  libXi-devel libXxf86vm-devel wayland-devel libxkbcommon-devel

# Debian / Ubuntu
sudo apt-get install -y golang gcc pkg-config libgl1-mesa-dev xorg-dev \
  libwayland-dev libxkbcommon-dev

# Arch
sudo pacman -S --needed go gcc pkgconf libgl libxcursor libxrandr \
  libxinerama libxi wayland libxkbcommon
```
</details>

Runtime: a kernel with WireGuard (mainline since 5.6) and `wg` on machines
you *mutate*; remote estate hosts need only `sshd` + `wg`. Remote hosts
come from `~/.ssh/config` or `~/.config/wgx/hosts` (one ssh target per
line), re-read on every refresh.

## Usage

```
wgx                # the console — native GUI, falls back to the TUI
wgx tui            # force the terminal console
wgx estate         # the whole estate as a text tree
wgx show           # peer dossiers, one-shot
wgx net create <name> [--subnet CIDR] [--topology mesh|hub-spoke] [--port N]
wgx net add <name> <member> [--endpoint host:port] [--hub]
wgx net render <name>
wgx net up <name> <member>
wgx attach <net> <container> <member>
```

**TUI keys:** `j`/`k` move · `/` filter · `r` refresh · `q` quit.
**GUI:** interface cards on top, estate tree left, dossier right;
auto-rescans every 30s; follows the OS light/dark theme live.

<div align="center">
<img src="docs/screenshots/peers-dark.png" width="880" alt="wgxplore — drilled into an interface: every peer with handshake age, and a full dossier for the selected one"/>
<br/><sub>Drilled in: every peer on the management plane, handshake-fresh, one selected — key, endpoint, allowed-ips, traffic, verdict.</sub>
</div>

<!-- TUI screenshot slot
<img src="docs/screenshots/tui.png" width="880" alt="wgxplore TUI over ssh"/>
-->

## Security model

- **The console is read-only**; every mutation is an explicit CLI verb
  that echoes its exact `wg`/`ip` command first.
- **Policy lives in the kernel** (cryptokey routing) — no policy service
  to misconfigure or compromise; a scanner can't even see the port.
- **Reads escalate gently** — direct → `sudo -n` → `pkexec`; ssh host
  keys are pinned (`accept-new`); declarations are `0600`.
- **Prototype caveat, stated plainly:** member private keys currently
  live in the declaration file — treat `/etc/wgx` like a keystore.
  Per-member custody is the top of the roadmap.

## Documentation

- [docs/EXAMPLES.md](docs/EXAMPLES.md) — five worked flows, commands included.
- [docs/POSITIONING.md](docs/POSITIONING.md) — boundaries, trades, and the stack thesis.
- [docs/WIREGUARD.md](docs/WIREGUARD.md) — the protocol from first principles.

## Status

0.1.0 — engine, estate inventory, reconciliation, TUI, themed GUI.
BSD-3-Clause. See [LICENSE](LICENSE).
