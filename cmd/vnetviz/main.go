// Command vnetviz visualizes the Linux network topology (network namespaces,
// veth pairs, bridges, Docker / Podman containers) as Mermaid or Graphviz DOT.
//
// Usage:
//
//	sudo vnetviz --format mermaid
//	sudo vnetviz --format dot --all --output net.dot
//	dot -Tsvg net.dot -o net.svg
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vz-shark/vnetviz/internal/collect"
	"github.com/vz-shark/vnetviz/internal/render"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// errReported signals that a command already wrote a message to stderr; main
// should exit non-zero without printing it a second time.
var errReported = errors.New("reported")

// options holds the parsed command-line flags.
type options struct {
	format   string
	output   string
	lo       bool
	ip       bool
	physical bool
	all      bool
	virtual  bool
	collapse bool
	upOnly   bool
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, "vnetviz:", err)
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var opt options
	cmd := &cobra.Command{
		Use:           "vnetviz",
		Short:         "Visualize Linux network topology as a tree, Mermaid, or Graphviz",
		Version:       version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runVnetviz(&opt)
		},
	}

	f := cmd.Flags()
	f.SortFlags = false
	f.StringVarP(&opt.format, "format", "f", "text", "output format: text | unicode | ascii | mermaid | dot | svg | png")
	f.StringVarP(&opt.output, "output", "o", "", "write to `FILE` instead of stdout")
	f.BoolVarP(&opt.virtual, "virtual", "v", false, "virtual topology only: --ip (no loopback or physical)")
	f.BoolVar(&opt.all, "all", false, "enable everything (--lo --ip --physical)")
	f.BoolVar(&opt.lo, "lo", false, "show loopback interfaces")
	f.BoolVar(&opt.ip, "ip", false, "show IP addresses")
	f.BoolVar(&opt.physical, "physical", false, "show physical NICs")
	f.BoolVar(&opt.collapse, "collapse", false, "collapse veth pairs into a single link")
	f.BoolVar(&opt.upOnly, "up-only", false, "hide interfaces that are operationally down")

	cmd.SetVersionTemplate("vnetviz {{.Version}}\n")
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetUsageFunc(usageFunc)
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) { _ = usageFunc(c) })
	return cmd
}

func runVnetviz(opt *options) error {
	if opt.all {
		opt.lo, opt.ip, opt.physical = true, true, true
	}
	// The virtual topology is the default view: with no scope flags we behave as
	// --virtual. Passing any scope flag (or --all) opts out of the default, so
	// e.g. `--physical` shows only physical NICs while `--virtual --physical`
	// adds them on top of the virtual set.
	if !opt.all && !opt.virtual && !opt.lo && !opt.ip && !opt.physical {
		opt.virtual = true
	}
	// --virtual surfaces addresses on top of the always-on virtual topology, but
	// leaves --lo / --physical off unless asked.
	if opt.virtual {
		opt.ip = true
	}

	collectOpts := collect.Options{
		Loopback: opt.lo,
		IP:       opt.ip,
		Physical: opt.physical,
		UpOnly:   opt.upOnly,
		Warnf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "vnetviz: warning: "+format+"\n", args...)
		},
	}

	top, needRoot, err := collect.Topology(collectOpts)
	if err != nil {
		return err
	}
	// If any namespace was discovered but needs root to enter, stop here rather
	// than emit a misleading partial diagram.
	if len(needRoot) > 0 {
		hintSudo(needRoot)
		return errReported
	}

	renderOpts := render.Options{CollapseVeth: opt.collapse}
	// Highlight the text output's "down" token only when it is heading straight
	// to a terminal, so piped or file (`-o`) output stays free of ANSI codes.
	if opt.output == "" && isTerminal(os.Stdout) {
		renderOpts.Color = true
	}

	var data []byte
	switch opt.format {
	case "text":
		// Like tree(1) with no --charset: pick the charset from the locale.
		renderOpts.Charset = render.CharsetASCII
		if localeIsUTF8() {
			renderOpts.Charset = render.CharsetUnicode
		}
		data = []byte(render.Text(top, renderOpts))
	case "unicode":
		renderOpts.Charset = render.CharsetUnicode
		data = []byte(render.Text(top, renderOpts))
	case "ascii":
		renderOpts.Charset = render.CharsetASCII
		data = []byte(render.Text(top, renderOpts))
	case "mermaid":
		data = []byte(render.Mermaid(top, renderOpts))
		// When writing to a Markdown file, wrap the diagram in a ```mermaid
		// fence so it renders inline on GitHub and other Markdown viewers.
		if strings.HasSuffix(strings.ToLower(opt.output), ".md") {
			data = []byte("```mermaid\n" + string(data) + "```\n")
		}
	case "dot":
		data = []byte(render.DOT(top, renderOpts))
	case "svg", "png":
		img, err := render.Image(top, renderOpts, opt.format)
		if err != nil {
			return err
		}
		data = img
	default:
		return fmt.Errorf("unknown format %q (want text, unicode, ascii, mermaid, dot, svg or png)", opt.format)
	}

	if opt.output == "" {
		// Refuse to splatter binary image data over an interactive terminal.
		if opt.format == "png" && isTerminal(os.Stdout) {
			return fmt.Errorf("refusing to write binary %s to the terminal; use --output FILE", opt.format)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
		return nil
	}
	if err := os.WriteFile(opt.output, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", opt.output, err)
	}
	return nil
}

