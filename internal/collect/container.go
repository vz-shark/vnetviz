package collect

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// portsTemplate flattens a container's published ports into a parseable stream
// of "hostIP|hostPort|containerPort;" records. Exposed-but-unpublished ports
// have a null binding list and are skipped by the inner range.
const portsTemplate = `{{range $p, $bs := .NetworkSettings.Ports}}{{range $bs}}{{.HostIp}}|{{.HostPort}}|{{$p}};{{end}}{{end}}`

// dockerContainers lists running Docker containers as namespace targets. The
// bool reports that the engine is installed but could not be queried while we
// are not root, i.e. running under sudo would likely fix it.
func dockerContainers() ([]nsTarget, bool) {
	return cliContainers("docker", model.KindDocker)
}

// podmanContainers does the same for Podman.
func podmanContainers() ([]nsTarget, bool) {
	return cliContainers("podman", model.KindPodman)
}

// cliContainers shells out to the docker/podman CLI to enumerate running
// containers and resolves each to its network namespace via /proc/<pid>/ns/net.
//
// Both CLIs accept the same `inspect` formatting, so one code path serves both.
//
// The returned bool flags the one case worth telling the user about: the engine
// is installed but `ps` failed while we are not root. That is almost always a
// socket-permission problem that sudo fixes, so the caller surfaces it as a
// "needs root" hint. A missing CLI, or a failure while already root (a dead
// daemon, which sudo won't fix), yields no containers and no hint.
func cliContainers(bin string, kind model.NSKind) ([]nsTarget, bool) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, false // not installed
	}

	ids, err := runCLI(bin, "ps", "-q")
	if err != nil {
		return nil, os.Geteuid() != 0
	}
	idList := strings.Fields(ids)
	if len(idList) == 0 {
		return nil, false
	}

	var targets []nsTarget
	for _, id := range idList {
		// {{.State.Pid}} is the container's main PID in the host pid ns;
		// {{.Name}} carries a leading slash on Docker which we trim; the third
		// field carries the published ports (may be empty).
		out, err := runCLI(bin, "inspect",
			"--format", "{{.State.Pid}}\t{{.Name}}\t"+portsTemplate, id)
		if err != nil {
			continue // skip a container we cannot inspect
		}
		fields := strings.SplitN(strings.TrimSuffix(out, "\n"), "\t", 3)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil || pid <= 0 {
			// Not running (PID 0) or unparseable; skip.
			continue
		}
		name := strings.TrimPrefix(strings.TrimSpace(fields[1]), "/")
		var ports []model.PortMap
		if len(fields) == 3 {
			ports = parsePorts(fields[2])
		}
		targets = append(targets, nsTarget{
			name:  name,
			kind:  kind,
			path:  fmt.Sprintf("/proc/%d/ns/net", pid),
			ports: ports,
		})
	}
	return targets, false
}

// parsePorts turns the "hostIP|hostPort|containerPort;" stream produced by
// portsTemplate into PortMaps, deduplicating the IPv4/IPv6 pair Docker emits
// for the same publish (e.g. 0.0.0.0 and :: bound to the same host port).
func parsePorts(s string) []model.PortMap {
	var out []model.PortMap
	seen := map[string]bool{}
	for _, rec := range strings.Split(s, ";") {
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "|", 3)
		if len(f) != 3 || f[1] == "" {
			continue
		}
		key := f[1] + "|" + f[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, model.PortMap{HostIP: f[0], HostPort: f[1], ContainerPort: f[2]})
	}
	return out
}

// dockerNetworkBridges maps each host bridge interface name to its friendly
// Docker network name (e.g. "br-abc123def456" -> "demo_frontend", "docker0" ->
// "bridge").
func dockerNetworkBridges() (map[string]string, error) { return cliNetworkBridges("docker") }

// podmanNetworkBridges does the same for Podman.
func podmanNetworkBridges() (map[string]string, error) { return cliNetworkBridges("podman") }

// cliNetworkBridges resolves bridge-driver container networks to their host
// bridge interface name. The bridge name is the explicit
// com.docker.network.bridge.name option when set, else Docker's default of
// "br-" + the first 12 hex digits of the network id.
//
// Like cliContainers it is best-effort and never errors: any failure just means
// bridges go without their friendly network name.
func cliNetworkBridges(bin string) (map[string]string, error) {
	if _, err := exec.LookPath(bin); err != nil {
		return nil, nil // not installed: skip silently
	}
	ids, err := runCLI(bin, "network", "ls", "-q")
	if err != nil {
		return nil, nil
	}
	idList := strings.Fields(ids)
	if len(idList) == 0 {
		return nil, nil
	}
	args := append([]string{"network", "inspect", "--format",
		"{{.Name}}\t{{.Id}}\t{{index .Options \"com.docker.network.bridge.name\"}}"}, idList...)
	out, err := runCLI(bin, args...)
	if err != nil {
		return nil, nil
	}
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) != 3 {
			continue
		}
		name, id, bridge := f[0], f[1], f[2]
		// A missing template lookup renders as "<no value>"; treat it as unset.
		if bridge == "" || bridge == "<no value>" {
			if len(id) < 12 {
				continue
			}
			bridge = "br-" + id[:12]
		}
		m[bridge] = name
	}
	return m, nil
}

func runCLI(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", bin, strings.Join(args, " "), err)
	}
	return string(out), nil
}
