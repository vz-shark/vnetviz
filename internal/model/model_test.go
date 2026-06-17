package model

import "testing"

// buildSample creates a host namespace with a bridge (docker0) and a veth whose
// peer (eth0) lives in a container namespace, mirroring a typical Docker setup:
//
//	host:  docker0(idx2) <- veth9(idx7, peer 3, master 2)
//	web:   eth0(idx3, peer 7)
func buildSample() *Topology {
	host := &Namespace{Name: "host", Kind: KindHost, Inode: "h"}
	br := &Iface{Name: "docker0", Index: 2, Type: TypeBridge, NS: host}
	veth := &Iface{Name: "veth9", Index: 7, PeerIndex: 3, MasterIndex: 2, Type: TypeVeth, NS: host}
	host.Ifaces = []*Iface{br, veth}

	web := &Namespace{Name: "web", Kind: KindDocker, Inode: "c"}
	eth0 := &Iface{Name: "eth0", Index: 3, PeerIndex: 7, Type: TypeVeth, NS: web,
		Addrs: []string{"172.17.0.2/16"}}
	web.Ifaces = []*Iface{eth0}

	return &Topology{Namespaces: []*Namespace{host, web}}
}

func TestEdges(t *testing.T) {
	top := buildSample()
	edges := top.Edges()

	var bridge, veth int
	for _, e := range edges {
		switch e.Kind {
		case EdgeMaster:
			bridge++
		case EdgeVeth:
			veth++
		}
	}
	if bridge != 1 {
		t.Errorf("want 1 bridge edge, got %d", bridge)
	}
	if veth != 1 {
		t.Errorf("want 1 veth edge (cross-namespace pair), got %d", veth)
	}
}

func TestVethPairNotDuplicated(t *testing.T) {
	// Both ends are veths; the pair must be emitted exactly once.
	top := buildSample()
	count := 0
	for _, e := range top.Edges() {
		if e.Kind == EdgeVeth {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("veth pair emitted %d times, want 1", count)
	}
}

func TestNodeIDStableAndUnique(t *testing.T) {
	top := buildSample()
	seen := map[string]bool{}
	for _, ns := range top.Namespaces {
		for _, i := range ns.Ifaces {
			id := NodeID(i)
			if seen[id] {
				t.Errorf("duplicate node id %q", id)
			}
			seen[id] = true
			if NodeID(i) != id {
				t.Errorf("node id not stable for %s", i.Name)
			}
		}
	}
}
