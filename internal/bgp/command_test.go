package bgp

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseTemplateSplitsLikeASHell(t *testing.T) {
	cases := map[string][]string{
		"gobgp global rib add {prefix} -a {family}":                                     {"gobgp", "global", "rib", "add", "{prefix}", "-a", "{family}"},
		"birdc 'show route protocol rtbh'":                                              {"birdc", "show route protocol rtbh"},
		`vtysh -c "conf t" -c 'ip route {prefix} blackhole'`:                            {"vtysh", "-c", "conf t", "-c", "ip route {prefix} blackhole"},
		`echo "a \"quoted\" word" back\ slash ''`:                                       {"echo", `a "quoted" word`, "back slash", ""},
		"  /usr/local/bin/rtbh.sh   announce\t{prefix}  ":                               {"/usr/local/bin/rtbh.sh", "announce", "{prefix}"},
		`sh -c 'gobgp global rib -a ipv4 | grep {prefix}'`:                              {"sh", "-c", "gobgp global rib -a ipv4 | grep {prefix}"},
		"exabgpcli announce route {prefix} next-hop {next_hop} community [{community}]": {"exabgpcli", "announce", "route", "{prefix}", "next-hop", "{next_hop}", "community", "[{community}]"},
	}
	for in, want := range cases {
		tpl, err := ParseTemplate(in)
		if err != nil {
			t.Errorf("ParseTemplate(%q): %v", in, err)
			continue
		}
		if !reflect.DeepEqual(tpl.args, want) {
			t.Errorf("ParseTemplate(%q) = %q, want %q", in, tpl.args, want)
		}
	}
}

func TestParseTemplateRejects(t *testing.T) {
	for in, why := range map[string]string{
		"":                                    "empty",
		"   ":                                 "empty",
		"gobgp global rib add {ip} | logger":  "shell operator",
		"gobgp global rib add {ip}; reboot":   "shell operator",
		"gobgp global rib add {ip} && echo":   "shell operator",
		"gobgp global rib add {ip} > /tmp/x":  "shell operator",
		"gobgp 'unterminated {ip}":            "unbalanced",
		"gobgp global rib add {peer_asn}":     "unknown placeholder",
		"{ip} global rib":                     "program name",
		`gobgp global rib add {ip} trailing\`: "unbalanced",
	} {
		if _, err := ParseTemplate(in); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("ParseTemplate(%q) = %v, want an error about %q", in, err, why)
		}
	}
	// Quoted operators are plain arguments.
	if _, err := ParseTemplate(`logger '|' {ip}`); err != nil {
		t.Errorf("quoted pipe rejected: %v", err)
	}
}

// TestExpandNeverAddsArguments: a value can only ever fill the argument its
// placeholder sits in. (Values come from netip / validated config anyway;
// this pins the second line of defence.)
func TestExpandNeverAddsArguments(t *testing.T) {
	tpl, err := ParseTemplate("gobgp global rib add {prefix} community {communities} -a {family}")
	if err != nil {
		t.Fatal(err)
	}
	argv := tpl.Expand(map[string]string{
		"prefix":      "1.2.3.4/32; rm -rf / #",
		"communities": "65535:666 $(reboot)",
		"family":      "ipv4",
	})
	want := []string{"gobgp", "global", "rib", "add", "1.2.3.4/32; rm -rf / #", "community", "65535:666 $(reboot)", "-a", "ipv4"}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("Expand = %q, want %q", argv, want)
	}
}

func TestDefaultTemplatesParse(t *testing.T) {
	for _, s := range []string{DefaultAnnounceCmd, DefaultWithdrawCmd, DefaultStatusCmd, DefaultRoutesCmd} {
		if _, err := ParseTemplate(s); err != nil {
			t.Errorf("default %q: %v", s, err)
		}
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cmd.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRunCommandEnvironment: daemon commands never inherit the API's
// secrets, and get the ASSTATS_BGP_* values instead.
func TestRunCommandEnvironment(t *testing.T) {
	t.Setenv("CLICKHOUSE_PASSWORD", "s3cret")
	t.Setenv("BGP_API_TOKEN", "t0ken")
	script := writeScript(t, `env`)
	tpl, _ := ParseTemplate(script + " {prefix}")
	d := NewCommandDaemon(CommandConfig{Announce: tpl, Withdraw: tpl, NextHopV4: "192.0.2.254", Communities: []string{"65535:666", "65535:65281"}})
	v := d.vars("announce", netip.MustParsePrefix("198.51.100.7/32"))
	out, err := runCommand(context.Background(), time.Second, tpl.Expand(v), envFor(v))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"s3cret", "t0ken", "CLICKHOUSE", "HOME="} {
		if strings.Contains(out, leak) {
			t.Errorf("environment leaks %q:\n%s", leak, out)
		}
	}
	for _, want := range []string{"ASSTATS_BGP_PREFIX=198.51.100.7/32", "ASSTATS_BGP_NEXT_HOP=192.0.2.254", "ASSTATS_BGP_COMMUNITIES=65535:666,65535:65281", "ASSTATS_BGP_ACTION=announce", "LANG=C"} {
		if !strings.Contains(out, want) {
			t.Errorf("environment lacks %q:\n%s", want, out)
		}
	}
}

func TestRunCommandTimeoutKillsHungChildren(t *testing.T) {
	// The child keeps stdout open: without WaitDelay, Run would wait for it.
	script := writeScript(t, "sleep 30 &\nsleep 30\n")
	start := time.Now()
	_, err := runCommand(context.Background(), 200*time.Millisecond, []string{script}, nil)
	if err == nil {
		t.Fatal("hung command succeeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error %v is not a deadline", err)
	}
	var ce *CommandError
	if !errors.As(err, &ce) || !strings.Contains(ce.Short(), "timed out") {
		t.Errorf("Short() = %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("timeout took %s", took)
	}
}

func TestRunCommandFailureAndOutputCap(t *testing.T) {
	script := writeScript(t, "head -c 200000 /dev/zero | tr '\\0' x\necho boom >&2\nexit 3\n")
	out, err := runCommand(context.Background(), 5*time.Second, []string{script}, nil)
	var ce *CommandError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want CommandError", err)
	}
	if len(out) > maxCommandOutput {
		t.Errorf("output %d bytes, cap %d", len(out), maxCommandOutput)
	}
	if strings.Contains(ce.Short(), "xxx") || !strings.Contains(ce.Short(), "exit status 3") {
		t.Errorf("Short() = %q: must name the exit status and never echo output", ce.Short())
	}
	if len(ce.Error()) > 2000 {
		t.Errorf("Error() is %d bytes", len(ce.Error()))
	}
}

func TestCommandDaemonRefusesIPv6WithoutNextHop(t *testing.T) {
	tpl, _ := ParseTemplate("true {prefix} {next_hop}")
	d := NewCommandDaemon(CommandConfig{Announce: tpl, Withdraw: tpl, NextHopV4: "192.0.2.254"})
	err := d.Announce(context.Background(), netip.MustParsePrefix("2001:db8::1/128"))
	var pe *PolicyError
	if !errors.As(err, &pe) || !strings.Contains(err.Error(), "BGP_NEXT_HOP_V6") {
		t.Fatalf("IPv6 announce without a v6 next hop = %v", err)
	}
	if err := d.Announce(context.Background(), netip.MustParsePrefix("192.0.2.1/32")); err != nil {
		t.Fatalf("IPv4 announce: %v", err)
	}
}
