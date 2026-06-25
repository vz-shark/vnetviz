// Package collect reads the live Linux network state (interfaces, addresses,
// namespaces, containers) and turns it into a model.Topology.
package collect

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"github.com/vz-shark/vnetviz/internal/model"
)

// Options controls what gets collected.
type Options struct {
	Virtual  bool // include virtual devices (veth, bridges, bonds, VLANs, ...)
	Loopback bool // include the loopback interface
	IP       bool // collect IP addresses
	Physical bool // include physical NICs
	Upped    bool // include interfaces that are operationally up
	Downed   bool // include interfaces that are operationally down

	// Warnf, when set, receives non-fatal warnings (e.g. a namespace that
	// could not be entered because we are not root).
	Warnf func(format string, args ...any)
}

func (o Options) warn(format string, args ...any) {
	if o.Warnf != nil {
		o.Warnf(format, args...)
	}
}

// nsTarget is a namespace we want to walk.
type nsTarget struct {
	name  string
	kind  model.NSKind
	path  string // file path to the netns; empty means "current/host"
	ports []model.PortMap
}

// Topology captures the whole reachable network state. The returned slice lists
// the names of namespaces that were discovered but could not be entered for lack
// of privileges; a non-empty slice is the caller's cue to suggest re-running
// under sudo.
func Topology(opts Options) (*model.Topology, []string, error) {
	targets, needRoot, err := discoverNamespaces(opts)
	if err != nil {
		return nil, nil, err
	}

	top := &model.Topology{}
	seenInode := map[string]bool{}

	for _, t := range targets {
		ns, err := collectNamespace(t, opts)
		if err != nil {
			// A permission error means the namespace exists but we lack the
			// privileges to enter it: record it (the caller reports these as a
			// single "needs root" error) rather than warning per namespace.
			if errors.Is(err, os.ErrPermission) {
				needRoot = append(needRoot, t.name)
			} else {
				opts.warn("namespace %q: %v", t.name, err)
			}
			continue
		}
		if ns == nil {
			continue
		}
		// Containers can share a netns (e.g. pods); only emit each once.
		if seenInode[ns.Inode] {
			continue
		}
		seenInode[ns.Inode] = true
		top.Namespaces = append(top.Namespaces, ns)
	}

	// Label bridges that back a Docker/Podman network with their friendly name.
	if nets, err := dockerNetworkBridges(); err != nil {
		opts.warn("docker networks: %v", err)
	} else {
		annotateNetworks(top, nets)
	}
	if nets, err := podmanNetworkBridges(); err != nil {
		opts.warn("podman networks: %v", err)
	} else {
		annotateNetworks(top, nets)
	}

	return top, needRoot, nil
}

// annotateNetworks tags each interface whose name matches a known container
// network bridge with that network's friendly name.
func annotateNetworks(t *model.Topology, nets map[string]string) {
	for _, ns := range t.Namespaces {
		for _, i := range ns.Ifaces {
			if name, ok := nets[i.Name]; ok {
				i.Network = name
			}
		}
	}
}

// discoverNamespaces builds the ordered list of namespaces to inspect: the host
// first, then named namespaces, then container namespaces. The second result
// names container engines that are installed but need root to read.
func discoverNamespaces(opts Options) ([]nsTarget, []string, error) {
	targets := []nsTarget{{name: "host", kind: model.KindHost}}

	// Named namespaces created with `ip netns add`. On most systems /var/run is
	// a symlink to /run, so both paths resolve to the same directory; dedup by
	// the resolved path to avoid listing every netns twice.
	seenDir := map[string]bool{}
	for _, dir := range []string{"/var/run/netns", "/run/netns"} {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			continue
		}
		if seenDir[real] {
			continue
		}
		seenDir[real] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			targets = append(targets, nsTarget{
				name: e.Name(),
				kind: model.KindNamed,
				path: filepath.Join(dir, e.Name()),
			})
		}
	}

	// Container engines are always probed (like the kernel-native veth/vlan/netns
	// data). A missing engine is skipped silently; one that is installed but
	// unreadable without root is reported via needRoot so the caller can hint at
	// sudo, exactly like a netns we could not enter.
	var needRoot []string
	if cs, root := dockerContainers(); root {
		needRoot = append(needRoot, "docker")
	} else {
		targets = append(targets, cs...)
	}
	if cs, root := podmanContainers(); root {
		needRoot = append(needRoot, "podman")
	} else {
		targets = append(targets, cs...)
	}

	return targets, needRoot, nil
}

