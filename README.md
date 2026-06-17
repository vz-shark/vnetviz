# vnetviz

**Visualize Linux network topology as Mermaid / Graphviz**

`vnetviz` auto-detects the Linux network configuration — network namespaces,
veth pairs, bridges, bonds, VLANs, Docker and Podman containers — and emits a
human-friendly diagram as a text tree, Mermaid, or Graphviz (DOT/SVG/PNG).

```text
Linux Network  ->  internal graph  ->  Mermaid / DOT  ->  SVG / PNG
```

## Install

One-liner (downloads a prebuilt Linux binary into `/usr/local/bin`, which
usually needs root — hence `sudo sh`):

```bash
curl -fsSL https://raw.githubusercontent.com/vz-shark/vnetviz/main/install.sh | sudo sh
```

The script never calls `sudo` on its own; if `/usr/local/bin` is not writable it
just tells you to re-run with `sudo`. To install without root, point
`VNETVIZ_BIN_DIR` at a directory you own:

```bash
curl -fsSL https://raw.githubusercontent.com/vz-shark/vnetviz/main/install.sh \
  | VNETVIZ_BIN_DIR="$HOME/.local/bin" sh
```

It verifies the release's SHA-256 checksum before installing. Pin a version with
`VNETVIZ_VERSION` (e.g. `VNETVIZ_VERSION=v0.1.0`).

With Go installed you can instead use `go install`:

```bash
go install github.com/vz-shark/vnetviz/cmd/vnetviz@latest
```

Or build from source:

```bash
go build -o vnetviz ./cmd/vnetviz
```

## Usage

```bash
# text tree to stdout: the default format and the default (virtual) scope —
# bridges, veth, VLANs, netns and containers, but not the host's physical NICs
# (root is needed to enter other namespaces)
sudo vnetviz

# Mermaid to stdout
sudo vnetviz --format mermaid

# Mermaid to a Markdown file: wrapped in a ```mermaid fence so it renders
# inline on GitHub
sudo vnetviz --format mermaid -o diagram.md

# Graphviz DOT, everything enabled, to a file -> render with dot
sudo vnetviz --format dot --all --output net.dot
dot -Tsvg net.dot -o net.svg

# SVG / PNG directly (vnetviz shells out to Graphviz `dot` for you)
sudo vnetviz --format svg --all --output net.svg
sudo vnetviz --format png --all --output net.png
```

If vnetviz finds network namespaces (named netns or containers) it cannot enter
because it is not running as root, it prints an error on stderr suggesting how to
re-run under `sudo` and exits non-zero (no partial diagram is produced):

```text
vnetviz: cannot read 1 namespace(s) without root: vztest
         re-run with: sudo vnetviz --ip
