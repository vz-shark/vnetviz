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

// toggle is an explicit on/off flag value (`--ip on`, `--ip off`). Unlike a
// plain bool it distinguishes "left at its default" from "explicitly set", which
// lets --all turn everything on while still yielding to any flag the user set by
// hand. The zero value is off; seed val with the desired default before
// registering so the help text and default behavior reflect it.
type toggle struct {
	val bool
}

func (t *toggle) String() string {
	if t.val {
		return "on"
	}
	return "off"
}

func (t *toggle) Set(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes", "1":
		t.val = true
	case "off", "false", "no", "0":
		t.val = false
	default:
		return fmt.Errorf("invalid value %q (want on or off)", s)
	}
	return nil
}

func (t *toggle) Type() string { return "on|off" }

// options holds the parsed command-line flags. The toggle defaults are chosen so
// that running vnetviz with no flags shows the virtual topology with IP
// addresses, veth pairs collapsed, and only operationally-up interfaces.
type options struct {
	format string
	output string
	all    bool

	virtual  toggle // show virtual devices (veth, bridges, ...)
	lo       toggle // show the loopback interface
	physical toggle // show physical NICs
	ip       toggle // show IP addresses
	detail   toggle // show interfaces in full detail (expand collapsed veth pairs)
	upped    toggle // show operationally-up interfaces
	downed   toggle // show operationally-down interfaces
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
	// Seed each toggle with its default so `--flag` is optional and the help
	// shows the default. With no flags this is the virtual topology, IPs on,
	// collapsed, up-only.
	opt := options{
		virtual:  toggle{val: true},
		lo:       toggle{val: false},
		physical: toggle{val: false},
		ip:       toggle{val: true},
		detail:   toggle{val: false},
		upped:    toggle{val: true},
		downed:   toggle{val: false},
	}
	cmd := &cobra.Command{
		Use:           "vnetviz",
		Short:         "Visualize Linux network topology as a tree, Mermaid, or Graphviz",
		Version:       version,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			return runVnetviz(c.Flags(), &opt)
		},
	}

	f := cmd.Flags()
	f.SortFlags = false
	f.StringVarP(&opt.format, "format", "f", "text", "output format: text | unicode | ascii | mermaid | dot | svg | png")
	f.StringVarP(&opt.output, "output", "o", "", "write to `FILE` instead of stdout")
	f.Var(&opt.virtual, "virtual", "show virtual devices (veth, bridges, bonds, VLANs, ...)")
	f.Var(&opt.lo, "lo", "show loopback interfaces")
	f.Var(&opt.physical, "physical", "show physical NICs")
	f.Var(&opt.ip, "ip", "show IP addresses")
	f.Var(&opt.detail, "detail", "show interfaces in full detail")
	f.Var(&opt.upped, "upped", "show operationally-up interfaces")
	f.Var(&opt.downed, "downed", "show operationally-down interfaces")
	f.BoolVar(&opt.all, "all", false, "show everything; per-flag on|off still takes precedence")

	cmd.SetVersionTemplate("vnetviz {{.Version}}\n")
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetUsageFunc(usageFunc)
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) { _ = usageFunc(c) })
	return cmd
}

// resolveToggle reduces a toggle to its final value: an explicitly-set flag
// always wins; otherwise --all forces it on, falling back to the default.
func resolveToggle(flags *pflag.FlagSet, all bool, name string, t toggle) bool {
	if all && !flags.Changed(name) {
		return true
	}
	return t.val
}

func runVnetviz(flags *pflag.FlagSet, opt *options) error {
	pick := func(name string, t toggle) bool { return resolveToggle(flags, opt.all, name, t) }

	collectOpts := collect.Options{
		Virtual:  pick("virtual", opt.virtual),
		Loopback: pick("lo", opt.lo),
		IP:       pick("ip", opt.ip),
		Physical: pick("physical", opt.physical),
		Upped:    pick("upped", opt.upped),
		Downed:   pick("downed", opt.downed),
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

	renderOpts := render.Options{CollapseVeth: !pick("detail", opt.detail)}
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
		{"Scope (each on|off):", []string{"virtual", "lo", "physical", "all"}},
		{"Display (each on|off):", []string{"ip", "detail", "upped", "downed"}},
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