// collectNamespace enters the target namespace (if needed) and reads its
// interfaces.
func collectNamespace(t nsTarget, opts Options) (*model.Namespace, error) {
	ns := &model.Namespace{Name: t.name, Kind: t.kind, Ports: t.ports}

	run := func() error {
		// Record the namespace's unique id from the current thread.
		if h, err := netns.Get(); err == nil {
			ns.Inode = h.UniqueId()
			h.Close()
		}
		ifaces, err := collectLinks(opts)
		if err != nil {
			return err
		}
		for _, i := range ifaces {
			i.NS = ns
		}
		ns.Ifaces = ifaces
		return nil
	}

	if t.path == "" {
		// Host / current namespace: no switch required.
		if err := run(); err != nil {
			return nil, err
		}
		return ns, nil
	}

	if err := inNamespace(t.path, run); err != nil {
		return nil, err
	}
	return ns, nil
}

// inNamespace runs fn with the calling goroutine pinned to the target network
// namespace, restoring the original namespace afterwards.
func inNamespace(path string, fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origin, err := netns.Get()
	if err != nil {
		return fmt.Errorf("get current netns: %w", err)
	}
	defer origin.Close()

	target, err := netns.GetFromPath(path)
	if err != nil {
		return fmt.Errorf("open netns: %w", err)
	}
	defer target.Close()

	if err := netns.Set(target); err != nil {
		return fmt.Errorf("enter netns (need root?): %w", err)
	}
	defer netns.Set(origin) //nolint:errcheck // best effort restore

	return fn()
}

// collectLinks lists the interfaces in the *current* namespace.
func collectLinks(opts Options) ([]*model.Iface, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}

	var out []*model.Iface
	for _, l := range links {
		attrs := l.Attrs()
		typ := classify(l)

		// Scope filter: each interface belongs to exactly one of loopback,
		// physical, or "virtual" (everything else — veth, bridges, bonds,
		// VLANs, tun, ...), gated by its own toggle.
		switch typ {
		case model.TypeLoopback:
			if !opts.Loopback {
				continue
			}
		case model.TypePhysical:
			if !opts.Physical {
				continue
			}
		default:
			if !opts.Virtual {
				continue
			}
		}

		// State filter: up and down interfaces are gated independently.
		up := linkUp(attrs)
		if up && !opts.Upped {
			continue
		}
		if !up && !opts.Downed {
			continue
		}

		iface := &model.Iface{
			Name:        attrs.Name,
			Index:       attrs.Index,
			PeerIndex:   attrs.ParentIndex,
			MasterIndex: attrs.MasterIndex,
			Type:        typ,
			State:       attrs.OperState.String(),
			Up:          up,
		}
		if len(attrs.HardwareAddr) > 0 {
			iface.MAC = attrs.HardwareAddr.String()
		}
		if v, ok := l.(*netlink.Vlan); ok {
			iface.VlanID = v.VlanId
		}
		if opts.IP {
			iface.Addrs = collectAddrs(l)
		}
		out = append(out, iface)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

func collectAddrs(l netlink.Link) []string {
	addrs, err := netlink.AddrList(l, netlink.FAMILY_ALL)
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		if a.IP == nil {
			continue
		}
		// Skip IPv6 link-local noise; it adds little to a topology view.
		if a.IP.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, a.IPNet.String())
	}
	return out
}

// linkUp reports whether an interface should be considered up: it must be
// administratively up (IFF_UP) and not report a down / lower-layer-down /
// not-present operational state. The "unknown" operstate that loopback and many
// virtual devices report while perfectly usable is treated as up.
func linkUp(attrs *netlink.LinkAttrs) bool {
	if attrs.Flags&net.FlagUp == 0 {
		return false
	}
	switch attrs.OperState {
	case netlink.OperDown, netlink.OperLowerLayerDown, netlink.OperNotPresent:
		return false
	default:
		return true
	}
}

// classify maps a netlink link to a coarse model.LinkType.
func classify(l netlink.Link) model.LinkType {
	attrs := l.Attrs()
	if attrs.Flags&net.FlagLoopback != 0 || attrs.Name == "lo" {
		return model.TypeLoopback
	}
	switch l.Type() {
	case "veth":
		return model.TypeVeth
	case "bridge":
		return model.TypeBridge
	case "bond":
		return model.TypeBond
	case "vlan":
		return model.TypeVlan
	case "tun":
		return model.TypeTun
	case "device":
		// A "device" with no virtual backing is a real NIC.
		if isPhysical(attrs.Name) {
			return model.TypePhysical
		}
		return model.TypeOther
	default:
		return model.TypeOther
	}
}

// isPhysical reports whether /sys says the interface is backed by real hardware
// (i.e. it is not under the virtual device tree).
func isPhysical(name string) bool {
	dest, err := os.Readlink(filepath.Join("/sys/class/net", name))
	if err != nil {
		return false
	}
	return !strings.Contains(dest, "devices/virtual/")
}