```

The message is colored yellow only when stderr is a terminal, so piped or
redirected output stays plain.

The `svg` and `png` formats require [Graphviz](https://graphviz.org/) (`dot`)
on your `PATH`; vnetviz generates the DOT internally and pipes it through
`dot -Tsvg` / `dot -Tpng`. When `--output` is omitted the diagram is written to
standard output (`png` to a terminal is refused — use `--output`).

The default `text` format prints a compact, `tree(1)`-style tree, readable
straight in the terminal:

```text
host
├─ br-9f3a21  [bridge]  (appnet)  172.18.0.1/16  up
│　├─ veth9a1  ==( veth )==  eth0  @web (docker)  172.18.0.2/16  up
│　└─ vethb22  ==( veth )==  eth0  @db (docker)  172.18.0.3/16  up
└─ host:8080  ==>  web:80/tcp
```

A bridge backed by a Docker/Podman network shows that network's friendly name in
parentheses (`(appnet)` above), and a published port appears as a self-contained
`host:<port>` node. Run with `--all` to also include the host's physical NICs and
loopback.

Like `tree(1)`, the default `text` format picks its line-drawing characters
from the locale: a UTF-8 locale gets Unicode connectors, otherwise it falls back
to ASCII (so `LANG=C vnetviz` looks like `tree --charset=ascii`). Use `--format
unicode` or `--format ascii` to force one regardless of locale:

```text
host
|-- br-9f3a21  [bridge]  (appnet)  172.18.0.1/16  up
|   |-- veth9a1  ==( veth )==  eth0  @web (docker)  172.18.0.2/16  up
|   `-- vethb22  ==( veth )==  eth0  @db (docker)  172.18.0.3/16  up
`-- host:8080  ==>  web:80/tcp
```

Each interface ends with its operational state (`up` / `down`). A down
interface has its whole row dimmed gray when the output goes to a terminal;
piped or redirected output stays plain. In `mermaid` output the state is shown
by the `up` / `down` label only (no custom color, so it reads in both light and
dark themes); in `dot` output down nodes are grayed and the word is omitted.

With `--collapse` the veth is folded away and the bridge links straight to
each container interface (`docker0 -> eth0 @web`).

### Options

Bridges, veth pairs, VLANs, named netns, and Docker/Podman containers — with
container names, bridge network names, and published-port nodes — are always
detected (a missing container CLI is skipped silently). The scope flags below
only add the host's own loopback/physical NICs and addresses.

| Category | Option | Description |
|---|---|---|
| Format | `--format text` | text tree, charset auto-selected from locale (default) |
|  | `--format unicode` | text tree, forced Unicode line drawing |
|  | `--format ascii` | text tree, forced ASCII (`tree --charset=ascii`) |
|  | `--format mermaid` | Mermaid output |
|  | `--format dot` | Graphviz DOT output |
|  | `--format svg` | SVG image (requires Graphviz `dot`) |
|  | `--format png` | PNG image (requires Graphviz `dot`) |
|  | `-f` | shorthand for `--format` |
| Output | `--output FILE` | write to FILE instead of stdout |
|  | `-o FILE` | shorthand for `--output` |
| Scope | `--virtual` | virtual topology only: adds `--ip` (default) |
|  | `-v` | shorthand for `--virtual` |
|  | `--all` | enable `--lo --ip --physical` |
|  | `--lo` | show loopback interfaces |
|  | `--ip` | show IP addresses |
|  | `--physical` | show physical NICs |
| Display | `--collapse` | collapse veth pairs into a single link |
|  | `--up-only` | hide interfaces that are operationally down |
| Misc | `--version` | print version and exit |

## How it works

* Interfaces and addresses are read with netlink (`vishvananda/netlink`).
* Namespaces are discovered from `/run/netns` (named) and from
  `/proc/<pid>/ns/net` for running Docker / Podman containers, then entered with
  `setns` (`vishvananda/netns`) — this is why root is required for the full
  picture.
* Published container ports come from `docker`/`podman inspect`, not
  from any interface: a publish is an iptables DNAT, so it is drawn as a
  self-contained dummy node in the host with the forward baked into its label
  (e.g. `host:8080  ==>  web:80/tcp`) — no edge is drawn across the diagram.
* veth pairs are matched by ifindex / iflink, which works **across** namespaces,
  so a container's `eth0` is correctly joined to its host-side veth and bridge.

## Layout

```
cmd/vnetviz       CLI entry point and flag parsing
internal/model    namespace/interface graph + edge derivation
internal/collect  live network state collection (netlink, netns, containers)
internal/render   Text (ASCII), Mermaid and DOT renderers
```

## Status & roadmap

Supported today:

- network namespaces (host, named netns, Docker/Podman containers)
- veth pairs matched across namespaces
- Linux bridges and bridge membership
- bonds and VLANs (802.1Q)
- IP addresses, loopback, and physical NICs
- Docker / Podman container discovery, with friendly network names on bridges
- published container ports as `host:<port>` nodes
- up/down state, with `--up-only` to hide down interfaces

Planned:

- VRF and Open vSwitch
- Kubernetes CNIs (Calico, Flannel, Cilium)
- rootless Podman networking (`slirp4netns` / `pasta`)

## License

MIT
