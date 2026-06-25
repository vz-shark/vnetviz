package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vz-shark/vnetviz/internal/model"
)

func sample() *model.Topology {
	host := &model.Namespace{Name: "host", Kind: model.KindHost, Inode: "h"}
	br := &model.Iface{Name: "docker0", Index: 2, Type: model.TypeBridge, NS: host, Up: true}
	veth := &model.Iface{Name: "veth9", Index: 7, PeerIndex: 3, MasterIndex: 2, Type: model.TypeVeth, NS: host, Up: true}
	host.Ifaces = []*model.Iface{br, veth}

	web := &model.Namespace{Name: "web", Kind: model.KindDocker, Inode: "c"}
	eth0 := &model.Iface{Name: "eth0", Index: 3, PeerIndex: 7, Type: model.TypeVeth, NS: web, Up: true,
		Addrs: []string{"172.17.0.2/16"}}
	web.Ifaces = []*model.Iface{eth0}

	return &model.Topology{Namespaces: []*model.Namespace{host, web}}
}

func TestMermaidContainsClustersAndEdges(t *testing.T) {
	out := Mermaid(sample(), Options{})
	for _, want := range []string{"graph TD", "\"host\"", "\"web (docker)\"", "docker0", "172.17.0.2/16", "veth"} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q\n%s", want, out)
		}
	}
}

func TestDOTContainsClustersAndEdges(t *testing.T) {
	out := DOT(sample(), Options{})
	for _, want := range []string{"digraph vnetviz", "cluster_0", "label=\"host\"", "style=dashed"} {
		if !strings.Contains(out, want) {
			t.Errorf("dot output missing %q\n%s", want, out)
		}
	}
	// Multi-line labels must use a single-backslash \n (DOT newline), never a
	// double-escaped \\n which Graphviz would print literally.
	if !strings.Contains(out, `eth0\n172.17.0.2/16`) {
		t.Errorf("dot label should join lines with a single \\n\n%s", out)
	}
	if strings.Contains(out, `\\n`) {
		t.Errorf("dot output must not contain double-escaped \\\\n\n%s", out)
	}
}

// sampleVlanBond builds a host with a bond (bond0 enslaving eth0/eth1), a VLAN
// on the bond (bond0.100), and a VLAN on a plain NIC (eth2.20).
func sampleVlanBond() *model.Topology {
	host := &model.Namespace{Name: "host", Kind: model.KindHost, Inode: "h"}
	bond := &model.Iface{Name: "bond0", Index: 10, Type: model.TypeBond, NS: host, Up: true}
	e0 := &model.Iface{Name: "eth0", Index: 2, MasterIndex: 10, Type: model.TypePhysical, NS: host, Up: true}
	e1 := &model.Iface{Name: "eth1", Index: 3, MasterIndex: 10, Type: model.TypePhysical, NS: host, Up: true}
	bvlan := &model.Iface{Name: "bond0.100", Index: 11, PeerIndex: 10, Type: model.TypeVlan, VlanID: 100, NS: host, Up: true, Addrs: []string{"10.0.100.1/24"}}
	e2 := &model.Iface{Name: "eth2", Index: 5, Type: model.TypePhysical, NS: host, Up: true}
	e2vlan := &model.Iface{Name: "eth2.20", Index: 12, PeerIndex: 5, Type: model.TypeVlan, VlanID: 20, NS: host, Up: true, Addrs: []string{"192.168.20.1/24"}}
	host.Ifaces = []*model.Iface{bond, e0, e1, bvlan, e2, e2vlan}
	return &model.Topology{Namespaces: []*model.Namespace{host}}
}

