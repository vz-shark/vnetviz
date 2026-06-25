package render

import (
	"fmt"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// Mermaid renders the topology as a Mermaid flowchart.
func Mermaid(t *model.Topology, opts Options) string {
	edges := t.Edges()
	if opts.CollapseVeth {
		edges = collapsedEdges(t)
	}
	nodes := visibleNodes(t, edges, opts.CollapseVeth)

	var b strings.Builder
	// Top-down flow: the host/container chain runs vertically while sibling
	// interfaces spread horizontally.
	b.WriteString("graph TD\n")

	for _, ns := range t.Namespaces {
		ifaces := nodes[ns]
		if len(ifaces) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  subgraph %s[%q]\n", clusterID(ns), nsTitle(ns))
		for _, i := range ifaces {
			fmt.Fprintf(&b, "    %s[%q]\n", model.NodeID(i), joinBR(labelLines(i, true)))
		}
		// Published ports listen on the host, so their dummy nodes live in the
		// host cluster. The forward destination is baked into the label, so no
		// edge is drawn across the diagram.
		if ns.Kind == model.KindHost {
			for _, c := range t.Namespaces {
				for _, p := range c.Ports {
					fmt.Fprintf(&b, "    %s([%q])\n", pubNodeID(c, p.HostPort), pubForwardLabel(c, p))
				}
			}
		}
		b.WriteString("  end\n")
	}

	for _, e := range edges {
		switch e.Kind {
		case model.EdgeVeth:
			fmt.Fprintf(&b, "  %s -. veth .- %s\n", model.NodeID(e.A), model.NodeID(e.B))
		case model.EdgeVlan:
			fmt.Fprintf(&b, "  %s -.->|%s| %s\n", model.NodeID(e.A), vlanTag(e.A), model.NodeID(e.B))
		default: // EdgeMaster / collapsed
			fmt.Fprintf(&b, "  %s --- %s\n", model.NodeID(e.A), model.NodeID(e.B))
		}
	}

	// No custom node styling: rely on the viewer's Mermaid theme so the diagram
	// stays readable in both light and dark mode. The up/down state is already
	// shown in each node's label text.
	return b.String()
}

func clusterID(ns *model.Namespace) string {
	return "ns_" + sanitizeID(ns.Inode)
}

func sanitizeID(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
