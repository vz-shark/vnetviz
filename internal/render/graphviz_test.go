package render

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

func TestGraphvizSVG(t *testing.T) {
	if _, err := exec.LookPath(graphvizBin); err != nil {
		// Graphviz absent: the error must point the user at installing it.
		_, gerr := Image(sample(), Options{}, "svg")
		if gerr == nil || !strings.Contains(gerr.Error(), "graphviz") {
			t.Fatalf("want a helpful 'graphviz not found' error, got %v", gerr)
		}
		t.Skip("graphviz not installed; verified the missing-binary error path")
	}

	out, err := Image(sample(), Options{}, "svg")
	if err != nil {
		t.Fatalf("Image svg: %v", err)
	}
	if !bytes.Contains(out, []byte("<svg")) {
		t.Errorf("svg output does not look like SVG:\n%s", out)
	}
	// The bridge node label should survive into the rendered SVG.
	if !bytes.Contains(out, []byte("docker0")) {
		t.Errorf("svg output missing the docker0 node label")
	}
}
