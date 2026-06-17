// Package render turns a model.Topology into an ASCII tree, Mermaid, Graphviz
// (DOT), or—via Graphviz—an SVG/PNG image.
package render

import (
	"fmt"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// Options tweak how the diagram is rendered.
type Options struct {
	// CollapseVeth hides veth interface nodes and connects whatever they were
	// attached to (typically a bridge) straight to the peer's namespace.
	CollapseVeth bool
	// Color enables ANSI coloring of the text output: the "down" token is
	// highlighted so it stands out. It only affects the text renderer and should
	// be set only when the destination is a terminal.
	Color bool
	// Charset selects the line-drawing characters used by the text tree:
	// Unicode box-drawing (the default) or plain ASCII like `tree --charset=ascii`.
	Charset Charset
}

// Charset selects the glyphs used to draw the text tree.
type Charset int

const (
	// CharsetUnicode uses Unicode box-drawing characters.
	CharsetUnicode Charset = iota
	// CharsetASCII uses only ASCII characters (no full-width glyphs), matching
	// `tree --charset=ascii`.
	CharsetASCII
)

// stateWord is the up/down token shown for an interface.
func stateWord(i *model.Iface) string {
	if i.Up {
		return "up"
	}
	return "down"
}

// labelLines returns the human readable lines describing an interface: its
// name, type, and (if present) addresses. When showState is true a trailing
// up/down line is appended; renderers that signal state by other means (e.g.
// graying out down nodes) pass false so the word is not duplicated.
func labelLines(i *model.Iface, showState bool) []string {
	lines := []string{i.Name}
	switch {
	case i.Type == model.TypeVeth:
		// no type line; the veth link conveys it
	case i.Type == model.TypeVlan && i.VlanID > 0:
		lines = append(lines, fmt.Sprintf("vlan %d", i.VlanID))
	default:
		lines = append(lines, string(i.Type))
	}
	if i.Network != "" {
		lines = append(lines, "("+i.Network+")")
	}
	lines = append(lines, i.Addrs...)
	if showState {
		lines = append(lines, stateWord(i))
	}
	return lines
}

// vlanTag is the short edge label for a VLAN-to-parent link, e.g. "vlan 100".
func vlanTag(i *model.Iface) string {
	if i.VlanID > 0 {
		return fmt.Sprintf("vlan %d", i.VlanID)
	}
	return "vlan"
}

// pubNodeID is the graph id of the dummy node representing a published host
// port for a container; it mirrors model.NodeID's "type_inode_suffix" shape.
func pubNodeID(c *model.Namespace, hostPort string) string {
	return "pub_" + sanitizeID(c.Inode) + "_" + sanitizeID(hostPort)
}

// pubForwardLabel describes a published port as a self-contained forward, e.g.
// "host:8080  ==>  web:80/tcp". Keeping the destination in the label lets the
// dummy node stand alone, so no edge has to be drawn across the diagram.
func pubForwardLabel(c *model.Namespace, p model.PortMap) string {
	return "host:" + p.HostPort + "  ==>  " + c.Name + ":" + p.ContainerPort
}

// nsTitle is the heading shown for a namespace cluster.
func nsTitle(ns *model.Namespace) string {
	switch ns.Kind {
	case model.KindHost:
		return "host"
	case model.KindDocker:
		return ns.Name + " (docker)"
	case model.KindPodman:
		return ns.Name + " (podman)"
	default:
		return ns.Name + " (netns)"
	}
}

// collapsedEdges rewrites the topology's edges so that veth pairs disappear and
// the things they joined are connected directly.
//
// For each veth pair we pick an "anchor" for each side: the bridge it is
// enslaved to, or, failing that, the veth interface itself. We then connect the
// two anchors. Bridge-membership edges that only involve veths are dropped
// because the membership is now expressed by the collapsed edge.
func collapsedEdges(t *model.Topology) []model.Edge {
	master := map[*model.Iface]*model.Iface{} // iface -> its bridge/bond
	for _, e := range t.Edges() {
		if e.Kind == model.EdgeMaster {
			master[e.A] = e.B
		}
	}

	var out []model.Edge
	for _, e := range t.Edges() {
		switch e.Kind {
		case model.EdgeMaster:
			// Keep master edges for non-veth members (e.g. a physical NIC).
			if e.A.Type != model.TypeVeth {
				out = append(out, e)
			}
		case model.EdgeVeth:
			a := anchor(e.A, master)
			b := anchor(e.B, master)
			if a == b {
				continue
			}
			out = append(out, model.Edge{A: a, B: b, Kind: model.EdgeMaster})
		default:
			// VLAN (and any future) edges are unaffected by veth collapsing.
			out = append(out, e)
		}
	}
	return out
}

func anchor(i *model.Iface, master map[*model.Iface]*model.Iface) *model.Iface {
	if m, ok := master[i]; ok {
		return m
	}
	return i
}

// visibleNodes returns, per namespace, the interfaces that should be drawn for
// the given edge set (any interface that appears in an edge, plus every
// non-veth interface so isolated bridges / NICs still show up).
func visibleNodes(t *model.Topology, edges []model.Edge, collapse bool) map[*model.Namespace][]*model.Iface {
	keep := map[*model.Iface]bool{}
	for _, e := range edges {
		keep[e.A] = true
		keep[e.B] = true
	}
	result := map[*model.Namespace][]*model.Iface{}
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			if collapse && i.Type == model.TypeVeth && !keep[i] {
				continue
			}
			if !collapse || keep[i] || i.Type != model.TypeVeth {
				result[ns] = append(result[ns], i)
			}
		}
	}
	return result
}

// joinBR joins label lines for Mermaid, which uses <br/> for line breaks.
func joinBR(lines []string) string { return strings.Join(lines, "<br/>") }

// dotLabel returns a quoted DOT label string. Each line is escaped for DOT and
// joined with the literal "\n" newline escape (a single backslash + n), which
// Graphviz renders as a line break. We quote by hand rather than with %q so
// that the "\n" separators are not themselves re-escaped into "\\n".
func dotLabel(lines []string) string {
	escaped := make([]string, len(lines))
	for i, l := range lines {
		l = strings.ReplaceAll(l, `\`, `\\`)
		l = strings.ReplaceAll(l, `"`, `\"`)
		escaped[i] = l
	}
	return `"` + strings.Join(escaped, `\n`) + `"`
}
