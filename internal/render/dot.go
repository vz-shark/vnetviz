package render

import (
	"fmt"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// DOT renders the topology as Graphviz DOT.
func DOT(t *model.Topology, opts Options) string {
	edges := t.Edges()
	if opts.CollapseVeth {
		edges = collapsedEdges(t)
	}
	nodes := visibleNodes(t, edges, opts.CollapseVeth)

	var b strings.Builder
	b.WriteString("digraph vnetviz {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box, style=rounded, fontname=\"sans-serif\"];\n")
	b.WriteString("  edge [dir=none];\n")

	for idx, ns := range t.Namespaces {
		ifaces := nodes[ns]
		if len(ifaces) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  subgraph cluster_%d {\n", idx)
		fmt.Fprintf(&b, "    label=%q;\n", nsTitle(ns))
		b.WriteString("    style=rounded; color=\"#999999\";\n")
		for _, i := range ifaces {
			// State is conveyed by graying down nodes, so the label omits the
			// up/down word.
			attrs := "label=" + dotLabel(labelLines(i, false))
			if i.Type.IsMaster() {
				attrs += ", style=\"rounded,filled\", fillcolor=\"#cde4ff\""
			}
			// Gray out interfaces that are down.
			if !i.Up {
				attrs += ", color=\"#aaaaaa\", fontcolor=\"#aaaaaa\""
			}
			fmt.Fprintf(&b, "    %s [%s];\n", model.NodeID(i), attrs)
		}
		// Published ports listen on the host, so their dummy nodes live in the
		// host cluster. The forward destination is baked into the label, so no
		// edge is drawn across the diagram.
		if ns.Kind == model.KindHost {
			for _, c := range t.Namespaces {
				for _, p := range c.Ports {
					fmt.Fprintf(&b, "    %s [shape=note, label=%q];\n", pubNodeID(c, p.HostPort), pubForwardLabel(c, p))
				}
			}
		}
		b.WriteString("  }\n")
	}

	for _, e := range edges {
		switch e.Kind {
		case model.EdgeVeth:
			fmt.Fprintf(&b, "  %s -> %s [style=dashed, label=\"veth\"];\n",
				model.NodeID(e.A), model.NodeID(e.B))
		case model.EdgeVlan:
			fmt.Fprintf(&b, "  %s -> %s [style=dotted, label=%q];\n",
				model.NodeID(e.A), model.NodeID(e.B), vlanTag(e.A))
		default:
			fmt.Fprintf(&b, "  %s -> %s;\n", model.NodeID(e.A), model.NodeID(e.B))
		}
	}

	b.WriteString("}\n")
	return b.String()
}