// usageFunc prints the flags grouped by purpose (rather than pflag's default
// alphabetical listing), with --version last.
func usageFunc(cmd *cobra.Command) error {
	w := cmd.OutOrStderr()
	fmt.Fprintln(w, "vnetviz — visualize Linux network topology as a tree, Mermaid, or Graphviz")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage: vnetviz [options]")
	fmt.Fprintln(w)

	flags := cmd.Flags()
	show := func(name string) {
		f := flags.Lookup(name)
		if f == nil {
			return
		}
		head := "--" + f.Name
		if f.Shorthand != "" {
			head += ", -" + f.Shorthand
		}
		if arg, _ := unquoteArg(f); arg != "" {
			head += " " + arg
		}
		_, help := unquoteArg(f)
		line := fmt.Sprintf("  %-22s %s", head, help)
		if f.DefValue != "" && f.DefValue != "false" {
			line += fmt.Sprintf(" (default %q)", f.DefValue)
		}
		fmt.Fprintln(w, line)
	}

	groups := []struct {
		title string
		names []string
	}{
		{"Output:", []string{"format", "output"}},
		{"Scope (default: --virtual):", []string{"virtual", "all", "lo", "ip", "physical"}},
		{"Display:", []string{"collapse", "up-only"}},
		{"Other:", []string{"help", "version"}},
	}
	for _, g := range groups {
		fmt.Fprintln(w, g.title)
		for _, n := range g.names {
			show(n)
		}
		fmt.Fprintln(w)
	}
	return nil
}

// unquoteArg returns the placeholder shown after a flag (a back-quoted token in
// the usage string, else the value type for non-bool flags) and the usage text
// with the back-quotes removed.
func unquoteArg(f *pflag.Flag) (arg, help string) {
	help = f.Usage
	if i := strings.IndexByte(help, '`'); i >= 0 {
		if j := strings.IndexByte(help[i+1:], '`'); j >= 0 {
			arg = help[i+1 : i+1+j]
			help = help[:i] + arg + help[i+1+j+1:]
			return arg, help
		}
	}
	if f.Value.Type() != "bool" {
		arg = f.Value.Type()
	}
	return arg, help
}

// hintSudo prints the "needs root" error to stderr when one or more namespaces
// were discovered but could not be entered without root. The message is
// colorized (yellow) only when stderr is a terminal, so redirected or piped
// output stays free of ANSI escape codes.
func hintSudo(needRoot []string) {
	if len(needRoot) == 0 {
		return
	}
	fmt.Fprint(os.Stderr, sudoHint(needRoot, os.Args[1:], isTerminal(os.Stderr)))
}

// sudoHint builds the "needs root, re-run with sudo" error message. It is split
// out from hintSudo so the formatting and colorization can be tested without a
// real terminal. The returned string ends with a newline; it is empty when no
// namespace needs root.
func sudoHint(needRoot, args []string, color bool) string {
	if len(needRoot) == 0 {
		return ""
	}
	const yellow, reset = "\033[33m", "\033[0m"
	on, off := "", ""
	if color {
		on, off = yellow, reset
	}
	cmd := strings.TrimSpace("sudo vnetviz " + strings.Join(args, " "))
	return fmt.Sprintf("%svnetviz: cannot read %d namespace(s) without root: %s%s\n",
		on, len(needRoot), strings.Join(needRoot, ", "), off) +
		fmt.Sprintf("%s         re-run with: %s%s\n", on, cmd, off)
}

// localeIsUTF8 reports whether the current locale uses a UTF-8 character set,
// mirroring how tree(1) decides between Unicode and ASCII line drawing. The
// first of LC_ALL, LC_CTYPE, LANG that is set wins (POSIX locale precedence);
// when none is set the locale is effectively "C", which is not UTF-8.
func localeIsUTF8() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		u := strings.ToUpper(v)
		return strings.Contains(u, "UTF-8") || strings.Contains(u, "UTF8")
	}
	return false
}

// isTerminal reports whether f is attached to a character device (a TTY).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
