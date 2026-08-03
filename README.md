# wgxplore

**The WireGuard networks console.** Sister project to
[zxplore](https://github.com/zxplore/zxplore): same console shape, same
primitives ethos, one domain over.

Declare networks, attach anything to them — hosts, **running containers via
the kernel's netns move**, VMs — and see the whole estate in one
keyboard-driven view: every device, every peer, handshake-fresh or stale,
declared or **not declared by anyone**.

The one-sentence version: wgxplore is a **declared, in-kernel networking
overlay you apply at runtime** — membership is a file, policy is cryptokey
routing, and the console is the kernel's own state read back and diffed
against the declarations.

## How it works

wgxplore adds **no datapath and no daemon**. The overlay *is* kernel
WireGuard; wgxplore is the declaration, application, and observation layer
on top of it. Three layers, smallest possible footprint each:

**1. Declare — the file is the API.**
A network is one JSON file in `/etc/wgx/<name>.json`: subnet, topology
(`mesh` or `hub-spoke`), port, members. `wgx net create` / `wgx net add`
write it; you can also just edit it. Nothing hides in a database or a
coordination server — `cat` shows you the entire network.

**2. Apply — render to plain WireGuard, run the primitives.**
`wgx net render` compiles the declaration into one standard `.conf` per
member. **Topology is nothing but the peer list**: in a mesh everyone gets
everyone as a `[Peer]`; in hub-spoke a spoke gets exactly one `[Peer]` (the
hub, with `AllowedIPs = <whole subnet>`) and the hub gets everyone. Policy
is therefore enforced by **cryptokey routing in the kernel** — a spoke
cannot address another spoke because no key routes there. There is no ACL
service to bypass, because there is no service.

`wgx net up` applies it with the same commands you would type by hand — and
echoes each one before running it:

    $ ip link add wgx-lab type wireguard
    $ wg setconf wgx-lab /etc/wgx/lab/host1.conf
    $ ip addr add 10.99.0.1/24 dev wgx-lab
    $ ip link set wgx-lab up

After wgxplore runs, what you have is stock WireGuard — indistinguishable
from a setup built by hand, administrable with `wg` alone, and not dependent
on wgxplore staying installed.

**3. Observe — the estate view.**
`wgx` / `wgx estate` runs `wg show all dump` locally and over plain ssh on
every host in your `~/.ssh/config` (or an explicit inventory file), in
parallel, and folds the results into one tree: host → interface → peer,
coloured by handshake age. Then it reconciles reality against the
declarations:

    declared + live    → healthy
    declared, absent   → never came up, or the host is down
    live, UNDECLARED   → nobody added this peer — alarm

That last row is the point. Cryptokey routing means **a peer cannot appear
by accident** — someone with root added a key. An undeclared peer on an
interface you manage is an intrusion signal, and wgxplore is built to make
it impossible to miss.

## Attaching a running container (the netns move)

    wgx attach lab <container> ct1

**What a netns is:** Linux can run many independent network stacks on one
kernel — each *network namespace* has its own interfaces, routing table,
firewall rules, and sockets. A container's "network isolation" is exactly
this: podman/docker put the container's processes in their own netns, so
they can only see the interfaces that exist inside it. `--network none`
means the netns contains nothing but loopback — the workload is networkless.

WireGuard interfaces have a property most tooling ignores: **the encrypted
UDP socket stays anchored in the namespace where the interface was
*created***, even after the interface itself is moved to another namespace.
wgxplore exploits exactly that:

1. create and configure `wgx-…` **in the host namespace** — the socket
   anchors here, where the real NIC and the route to the hub live;
2. `ip link set … netns <pid>` — move the interface into the running
   container's namespace;
3. address it and bring it up from inside via `nsenter`.

The container — no restart, no privileges, no sidecar process, no agent in
the image — now owns a mesh interface. Everything it sends into that
interface is encrypted *by the kernel* and leaves through the host-anchored
socket; plaintext never exists inside the container's namespace boundary,
and the container cannot reach the underlay at all. Run it `--network none`
and the WireGuard network is its **only** connectivity — there is nothing
else to escape to. The same move works for any netns: VMs bridged through a
namespace, or namespaces you built by hand with `ip netns add`.

## Works with any WireGuard device

wgxplore has no agent, no wire protocol of its own, and no lock-in format —
which makes it interoperable in both directions:

**It observes estates it did not create.** The inventory needs exactly two
things on the far side: reachable ssh and the `wg` tool. Linux servers, BSD
boxes, OpenWrt/VyOS routers, a hand-rolled `wg0` from 2019 — if
`ssh <host> wg show all dump` works, it is in the tree, health-coloured and
reconciled. Pre-existing interfaces (`wg-mgmt`, `wg-k8s`, …) appear beside
the networks wgxplore declared, and the undeclared-peer alarm covers all of
them.

**Its networks accept any WireGuard implementation.** A declaration compiles
to standard `[Interface]`/`[Peer]` config. Any device that speaks WireGuard
— a phone, a MikroTik, an OPNsense firewall, wg-quick anywhere — joins a
wgxplore network with its native tooling: give it its subnet address and the
hub's `[Peer]` block (public key, endpoint, allowed-ips), and add its public
key to the declaration so the estate view knows it belongs. (The rendered
files are `wg setconf` format — no `Address=` line, because wgxplore applies
addresses via `ip addr` so the same file works before *and* after a netns
move. A foreign device sets its address in its own UI, which it was doing
anyway.)

No flag day, no migration: point wgxplore at what you have, declare what
you add.

## Boundaries: same and different vs Tailscale, NetBird, wesher

**What is the same** — and this matters — is the wire. Tailscale, NetBird,
wesher, innernet, and wgxplore all move packets as WireGuard: the same Noise
handshake, the same cryptokey routing, the same 25-year-audit-surface-sized
protocol. None of them (wgxplore included) invent cryptography. The
differences are all in *everything around* the tunnel: who coordinates, who
relays, where policy lives, and what is left on the box when you uninstall.

| | coordination plane | datapath | container attach | policy | works with plain wg peers |
|---|---|---|---|---|---|
| Tailscale | SaaS + DERP relays | userspace (wireguard-go) | sidecar | ACL service | no — own control protocol |
| NetBird | own server | kernel (Linux) | sidecar | server-side | partially |
| innernet / dsnet | own server / registry | kernel | — | server-side | partially |
| wesher | gossip daemon on every node | kernel | — | — | no |
| wg-quick by hand | none | kernel | — | allowed-ips | yes — it *is* plain wg |
| **wgxplore** | **none — a file** | **kernel** | **netns move, no sidecar** | **allowed-ips (kernel-enforced)** | **yes — renders plain wg** |

Per-tool, where the boundary sits:

- **Tailscale** solves a different problem: any two devices, anywhere,
  through any NAT, zero configuration. The cost is a SaaS control plane
  that holds your ACLs and brokers your keys, DERP relays your packets may
  transit, and a userspace datapath. If the coordination service is down,
  new connections degrade. wgxplore has no third party to trust and nothing
  userspace in the path — and in exchange makes *you* provide one reachable
  UDP port.
- **NetBird** is closest in spirit on the datapath (kernel WireGuard) but
  keeps a management server and agents: membership, ACLs, and DNS live
  server-side, nodes run a daemon. wgxplore's "server" is a JSON file and
  its "agent" is nothing — the estate is read over the ssh you already have.
- **wesher** automates meshes with a gossip daemon on every node and
  encryption keys distributed via a cluster key. Convergence is automatic;
  the price is a resident daemon on every member and membership decided by
  gossip rather than by a file you review. wgxplore chose the file.
- **innernet / dsnet** are the config-manager cousins: server-issued
  invitations, CIDR-based ACLs, kernel datapath. Still a server and a
  database in the loop; still their config formats on the members.
- **wg-quick by hand** is the baseline wgxplore refuses to abandon: the
  result of `wgx net up` *is* a hand-rollable setup. wgxplore removes the
  N-configs-drifting problem (render from one declaration), the blindness
  (fleet-wide live view), and the trust (reconciliation + undeclared-peer
  alarm) — and adds the container netns move, which no config manager does.

**The trade we accept:** no CGNAT hole-punching, no relay fleet. A
hub-spoke estate needs one reachable UDP port. Two laptops behind hostile
NATs with no rendezvous are Tailscale's problem, not ours. Membership
changes propagate by re-rendering, not gossip: deliberate, visible,
file-shaped.

## wgxplore in kldload

[kldload](https://kldload.com) is wgxplore's first-party distribution — the
reproducible substrate installer bakes wgxplore into **every profile** it
ships:

- **`wgx` on every install** — desktop, server, kvm, core: the static TUI
  build is part of the OS, so the estate console is on every box, root
  shell or ssh session, no install step.
- **GUI where there is a screen** — GL-capable profiles get the Fyne GUI
  with a desktop tile beside zxplore's; headless profiles get the static
  binary. The installer asserts at build time which one it shipped.
- **Polkit integration** — reading WireGuard state needs `CAP_NET_ADMIN`;
  kldload ships a polkit rule so console users get the *read-only* estate
  view without a password prompt on every refresh. wgxplore escalates reads
  gently in order: direct if root → `sudo -n` → `pkexec`.
- **Traceability** — every kldload image records the exact wgxplore commit
  it ingested in `/etc/kldload/wgxplore-commit`, so any installed system
  answers "which wgxplore is this?".
- **Air-gap ready** — kldload's darksite builds ship wgxplore from a source
  cache, so the console exists on installs that have never seen the
  internet — which is exactly where a no-SaaS overlay is the only kind
  that works.

kldload is a consumer, not a dependency: wgxplore runs on any Linux with
kernel WireGuard, and its networks span any device that speaks the protocol
(see above).

## Use cases

- **See the estate you already have.** Zero adoption cost: install nothing
  anywhere else, run `wgx`, and every WireGuard interface on every
  ssh-reachable host appears in one tree with handshake health. Day one
  value on day one.
- **Catch the peer nobody added.** The reconciliation loop turns the
  inventory into an intrusion tripwire: a public key on a managed interface
  that no declaration lists means someone with root put it there. `wgx
  estate` in a cron job + grep for `UNDECLARED` is a real control.
- **Network-isolate a workload, hard.** Run a container `--network none`
  and `wgx attach` it: its only connectivity is an encrypted mesh whose
  peer list the kernel enforces. Great for scrapers, agents, build jobs —
  anything that should reach exactly the things you named and nothing else.
- **A management backplane that survives the CNI.** Declare a small
  hub-spoke net over your hypervisors, NAS, and routers: ssh, metrics, and
  backups ride an encrypted overlay that keeps working when the fancy
  networking above it is on fire.
- **Kubernetes node wires.** Declare the node-to-node underlay (the wires),
  let the CNI do its thing on top — wgxplore manages WireGuard interfaces,
  it does not fight your cluster's network plugin.
- **Air-gapped and darksite estates.** No SaaS, no phone-home, no relay:
  declarations are files, so a network can be declared, rendered, and
  carried in on the same USB stick as the OS. (This is the kldload
  darksite story.)
- **Mixed fleets.** The hub is a Linux box; the spokes are whatever you
  have — phones, OPNsense, OpenWrt, a colleague's hand-rolled wg0 —
  because members join with their native WireGuard tooling.
- **ZFS replication over the mesh** (the [zxplore](https://github.com/zxplore/zxplore)
  pairing). `zfs send` needs an encrypted transport, and ssh's userspace
  cipher is the classic single-core bottleneck. Over a wgxplore network the
  *kernel* owns the encryption, so replication can ride a dumb fast pipe
  inside the tunnel — `zfs send | mbuffer/nc → 10.99.0.2` — at line rate,
  multi-queue, no ssh in the datapath. Overlay addresses are declared and
  stable, so `zfs send` targets survive DHCP, ISP changes, and moves; a
  hub-spoke net makes the backup box reachable *only* over the mesh, which
  is an off-site replication target with no public surface at all.

## A worked example

Two machines and a container: `hub.example.net` (reachable UDP 51820) and a
laptop, plus an isolated container on the laptop. Declaring on the hub:

    # wgx net create lab --subnet 10.99.0.0/24 --topology hub-spoke
    ✓ network "lab" declared: hub-spoke 10.99.0.0/24 → /etc/wgx/lab.json
    # wgx net add lab hub --hub --endpoint hub.example.net:51820
    ✓ member "hub" → 10.99.0.1  pub 3fJk9…
    # wgx net add lab laptop
    ✓ member "laptop" → 10.99.0.2  pub 8Qw2p…
    # wgx net add lab ct1
    ✓ member "ct1" → 10.99.0.3  pub Zr51x…
    # wgx net render lab
    ✓ /etc/wgx/lab/hub.conf
    ✓ /etc/wgx/lab/laptop.conf
    ✓ /etc/wgx/lab/ct1.conf

Bring the hub up — every command is echoed before it runs:

    # wgx net up lab hub
    $ ip link add wgx-lab type wireguard
    $ wg setconf wgx-lab /etc/wgx/lab/hub.conf
    $ ip addr add 10.99.0.1/24 dev wgx-lab
    $ ip link set wgx-lab up
    ✓ wgx-lab up as "hub" (10.99.0.1)

Copy `/etc/wgx/lab.json` (or just `laptop.conf`) to the laptop, `wgx net up
lab laptop` there, and the tunnel is live — `ping 10.99.0.1` answers.
Spokes have no ListenPort and roam freely; the hub is the only address
anyone must reach. Now the container, on the laptop:

    # podman run -d --network none --name scraper myimage
    # wgx attach lab scraper ct1
    $ ip link add wgx-bGFic2Ny type wireguard
    $ wg setconf wgx-bGFic2Ny /etc/wgx/lab/ct1.conf
    $ ip link set wgx-bGFic2Ny netns 41337
    $ nsenter -t 41337 -n ip addr add 10.99.0.3/24 dev wgx-bGFic2Ny
    $ nsenter -t 41337 -n ip link set wgx-bGFic2Ny up
    ✓ container "scraper" is on "lab" as "ct1" (10.99.0.3) — interface
      lives in its netns, socket anchored here

The container can now reach `10.99.0.1` (and, hub-spoke, *only* the hub —
cryptokey routing drops anything else) while having no other network at
all. Check the estate from anywhere:

    $ wgx estate
    estate: 2 hosts · 2 interfaces · 3 peers · 3 alive

    local
      ├─ wgx-lab  10.99.0.0/24  (2 peers)
      │  ├─ ● 10.99.0.2/32   —                 42s
      │  └─ ● 10.99.0.3/32   —                 8s

    laptop
      ├─ wgx-lab  10.99.0.0/24  (1 peers)
      │  └─ ● 10.99.0.0/24   hub.example.net:51820   12s

## Use

    wgx                                    the console (GUI, or TUI over ssh)
    wgx estate                             the whole estate as a text tree
    wgx show                               peer dossiers, one-shot

    # declare a lab: one reachable hub, spokes join from anywhere
    wgx net create lab --subnet 10.99.0.0/24 --topology hub-spoke
    wgx net add lab host1 --hub --endpoint host1.example.net:51820
    wgx net add lab ct1
    wgx net render lab                     one .conf per member, /etc/wgx/lab/
    wgx net up lab host1                   this host up as host1 (root)
    wgx attach lab <container> ct1         container's ONLY net is the mesh

Remote hosts come from `~/.ssh/config` (or `~/.config/wgx/hosts`); the far
side needs only `sshd` and `wg`. Every mutation prints the exact `wg`/`ip`
command before running it.

## Build

Prerequisites: Go ≥ 1.26 and, at runtime, `wireguard-tools` (`wg`) plus a
kernel with WireGuard (mainline since 5.6 — i.e. anything current). The GUI
build additionally needs the GL/X11/wayland dev stack:

    # Fedora
    dnf install golang wireguard-tools gcc libX11-devel libXcursor-devel \
        libXrandr-devel libXinerama-devel libXi-devel mesa-libGL-devel \
        libxkbcommon-devel wayland-devel
    # Debian/Ubuntu
    apt install golang wireguard-tools gcc libx11-dev libxcursor-dev \
        libxrandr-dev libxinerama-dev libxi-dev libgl1-mesa-dev \
        libxkbcommon-dev libwayland-dev

Then:

    make            # both: wgx (GUI+TUI, cgo/Fyne) and wgx-tui (static)
    make wgx-tui    # just the static console — no cgo, no GL deps needed
    make test       # go vet, both build variants
    make install    # into /usr/local/bin (PREFIX/DESTDIR respected)

Or without the Makefile:

    CGO_ENABLED=0 go build -o wgx-tui .          # static TUI
    CGO_ENABLED=1 go build -tags gui -o wgx .    # GUI + TUI

The static `wgx-tui` has **zero runtime dependencies** — `scp` it to any
Linux box and the console works there; only the machines whose interfaces
you *mutate* need `wg`/`ip` installed. Estate scanning of remote hosts
needs nothing on the far side beyond `sshd` and `wg`.

## Status

0.1.0 — engine, estate inventory, reconciliation, TUI, GUI. Honest caveats
of the prototype: member private keys live in the declaration file
(per-member key custody is the plan), membership changes re-render by hand,
IPv4 only. Enrollment, key rotation, and joined networks are next.

BSD-3-Clause.
