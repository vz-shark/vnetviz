// Package model defines the internal graph representation that sits between
// the raw Linux network state and the Mermaid / Graphviz renderers.
//
//	Linux Network  ->  model.Topology  ->  Mermaid / DOT
package model

import "fmt"

// LinkType is a coarse classification of a network interface.
type LinkType string

const (
	TypeLoopback LinkType = "loopback"
	TypeVeth     LinkType = "veth"
	TypeBridge   LinkType = "bridge"
	TypeBond     LinkType = "bond"
	TypeVlan     LinkType = "vlan"
	TypePhysical LinkType = "physical"
	TypeTun      LinkType = "tun"
	TypeOther    LinkType = "other"
)

// IsMaster reports whether interfaces can be enslaved to this type (bridge or
// bond), i.e. it acts as a container for member interfaces.
func (lt LinkType) IsMaster() bool {
	return lt == TypeBridge || lt == TypeBond
}

// NSKind describes where a namespace came from.
type NSKind string

const (
	KindHost   NSKind = "host"
	KindNamed  NSKind = "named"
	KindDocker NSKind = "docker"
	KindPodman NSKind = "podman"
)

// Iface is a single network interface inside a namespace.
type Iface struct {
	Name string
	// Index is the ifindex within the owning namespace.
	Index int
	// PeerIndex is the iflink / parent ifindex. For a veth it points at the
	// ifindex of its peer (which may live in another namespace).
	PeerIndex int
	// MasterIndex is the ifindex of the bridge this interface is enslaved to
	// (0 when it has no master). Always within the same namespace.
	MasterIndex int
	MAC         string
	Type        LinkType
	State       string
	// Up reports whether the interface is operationally up (administratively up
	// and not in a down / lower-layer-down operational state).
	Up    bool
	Addrs []string
	// VlanID is the 802.1Q tag for a VLAN interface (0 otherwise). For a VLAN,
	// PeerIndex carries the ifindex of the parent (lower) device.
	VlanID int
	// Network is the friendly Docker/Podman network name for a bridge that backs
	// one (e.g. "demo_frontend" for the kernel bridge "br-d896f0a9fbe7"). Empty
	// for interfaces that are not container-network bridges.
	Network string

	NS *Namespace // back-pointer to the owning namespace
}

// PortMap is a published container port: a host endpoint forwarded to a port
// inside the container (the `docker -p host:container` mapping, realized as an
// iptables DNAT rather than a network interface).
type PortMap struct {
	HostIP        string // host bind address, e.g. "0.0.0.0" ("" when unknown)
	HostPort      string // host-side port, e.g. "8080"
	ContainerPort string // container-side port including protocol, e.g. "80/tcp"
}

// Namespace groups the interfaces that share one network namespace.
type Namespace struct {
	// Name is a human friendly label: "host", a named netns, or a container
	// name.
	Name  string
	Kind  NSKind
	Inode string // unique id of the namespace (dev:ino)
	// Ports holds the container's published (host-forwarded) ports, if any.
	Ports  []PortMap
	Ifaces []*Iface
}

// Topology is the whole captured network state.
type Topology struct {
	Namespaces []*Namespace
}

// ifaceKey uniquely identifies an interface across the whole topology.
type ifaceKey struct {
	ns    string
	index int
}

func keyOf(i *Iface) ifaceKey { return ifaceKey{ns: i.NS.Inode, index: i.Index} }

// EdgeKind distinguishes how two interfaces are connected.
type EdgeKind int

const (
	// EdgeVeth links the two ends of a veth pair.
	EdgeVeth EdgeKind = iota
	// EdgeMaster links an interface to the master it is enslaved to
	// (a bridge or a bond).
	EdgeMaster
	// EdgeVlan links a VLAN interface to its parent (lower) device.
	EdgeVlan
)

// Edge is a rendered connection between two interfaces.
type Edge struct {
	A, B *Iface
	Kind EdgeKind
}

// NodeID returns a stable identifier for an interface, unique across the whole
// topology, safe to use as a Mermaid / DOT node id.
func NodeID(i *Iface) string {
	return fmt.Sprintf("n_%s_%d", sanitize(i.NS.Inode), i.Index)
}

func sanitize(s string) string {
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

// Edges derives the connections between interfaces from the captured state.
//
//   - master membership: an interface with a MasterIndex gets an EdgeMaster to
//     the bridge or bond it is enslaved to, in the same namespace.
//   - veth pairs: two interfaces (a, b) form a pair when a.Index == b.PeerIndex
//     and b.Index == a.PeerIndex. This holds even when the peers live in
//     different namespaces.
//   - VLAN: a VLAN interface gets an EdgeVlan to its parent (lower) device,
//     which is always in the same namespace.
func (t *Topology) Edges() []Edge {
	// Index every interface by namespace+ifindex.
	byKey := map[ifaceKey]*Iface{}
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			byKey[keyOf(i)] = i
		}
	}

	var edges []Edge

	// Master membership: enslaved to a bridge or bond (same namespace lookup).
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			if i.MasterIndex == 0 {
				continue
			}
			if m, ok := byKey[ifaceKey{ns: ns.Inode, index: i.MasterIndex}]; ok {
				edges = append(edges, Edge{A: i, B: m, Kind: EdgeMaster})
			}
		}
	}

	// VLAN to parent device (same namespace lookup).
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			if i.Type != TypeVlan || i.PeerIndex == 0 {
				continue
			}
			if p, ok := byKey[ifaceKey{ns: ns.Inode, index: i.PeerIndex}]; ok {
				edges = append(edges, Edge{A: i, B: p, Kind: EdgeVlan})
			}
		}
	}

	// veth pairs, de-duplicated so each pair is emitted once.
	seen := map[[2]ifaceKey]bool{}
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			if i.Type != TypeVeth || i.PeerIndex == 0 {
				continue
			}
			peer := findPeer(byKey, i)
			if peer == nil {
				continue
			}
			ka, kb := keyOf(i), keyOf(peer)
			pair := orderedPair(ka, kb)
			if seen[pair] {
				continue
			}
			seen[pair] = true
			edges = append(edges, Edge{A: i, B: peer, Kind: EdgeVeth})
		}
	}

	return edges
}

// findPeer locates the other end of a veth pair. The peer has Index ==
// i.PeerIndex and points back with PeerIndex == i.Index. It may be in any
// namespace, so we scan all candidates with the matching index.
func findPeer(byKey map[ifaceKey]*Iface, i *Iface) *Iface {
	for k, cand := range byKey {
		if k.index != i.PeerIndex {
			continue
		}
		if cand.PeerIndex == i.Index && cand != i {
			return cand
		}
	}
	return nil
}

func orderedPair(a, b ifaceKey) [2]ifaceKey {
	if a.ns < b.ns || (a.ns == b.ns && a.index <= b.index) {
		return [2]ifaceKey{a, b}
	}
	return [2]ifaceKey{b, a}
}
