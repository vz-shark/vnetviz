package main

import (
	"strings"
	"testing"
)

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
		got := sudoHint([]string{"vztest", "web"}, []string{"--docker", "--ip"}, false)
		if strings.Contains(got, "\033[") {
			t.Errorf("plain hint should not contain ANSI escapes: %q", got)
		}
		if !strings.Contains(got, "cannot read 2 namespace(s) without root") {
			t.Errorf("hint should report the count: %q", got)
		}
		if !strings.Contains(got, "vztest, web") {
			t.Errorf("hint should list skipped namespaces: %q", got)
		}
		if !strings.Contains(got, "sudo vnetviz --docker --ip") {
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
