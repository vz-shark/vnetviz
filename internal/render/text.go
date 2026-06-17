package render

import (
	"fmt"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// Text renders the topology as a tree(1)-style tree, e.g.
//
//	host
//	├─ docker0  [bridge]  up
//	│　├─ veth9  ==( veth )==  eth0  @web (docker)  172.17.0.2/16  up
//	│　└─ ens18  [physical]  192.168.1.117/24  up
//	└─ ...
//
// It is meant for reading straight in a terminal, no external tool required.
func Text(t *model.Topology, opts Options) string {
	peer := vethPeers(t)
	parent := vlanParents(t)
	masters := masterLinks(t)

	g := glyphsFor(opts.Charset)

	var b strings.Builder
	for i, ns := range t.Namespaces {
		if i > 0 {
			b.WriteString("\n")
		}
		rows := nsRows(t, ns, peer, parent, masters, opts)
		b.WriteString(nsTitle(ns))
		b.WriteString("\n")
		writeTree(&b, rows, "", g)
	}
	return b.String()
}

// treeGlyphs holds the four connector strings used to draw the tree. Within a
// charset all four have the same display width so children stay aligned.
type treeGlyphs struct {
	tee   string // branch for a non-last entry
	last  string // branch for the last entry
	vert  string // continuation prefix below a non-last entry
	blank string // continuation prefix below the last entry
}

var (
	// unicodeGlyphs is the default, compact Unicode style. The continuation
	// below a branch uses a full-width space so it lines up with "├─ ".
	unicodeGlyphs = treeGlyphs{tee: "├─ ", last: "└─ ", vert: "│　", blank: "   "}
	// asciiGlyphs matches `tree --charset=ascii`: only ASCII, no full-width.
	asciiGlyphs = treeGlyphs{tee: "|-- ", last: "`-- ", vert: "|   ", blank: "    "}
)

func glyphsFor(cs Charset) treeGlyphs {
	if cs == CharsetASCII {
		return asciiGlyphs
	}
	return unicodeGlyphs
}

// textRow is one node in the ASCII tree.
type textRow struct {
	label    string
	children []textRow
}

// nsRows builds the top-level rows for a namespace: each bridge with its
// enslaved members as children, followed by any free-standing interfaces.
func nsRows(t *model.Topology, ns *model.Namespace, peer, parent, masters map[*model.Iface]*model.Iface, opts Options) []textRow {
	var bridges, free []*model.Iface
	members := map[int][]*model.Iface{} // master ifindex -> members

	for _, i := range ns.Ifaces {
		switch {
		case i.Type.IsMaster():
			bridges = append(bridges, i)
		case i.MasterIndex != 0:
			members[i.MasterIndex] = append(members[i.MasterIndex], i)
		default:
			free = append(free, i)
		}
	}

	var rows []textRow
	for _, m := range bridges {
		row := textRow{label: ifaceLabel(m, peer, parent, masters, opts)}
		for _, mem := range members[m.Index] {
			// In collapse mode a veth member is folded away if its peer is
			// shown on the connection line, so we still emit one row per
			// member but the label points at the remote end.
			row.children = append(row.children, textRow{label: ifaceLabel(mem, peer, parent, masters, opts)})
		}
		rows = append(rows, row)
	}
	for _, f := range free {
		// A free veth whose master we never saw still gets its peer annotated.
		rows = append(rows, textRow{label: ifaceLabel(f, peer, parent, masters, opts)})
	}
	// Published ports listen on the host, so their dummy "host:<port>" nodes are
	// shown under the host, forwarding into the container, e.g.
	// "host:8080  ==>  web:80/tcp".
	if ns.Kind == model.KindHost {
		for _, c := range t.Namespaces {
			for _, p := range c.Ports {
				rows = append(rows, textRow{label: pubForwardLabel(c, p)})
			}
		}
	}
	return rows
}

// ifaceLabel formats a single interface line, including its type tag, any
// addresses, and—for a veth—the peer it connects to.
func ifaceLabel(i *model.Iface, peer, parent, masters map[*model.Iface]*model.Iface, opts Options) string {
	var sb strings.Builder
	sb.WriteString(i.Name)

	switch i.Type {
	case model.TypeVeth:
		// type tag omitted; the connection line below conveys "veth".
	case model.TypeVlan:
		sb.WriteString("  [" + vlanTag(i))
		if p, ok := parent[i]; ok {
			sb.WriteString(" @" + p.Name)
		}
		sb.WriteString("]")
	default:
		sb.WriteString("  [" + string(i.Type) + "]")
	}
	if i.Network != "" {
		sb.WriteString("  (" + i.Network + ")")
	}

	if p, ok := peer[i]; ok {
		if opts.CollapseVeth {
			if br, ok := masters[p]; ok && i.MasterIndex == 0 {
				// i is a free veth (e.g. a container's eth0) whose host-side peer
				// is enslaved to a bridge: show that bridge as the remote end,
				// keeping i's own name and addresses so the row reads from i's
				// side, e.g. "eth0  ==( veth )==  br-xxx  @host  172.18.0.5/16".
				sb.WriteString(fmt.Sprintf("  ==( veth )==  %s  @%s", br.Name, nsTitle(br.NS)))
				if len(i.Addrs) > 0 {
					sb.WriteString("  " + strings.Join(i.Addrs, " "))
				}
				sb.WriteString("  " + stateWord(i))
				return colorDown(sb.String(), i, opts.Color)
			}
			// Collapse: fold the local veth away and present the remote
			// endpoint directly, e.g. "eth0  @web (docker)  172.17.0.2/16".
			sb.Reset()
			sb.WriteString(fmt.Sprintf("%s  @%s", p.Name, nsTitle(p.NS)))
			if len(p.Addrs) > 0 {
				sb.WriteString("  " + strings.Join(p.Addrs, " "))
			}
			sb.WriteString("  " + stateWord(p))
			return colorDown(sb.String(), p, opts.Color)
		}
		sb.WriteString(fmt.Sprintf("  ==( veth )==  %s  @%s", p.Name, nsTitle(p.NS)))
		if len(p.Addrs) > 0 {
			sb.WriteString("  " + strings.Join(p.Addrs, " "))
		}
	}

	if len(i.Addrs) > 0 {
		sb.WriteString("  " + strings.Join(i.Addrs, " "))
	}
	sb.WriteString("  " + stateWord(i))
	return colorDown(sb.String(), i, opts.Color)
}

// colorDown grays a whole interface line with ANSI when color is on and the
// interface is down, so the entire row reads as inactive on a terminal; up
// interfaces and non-colored output are left plain.
func colorDown(s string, i *model.Iface, color bool) string {
	if !color || i.Up {
		return s
	}
	const gray, reset = "\033[38;5;245m", "\033[0m"
	return gray + s + reset
}

// vethPeers maps each veth interface to the interface at the other end of the
// pair, derived from the topology's edges.
func vethPeers(t *model.Topology) map[*model.Iface]*model.Iface {
	m := map[*model.Iface]*model.Iface{}
	for _, e := range t.Edges() {
		if e.Kind == model.EdgeVeth {
			m[e.A] = e.B
			m[e.B] = e.A
		}
	}
	return m
}

// masterLinks maps each enslaved interface to its master (bridge/bond) within
// the same namespace, resolved via the kernel's master ifindex. It lets the
// collapsed view name the bridge a container's veth ultimately attaches to.
func masterLinks(t *model.Topology) map[*model.Iface]*model.Iface {
	out := map[*model.Iface]*model.Iface{}
	for _, ns := range t.Namespaces {
		byIndex := make(map[int]*model.Iface, len(ns.Ifaces))
		for _, i := range ns.Ifaces {
			byIndex[i.Index] = i
		}
		for _, i := range ns.Ifaces {
			if i.MasterIndex != 0 {
				if m, ok := byIndex[i.MasterIndex]; ok {
					out[i] = m
				}
			}
		}
	}
	return out
}

// vlanParents maps each VLAN interface to its parent (lower) device.
func vlanParents(t *model.Topology) map[*model.Iface]*model.Iface {
	m := map[*model.Iface]*model.Iface{}
	for _, e := range t.Edges() {
		if e.Kind == model.EdgeVlan {
			m[e.A] = e.B
		}
	}
	return m
}

// writeTree prints rows as a tree(1)-style tree using the given connector
// glyphs, with no blank spacer lines so the output stays compact.
func writeTree(b *strings.Builder, rows []textRow, prefix string, g treeGlyphs) {
	for i, r := range rows {
		last := i == len(rows)-1
		branch, childPrefix := g.tee, prefix+g.vert
		if last {
			branch, childPrefix = g.last, prefix+g.blank
		}
		fmt.Fprintf(b, "%s%s%s\n", prefix, branch, r.label)
		writeTree(b, r.children, childPrefix, g)
	}
}
