# Where wgxplore sits

> The boundaries, the trades, and the stack thesis — the long-form version
> of the [README](../README.md) positioning.

**What's the same is the wire** — all of these move packets as real
WireGuard: same Noise handshake, same cryptokey routing. Nobody here
invents cryptography. The differences are everything *around* the tunnel:

| | coordination plane | datapath | container attach | policy | plain-wg peers |
|---|---|---|---|---|---|
| Tailscale | SaaS + DERP relays | userspace (wireguard-go) | sidecar | ACL service | no |
| NetBird | own server | kernel (Linux) | sidecar | server-side | partially |
| innernet / dsnet | own server / registry | kernel | — | server-side | partially |
| wesher | gossip daemon per node | kernel | — | — | no |
| wg-quick by hand | none | kernel | — | allowed-ips | yes — it *is* plain wg |
| **wgxplore** | **none — a file** | **kernel** | **netns move, no sidecar** | **allowed-ips (kernel)** | **yes — renders plain wg** |

- **Tailscale** solves any-two-devices-anywhere through any NAT, zero
  config — at the cost of a SaaS control plane holding your ACLs, relays
  your packets may transit, and a userspace datapath. wgxplore trusts no
  third party and keeps userspace out of the path — and in exchange asks
  *you* for one reachable UDP port.
- **NetBird** is closest on the datapath (kernel WireGuard) but keeps a
  management server and per-node agents. wgxplore's "server" is a JSON
  file; its "agent" is the ssh you already have.
- **wesher** converges automatically via a gossip daemon on every member;
  membership is decided by gossip, not by a file you review. wgxplore
  chose the file.
- **innernet / dsnet** are the config-manager cousins — server-issued
  invites, kernel datapath, their formats on the members. Still a server
  and a database in the loop.
- **wg-quick by hand** is the baseline wgxplore refuses to abandon: the
  result *is* hand-rollable. wgxplore removes the N-configs-drifting, the
  blindness, and the trust — and adds the container attach, which no
  config manager does.

**The trade we accept:** no CGNAT hole-punching, no relay fleet. One
reachable UDP port runs an estate; two laptops behind hostile NATs are
Tailscale's problem, not ours. Membership propagates by re-rendering, not
gossip: deliberate, visible, file-shaped.

**The one-line map:** wgxplore *competes* with the config managers,
*completes* everything that already speaks WireGuard, and *coexists* with
the SaaS meshes — run one for your roaming laptop and wgxplore for the
estate you own, on the same day, without conflict.

## Where wgxplore sits

Draw the map honestly and the position is narrow and strong:

**Built for:** an estate you *own* — machines, VMs, containers, and at
least one reachable UDP port somewhere. Membership that changes monthly,
not hourly. Operators who want nothing in the trust chain but their file,
their kernel, and their ssh.

**Wrong tool for:** two roaming devices with no home base (that is
genuinely Tailscale's game); thousand-node fleets with hourly churn (run a
control plane, you'll need one); L2 overlays (WireGuard is L3 — no
broadcast, no VXLAN games).

**Strengths, concretely:**

- *Nothing to run, nothing to lose.* The "registry" is a file. Lose it and
  the estate still works — the kernel state is readable back.
- *Nothing in the datapath but the kernel.* No relay outage, no bandwidth
  ceiling set by someone else, no third party your packets transit.
- *Revocation is subtraction.* Remove the member, re-render: the kernel
  stops routing that key. No CRL, no token expiry to reason about.
- *The audit story is `git log /etc/wgx`.* Every membership change is a
  file diff made by a person.
- *The container attach.* No agent-based tool can do it: the workload gets
  the mesh without any process alongside it.

**Weaknesses, concretely:**

- *No rendezvous, no network.* If no member has a reachable port, nothing
  forms — there is no relay to save you.
- *Convergence is you.* Add a member at midnight and the estate learns at
  midnight only if you re-render at midnight.
- *Prototype key custody.* Today the declaration holds private keys —
  treat `/etc/wgx` like a keystore until per-member custody lands (top of
  the roadmap).


## Scope: one thing, done well

wgxplore does **declared WireGuard, reconciled**: a file describes the
network, primitives apply it, the console diffs reality against it. That
is the whole program. The Unix answer to everything adjacent is
*composition*, not features:

| you want | compose |
|---|---|
| time-boxed access | `cron` + `wgx net render` — prune the member, re-render |
| enrollment | `scp` the conf out, `wgx net add` the key back over ssh |
| membership history & review | keep `/etc/wgx` in `git` — the diff *is* the change request |
| networks up at boot | your init: a oneshot unit that runs `wgx net up` |
| alerting | `wgx estate \| grep UNDECLARED` in the monitor you already run |
| moving workloads | `zfs send` / `rsync` / `pg_basebackup` — over the mesh |

What wgxplore will **never** grow, because each one is a second thing: a
daemon, a database, a broker or identity service, relays or NAT
traversal, a CNI. The tools that made those choices are listed in the
boundaries table — they are fine tools solving a different problem.

The short roadmap only sharpens the one thing:

- **Per-member key custody** — only *public* keys in the declaration; the
  file describes the network without being a keystore.
- **Fleet-grade verbs** — deterministic gap-filling address allocation for
  any subnet size, idempotent `net add`, `--json` output, and subnet
  carving from a declared pool: the primitives a fleet script, OpenTofu
  run, or appliance can drive in a loop. The *decisions* stay in the
  caller — wgx allocates from the file, never from a service.
- **`wgx net adopt`** — bring a live untracked interface under
  declaration, arming the alarm on estates that predate wgxplore.
- **Key rotation** — staged re-key, both keys valid during the swap.
- **Audit log** — every executed mutation appended with timestamp and
  argv, zxplore-style.
- **Joined networks** — a gateway member bridging two declared networks,
  policy still nothing but AllowedIPs.


### The line in the sand

On the kldload stack, wgxplore is the network half of a bigger claim —
that the traditional physics of infrastructure is optional:

**Traditional:** storage is hardware — disks, partitions, filesystems,
and a userland pile of tools (rsync, tar, dd, LVM, backup agents) to move
bytes between them. Workloads are *installations*: a VM is a disk chained
to its hypervisor, a container is layers married to one host. Networks
are *places* — subnets that mean something only where they are. Moving
any of it is a project, with downtime.

**The stack:** ZFS makes storage a uniform data lake — the VM disk, the
container volume, the root filesystem, the database are all **datasets**,
and four primitives (`snapshot`, `send`, `recv`, `clone`) replace the
tool pile. Datasets exist wherever they're imported; replication is
point-and-shoot between any number of hosts
([zxplore](https://github.com/zxplore/zxplore) makes it two panes and a
confirm). wgxplore makes the network a declared **property** instead of a
place: `10.77.0.3` means the same thing at any site, and attaching it to
a host, VM, or *running container* is one verb.

Put together, a workload stops being an installation and becomes **a
dataset plus an overlay address** — a connectable image. Replicate it
anywhere, spin it up, attach it, and it comes online with the same
identity and the same data; destroy the copy and nothing is lost that a
snapshot doesn't hold. Storage goes from hardware to objects; "per-disk"
VMs and per-host containers stop being facts of life. The wires for all
of it are kernel WireGuard, declared in one file — which is the only part
this repo provides, and the only part it needs to.

