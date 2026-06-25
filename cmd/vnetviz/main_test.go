package main

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestToggleSet(t *testing.T) {
	on := []string{"on", "On", "TRUE", "yes", "1"}
	off := []string{"off", "Off", "false", "no", "0"}
	for _, s := range on {
		var tg toggle
		if err := tg.Set(s); err != nil || !tg.val {
			t.Errorf("Set(%q) = (%v, %v), want on", s, tg.val, err)
		}
	}
	for _, s := range off {
		tg := toggle{val: true}
		if err := tg.Set(s); err != nil || tg.val {
			t.Errorf("Set(%q) = (%v, %v), want off", s, tg.val, err)
		}
	}
	var tg toggle
	if err := tg.Set("maybe"); err == nil {
		t.Errorf("Set(%q) should error", "maybe")
	}
	if got := (&toggle{val: true}).String(); got != "on" {
		t.Errorf("String(on) = %q", got)
	}
	if got := (&toggle{val: false}).String(); got != "off" {
		t.Errorf("String(off) = %q", got)
	}
}

func TestResolveToggle(t *testing.T) {
	cases := []struct {
		name string
		args []string
		all  bool
		def  bool
		want bool
	}{
		{"default on, untouched", nil, false, true, true},
		{"default off, untouched", nil, false, false, false},
		{"all forces default-off on", nil, true, false, true},
		{"all forces default-on on", nil, true, true, true},
		{"explicit off wins over all", []string{"--ip", "off"}, true, true, false},
		{"explicit on without all", []string{"--ip", "on"}, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Register and parse against the same toggle the real flag set uses,
			// so flags.Changed and the toggle's parsed value stay consistent.
			f := pflag.NewFlagSet("test", pflag.ContinueOnError)
			tg := toggle{val: c.def}
			f.Var(&tg, "ip", "")
			if err := f.Parse(c.args); err != nil {
				t.Fatalf("parse %v: %v", c.args, err)
			}
			if got := resolveToggle(f, c.all, "ip", tg); got != c.want {
				t.Errorf("resolveToggle(all=%v, def=%v, args=%v) = %v, want %v", c.all, c.def, c.args, got, c.want)
			}
		})
	}
}

func TestLocaleIsUTF8(t *testing.T) {
	// Cleared by t.Setenv after each subtest; set all three so leftover
	// environment from the test runner cannot leak in.
	cases := []struct {
		name                 string
		lcAll, lcCtype, lang string
		want                 bool
	}{
		{"C locale is ascii", "C", "", "", false},
		{"empty locale is ascii", "", "", "", false},
		{"LANG utf-8", "", "", "en_US.UTF-8", true},
		{"LANG utf8 lowercase", "", "", "ja_JP.utf8", true},
		{"LC_ALL wins over LANG", "C", "", "en_US.UTF-8", false},
		{"LC_CTYPE utf-8 with C LANG", "", "en_US.UTF-8", "C", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("LC_ALL", c.lcAll)
			t.Setenv("LC_CTYPE", c.lcCtype)
			t.Setenv("LANG", c.lang)
			if got := localeIsUTF8(); got != c.want {
				t.Errorf("localeIsUTF8() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSudoHint(t *testing.T) {
	t.Run("no skipped namespaces yields no message", func(t *testing.T) {
		if got := sudoHint(nil, []string{"--docker"}, true); got != "" {
			t.Fatalf("want empty hint, got %q", got)
		}
	})

	t.Run("plain text has no ANSI codes and echoes flags", func(t *testing.T) {
		got := sudoHint([]string{"vztest", "web"}, []string{"--all", "--ip", "off"}, false)
		if strings.Contains(got, "\033[") {
			t.Errorf("plain hint should not contain ANSI escapes: %q", got)
		}
		if !strings.Contains(got, "cannot read 2 namespace(s) without root") {
			t.Errorf("hint should report the count: %q", got)
		}
		if !strings.Contains(got, "vztest, web") {
			t.Errorf("hint should list skipped namespaces: %q", got)
		}
		if !strings.Contains(got, "sudo vnetviz --all --ip off") {
			t.Errorf("hint should echo the original flags after sudo: %q", got)
		}
	})

	t.Run("colorized hint wraps lines in yellow", func(t *testing.T) {
		got := sudoHint([]string{"vztest"}, nil, true)
		if !strings.Contains(got, "\033[33m") || !strings.Contains(got, "\033[0m") {
			t.Errorf("colorized hint should contain yellow on/off codes: %q", got)
		}
		// With no extra args the command is just "sudo vnetviz" (no trailing space).
		if !strings.Contains(got, "re-run with: sudo vnetviz\033[0m") {
			t.Errorf("hint should not leave a trailing space before reset: %q", got)
		}
	})
}
