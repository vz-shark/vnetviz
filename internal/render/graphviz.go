package render

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vz-shark/vnetviz/internal/model"
)

// graphvizBin is the Graphviz executable used to rasterize/vectorize DOT.
// It is a variable so tests (and users via PATH) can influence resolution.
var graphvizBin = "dot"

// Image renders the topology to an image by generating DOT and piping it
// through Graphviz. format is a Graphviz output type such as "svg" or "png".
func Image(t *model.Topology, opts Options, format string) ([]byte, error) {
	return Graphviz(DOT(t, opts), format)
}

// Graphviz feeds dot source to `dot -T<format>` and returns the rendered bytes.
// It returns a helpful error when Graphviz is not installed.
func Graphviz(dot, format string) ([]byte, error) {
	if _, err := exec.LookPath(graphvizBin); err != nil {
		return nil, fmt.Errorf(
			"graphviz %q not found in PATH; install graphviz (e.g. apt install graphviz) to use --format %s",
			graphvizBin, format)
	}

	cmd := exec.Command(graphvizBin, "-T"+format)
	cmd.Stdin = strings.NewReader(dot)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s -T%s failed: %v: %s", graphvizBin, format, err, msg)
		}
		return nil, fmt.Errorf("%s -T%s failed: %w", graphvizBin, format, err)
	}
	return out.Bytes(), nil
}