func TestTextBondAndVlan(t *testing.T) {
	out := Text(sampleVlanBond(), Options{})
	for _, want := range []string{
		"bond0  [bond]",            // bond shown as a master container
		"├─ eth0  [physical]",      // its slaves nested under it
		"[vlan 100 @bond0]",        // VLAN tagged with id and parent
		"eth2.20  [vlan 20 @eth2]", // VLAN on a plain NIC
		"10.0.100.1/24",            // VLAN address surfaced
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
}

func TestDOTVlanEdge(t *testing.T) {
	out := DOT(sampleVlanBond(), Options{})
	if !strings.Contains(out, `label="vlan 100"`) {
		t.Errorf("dot output missing vlan edge label\n%s", out)
	}
	if !strings.Contains(out, "fillcolor=\"#cde4ff\"") {
		t.Errorf("dot output should highlight the bond master\n%s", out)
	}
}

func TestMermaidVlanEdge(t *testing.T) {
	out := Mermaid(sampleVlanBond(), Options{})
	if !strings.Contains(out, "|vlan 100|") {
		t.Errorf("mermaid output missing vlan edge label\n%s", out)
	}
}

func TestTextTreeShowsBridgeMembersAndPeer(t *testing.T) {
	out := Text(sample(), Options{})
	for _, want := range []string{
		"host",
		"docker0  [bridge]",
		"web (docker)",
		"==( veth )==",  // veth connection annotation
		"@web (docker)", // peer namespace label on the connection
		"172.17.0.2/16", // peer address surfaced on the line
		"└─ ",           // tree branch connector
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
}

// downSample is a host with one up NIC and one down NIC.
func downSample() (*model.Topology, *model.Iface) {
	host := &model.Namespace{Name: "host", Kind: model.KindHost, Inode: "h"}
	up := &model.Iface{Name: "eth0", Index: 2, Type: model.TypePhysical, NS: host, Up: true}
	down := &model.Iface{Name: "eth1", Index: 3, Type: model.TypePhysical, NS: host, Up: false}
	host.Ifaces = []*model.Iface{up, down}
	return &model.Topology{Namespaces: []*model.Namespace{host}}, down
}

func TestStateShownAndDownGrayed(t *testing.T) {
	top, down := downSample()

	t.Run("text shows up/down tokens", func(t *testing.T) {
		out := Text(top, Options{})
		if !strings.Contains(out, "eth0  [physical]  up") {
			t.Errorf("up interface should be tagged up\n%s", out)
		}
		if !strings.Contains(out, "eth1  [physical]  down") {
			t.Errorf("down interface should be tagged down\n%s", out)
		}
		if strings.Contains(out, "\033[") {
			t.Errorf("text must not contain ANSI escapes\n%q", out)
		}
	})

	t.Run("text grays the whole down row when Color is set", func(t *testing.T) {
		out := Text(top, Options{Color: true})
		if !strings.Contains(out, "\033[38;5;245meth1  [physical]  down\033[0m") {
			t.Errorf("down row should be wrapped in gray ANSI\n%q", out)
		}
		// The up row stays plain.
		if strings.Contains(out, "\033[38;5;245meth0") {
			t.Errorf("up row should not be colored\n%q", out)
		}
	})

	t.Run("dot grays the down node without an up/down label", func(t *testing.T) {
		out := DOT(top, Options{})
		if !strings.Contains(out, fmt.Sprintf("%s [label=\"eth1\\nphysical\", color=\"#aaaaaa\", fontcolor=\"#aaaaaa\"]", model.NodeID(down))) {
			t.Errorf("down node should carry gray color attrs and no state word\n%s", out)
		}
		if strings.Contains(out, "\\ndown") || strings.Contains(out, "\\nup") {
			t.Errorf("dot labels should not include the up/down word\n%s", out)
		}
	})

	t.Run("mermaid does not gray the down node", func(t *testing.T) {
		out := Mermaid(top, Options{})
		if strings.Contains(out, "style ") {
			t.Errorf("mermaid should emit no custom style lines\n%s", out)
		}
		// State is still conveyed in the label text.
		if !strings.Contains(out, "down") {
			t.Errorf("down node label should still show the down token\n%s", out)
		}
	})
}

func TestTextCharset(t *testing.T) {
	t.Run("unicode is the default", func(t *testing.T) {
		// sampleVlanBond has a bond with two slaves, so a tee, a continuation,
		// and a last-branch connector all appear.
		out := Text(sampleVlanBond(), Options{})
		for _, want := range []string{"├─ ", "└─ ", "│　"} {
			if !strings.Contains(out, want) {
				t.Errorf("default charset should use Unicode connector %q\n%s", want, out)
			}
		}
	})

	t.Run("ascii uses only ASCII connectors like tree --charset=ascii", func(t *testing.T) {
		out := Text(sampleVlanBond(), Options{Charset: CharsetASCII})
		for _, want := range []string{"|-- ", "`-- ", "|   "} {
			if !strings.Contains(out, want) {
				t.Errorf("ascii charset missing %q\n%s", want, out)
			}
		}
		// No Unicode box-drawing or full-width glyphs in ASCII mode.
		for _, bad := range []string{"├", "└", "│", "─", "　"} {
			if strings.Contains(out, bad) {
				t.Errorf("ascii charset must not contain %q\n%s", bad, out)
			}
		}
	})
}

func TestCollapseVethRemovesVethNodes(t *testing.T) {
	out := Mermaid(sample(), Options{CollapseVeth: true})
	if strings.Contains(out, "veth9") {
		t.Errorf("collapsed output should not contain veth node veth9\n%s", out)
	}
	// docker0 should now connect directly to the container's eth0.
	if !strings.Contains(out, "docker0") || !strings.Contains(out, "eth0") {
		t.Errorf("collapsed output should keep docker0 and eth0\n%s", out)
	}
	if strings.Count(out, "---") == 0 {
		t.Errorf("collapsed output should contain a direct link\n%s", out)
	}
}

func TestPublishedPortsDummyNode(t *testing.T) {
	top := sample()
	web := top.Namespaces[1] // the "web" container
	web.Ports = []model.PortMap{{HostIP: "0.0.0.0", HostPort: "8080", ContainerPort: "80/tcp"}}
	target := model.NodeID(web.Ifaces[0]) // eth0, the primary interface

	t.Run("text row under host", func(t *testing.T) {
		got := Text(top, Options{})
		// The dummy node lives under the host and names the destination container.
		if !strings.Contains(got, "host:8080  ==>  web:80/tcp") {
			t.Errorf("host should carry a forward row naming the container\n%s", got)
		}
		// The row must sit in the host block, i.e. before the "web (docker)"
		// section header (matched as a line, not the "@web (docker)" peer note).
		if strings.Index(got, "host:8080") > strings.Index(got, "\nweb (docker)\n") {
			t.Errorf("forward row should appear in the host block, not the container\n%s", got)
		}
	})
	t.Run("mermaid self-contained node, no edge", func(t *testing.T) {
		got := Mermaid(top, Options{})
		if !strings.Contains(got, `(["host:8080  ==>  web:80/tcp"])`) {
			t.Errorf("mermaid should emit a self-contained dummy port node\n%s", got)
		}
		if strings.Contains(got, ".-> "+target) {
			t.Errorf("mermaid should not draw a forward edge to eth0\n%s", got)
		}
	})
	t.Run("dot self-contained node, no edge", func(t *testing.T) {
		got := DOT(top, Options{})
		if !strings.Contains(got, `[shape=note, label="host:8080  ==>  web:80/tcp"]`) {
			t.Errorf("dot should emit a self-contained dummy port node\n%s", got)
		}
		if strings.Contains(got, "dir=forward") {
			t.Errorf("dot should not draw a forward edge\n%s", got)
		}
	})
}

func TestBridgeShowsDockerNetworkName(t *testing.T) {
	top := sample()
	top.Namespaces[0].Ifaces[0].Network = "demo_frontend" // docker0 -> friendly name

	t.Run("text", func(t *testing.T) {
		if got := Text(top, Options{}); !strings.Contains(got, "docker0  [bridge]  (demo_frontend)") {
			t.Errorf("text bridge row should carry the network name\n%s", got)
		}
	})
	t.Run("mermaid", func(t *testing.T) {
		if got := Mermaid(top, Options{}); !strings.Contains(got, "(demo_frontend)") {
			t.Errorf("mermaid bridge node should carry the network name\n%s", got)
		}
	})
}

func TestTextCollapseNamesBridgeFromContainerSide(t *testing.T) {
	out := Text(sample(), Options{CollapseVeth: true})
	// From the container's eth0 the collapsed row should point at the bridge its
	// host-side peer is enslaved to (docker0), not the host-side veth, and carry
	// the container's own address.
	want := "eth0  ==( veth )==  docker0  @host  172.17.0.2/16  up"
	if !strings.Contains(out, want) {
		t.Errorf("collapsed container row should name the bridge: want %q\n%s", want, out)
	}
	if strings.Contains(out, "veth9  @") {
		t.Errorf("collapsed container row should not surface the host veth name\n%s", out)
	}
}
