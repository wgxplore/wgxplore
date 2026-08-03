# wgxplore

**The WireGuard networks console.** Sister project to
[zxplore](https://github.com/zxplore/zxplore): same console shape, same
primitives ethos, one domain over.

Declare networks (ptp / hub-spoke / mesh / joined), attach anything to them —
hosts, **containers via the kernel's netns move**, VMs — and see the whole
estate in one keyboard-driven view: every device, every peer, handshake-fresh
or stale, declared or **not declared by anyone**.

## Why it is different

Every ingredient exists somewhere; the combination does not:

| | coordination plane | datapath | container attach | policy |
|---|---|---|---|---|
| Tailscale | SaaS + DERP relays | userspace (wireguard-go) | sidecar | ACL service |
| NetBird | own server | kernel (Linux) | sidecar | server-side |
| wesher | gossip daemon | kernel | — | — |
| **wgxplore** | **none — a file** | **kernel** | **netns move, no sidecar** | **allowed-ips (kernel-enforced)** |

The trade we accept: no CGNAT hole-punching. An estate with one reachable hub
port does not need a relay fleet; two laptops behind hostile NATs are not the
target.

## Use

    wgx                                    the console (TUI)
    wgx show                               peer dossiers, one-shot
    wgx net create lab --subnet 10.99.0.0/24 --topology hub-spoke
    wgx net add lab host1 --hub --endpoint host:51820
    wgx net add lab ct1
    wgx net render lab && wgx net up lab host1
    wgx attach lab <container> ct1         container's ONLY net is the mesh

Declarations live in `/etc/wgx/<name>.json` — the file is the API. Remote
hosts come from `~/.ssh/config`; the far side needs only `sshd` and kernel
WireGuard. Every mutation prints the exact `wg`/`ip` command before running it.

## Status

0.1.0 — engine + estate inventory + TUI. GUI (Fyne, zxplore's chassis),
enrollment, key rotation, and joined networks are next.

BSD-3-Clause.
