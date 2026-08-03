# Using wgxplore: worked examples

> Every flow is real; under each one are the exact commands it runs.

Every flow below is real, and under each is the exact command it runs —
because that's the contract: you could always have typed it yourself.

**Declare a lab and bring the hub up.**
```
# wgx net create lab --subnet 10.99.0.0/24 --topology hub-spoke
# wgx net add lab hub --hub --endpoint hub.example.net:51820
# wgx net add lab laptop
# wgx net render lab
# wgx net up lab hub
$ ip link add wgx-lab type wireguard
$ wg setconf wgx-lab /etc/wgx/lab/hub.conf
$ ip addr add 10.99.0.1/24 dev wgx-lab
$ ip link set wgx-lab up
```
Copy `laptop.conf` over, `wgx net up lab laptop` there, and `ping 10.99.0.1`
answers. Spokes have no ListenPort and roam freely; the hub is the only
address anyone must reach.

**Give a running container exactly one network: yours.**
```
# podman run -d --network none --name scraper myimage
# wgx attach lab scraper ct1
$ ip link add wgx-bGFic2Ny type wireguard
$ wg setconf wgx-bGFic2Ny /etc/wgx/lab/ct1.conf
$ ip link set wgx-bGFic2Ny netns 41337
$ nsenter -t 41337 -n ip addr add 10.99.0.3/24 dev wgx-bGFic2Ny
$ nsenter -t 41337 -n ip link set wgx-bGFic2Ny up
```
The container now owns a mesh interface it cannot escape — plaintext never
exists inside its namespace, and hub-spoke means it can reach the hub and
*nothing else*. (Why this works: [the netns move](#the-netns-move).)

**See everything, from anywhere.**
```
$ wgx estate
estate: 2 hosts · 2 interfaces · 3 peers · 3 alive

local
  ├─ wgx-lab  10.99.0.0/24  (2 peers)
  │  ├─ ● 10.99.0.2/32   —                       42s
  │  └─ ● 10.99.0.3/32   —                       8s

laptop
  ├─ wgx-lab  10.99.0.0/24  (1 peers)
  │  └─ ● 10.99.0.0/24   hub.example.net:51820   12s
```
Remote hosts come from `~/.ssh/config` (or `~/.config/wgx/hosts`); the far
side needs **nothing but `sshd` and `wg`**. Unreachable boxes stay in the
tree — "the box is down" is inventory, not an error.

**Catch the peer nobody added.**
```
$ wgx estate | grep UNDECLARED   # a real control, cron-able
```
The reconciliation is three states: declared + live → healthy · declared,
absent → never came up or host down · **live, undeclared → alarm**. The
alarm arms **per interface**: an interface is *managed* when its own key
appears in a declaration. Interfaces wgxplore merely observes (your
pre-existing `wg0`) read as neutral *untracked* — an adopted estate is
inventory, not a wall of false alarms.

**Join a device wgxplore will never run on.**
Give a phone / MikroTik / OPNsense its subnet address and the hub's `[Peer]`
block in its native UI, add its public key to the declaration, done — the
estate view now knows it belongs.


## Five ways to use it

Ever wondered what it would be like to point at a host — data, workloads,
backplane network and all — and spray running copies of it across any
cloud, any site, any bare-metal box, **as if the boundaries between them
didn't exist**? That's what these verbs unlock when the whole stack is
present (example 1). The rest are day-one wins that need nothing but this
tool: the same four verbs — declare, render, up, attach — pointed at five
different problems.

### 1 · Two sites, one backplane: replicate live workloads across the internet

Two [kldload](https://kldload.com) systems, anywhere on the internet, one
declared network between them — and every layer above inherits it:

```
# on site-a (the reachable one)
wgx net create backplane --subnet 10.77.0.0/24 --topology hub-spoke
wgx net add backplane site-a --hub --endpoint a.example.net:51820
wgx net add backplane site-b
wgx net add backplane db-a          # containers get memberships too
wgx net add backplane db-b
wgx net render backplane && wgx net up backplane site-a
# on site-b: wgx net up backplane site-b, then attach the live containers
wgx attach backplane db-a db-a      # site-a
wgx attach backplane db-b db-b      # site-b
```

Two *running* containers on two different sites now share a private,
kernel-encrypted backplane that neither the sites' firewalls nor the
containers' own isolation can see into — `db-b` replicates from
`db-a:5432` at `10.77.0.3` as if they shared a rack. And because kldload
is a ZFS substrate, the composition goes further with primitives only:
container volumes are datasets, so **moving a workload between sites is
`zfs send` over the mesh** — snapshot on site-a, send at kernel speed
([zxplore](https://github.com/zxplore/zxplore) makes it two panes and a
confirm), start the container on site-b, `wgx attach` it, and it comes up
at the same overlay address with its data. Node-to-node k8s traffic,
etcd/NATS replication, golden-image distribution — anything that can
target an IP can ride the backplane.

### 2 · The workload that can reach exactly one thing

An agent, a scraper, a build job — something you want to *use* but not
*trust*. Give it no network at all, then attach it to a hub-spoke net
whose hub is the one service it may talk to:

```
wgx net create egress --subnet 10.66.0.0/24 --topology hub-spoke
wgx net add egress api --hub --endpoint api.internal:51820
wgx net add egress job
wgx net render egress
podman run -d --network none --name job untrusted-image
wgx attach egress job job
```

From inside: `10.66.0.1` answers, and *nothing else exists* — no default
route, no DNS, no underlay, and spoke-to-spoke traffic dies in the kernel
because no key routes there. This is not a firewall rule the workload
might find a way around; there is no other network **to** reach. Fully
compromised, the job can talk to the API it was already allowed to talk
to.

### 3 · The backup box that doesn't exist

An off-site backup target at a relative's house, behind their NAT, with
**zero open ports**. Make your home box the hub; the backup box is a
spoke that only ever dials out:

```
wgx net create vault --subnet 10.88.0.0/24 --topology hub-spoke
wgx net add vault home --hub --endpoint home.example.net:51820
wgx net add vault backup
wgx net render vault && wgx net up vault home
# at the relative's house, once: wgx net up vault backup
```

The spoke's `PersistentKeepalive` holds the NAT mapping open, so the hub
can always reach `10.88.0.2` — but from the internet's point of view the
backup box is not there: nothing listens, nothing answers, no port
forward exists. Nightly, from home:

```
zfs send -w -i @last tank/vault@today | ssh 10.88.0.2 zfs recv backups/vault
```

Encrypted datasets travel raw (`-w`) — the box stores what it can never
read, over a link only your keys can address.

### 4 · The tripwire

Declare your estate and the reconciliation loop becomes an intrusion
control. A WireGuard peer cannot appear by accident: cryptokey routing
means someone with root added that key. So watch for exactly that:

```
# monitoring, anywhere that can ssh the estate:
wgx estate | grep -q UNDECLARED && notify "rogue WireGuard peer"
```

Every declared interface on every host is checked against the
declarations on every scan — a key you didn't add lights up `⚠ UNDECLARED`
in the console and lands in that grep. Interfaces wgxplore doesn't manage
read as neutral *untracked* (inventory, not judgement), so the alarm only
ever means the one thing worth waking up for. (`wgx net adopt`, on the
roadmap, brings pre-existing interfaces under declaration.)

### 5 · The phone in your pocket

Members don't run wgxplore — they speak WireGuard. Add a phone to a
network and hand it its config through the app it already has:

```
wgx net add road phone
wgx net render road
qrencode -t ansiutf8 < /etc/wgx/road/phone.conf   # scan with the WG app
```

Add the address (`10.44.0.3/24`) in the app's interface screen — done:
the phone is on your estate's network with its native client, visible in
the estate view like every other peer, no app of ours anywhere near it.
The same flow is a MikroTik, an OPNsense box, or a colleague's
hand-rolled config — paste keys instead of scanning.

