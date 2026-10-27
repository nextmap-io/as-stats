package bgp

import (
	"context"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The BIRD / FRR helper scripts in scripts/bgp, driven through a
// CommandDaemon exactly as the API runs them, against stub birdc / vtysh
// binaries that keep state in files. They check the scripts' logic and the
// commands they send; they are not a test of BIRD or FRR themselves.

func repoScript(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "bgp", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeExec(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func mustTemplate(t *testing.T, s string) Template {
	t.Helper()
	tpl, err := ParseTemplate(s)
	if err != nil {
		t.Fatal(err)
	}
	return tpl
}

func TestBirdScript(t *testing.T) {
	dir := t.TempDir()
	birdc := writeExec(t, dir, "birdc", `#!/bin/sh
d=`+dir+`
echo "$*" >> "$d/calls"
case "$1" in
  configure) echo "Reading configuration from /etc/bird/bird.conf"; echo "Reconfigured" ;;
  show)
    case "$4" in
      rtbh4) f="$d/rtbh4.conf" ;;
      rtbh6) f="$d/rtbh6.conf" ;;
      *) echo "BGP state:          Established"; exit 0 ;;
    esac
    [ -f "$f" ] && sed -n 's/^route \(.*\) blackhole;$/\1  blackhole [rtbh 12:00:00] * (200)/p' "$f" ;;
esac
`)
	script := repoScript(t, "bird-rtbh.sh")
	opts := " --dir=" + dir + " --birdc=" + birdc
	d := NewCommandDaemon(CommandConfig{
		Announce:  mustTemplate(t, script+opts+" announce {prefix}"),
		Withdraw:  mustTemplate(t, script+opts+" withdraw {prefix}"),
		Routes:    mustTemplate(t, script+opts+" routes"),
		Status:    mustTemplate(t, script+opts+" status rtbh_upstream"),
		NextHopV4: "192.0.2.254",
	})
	ctx := context.Background()
	v4, v6 := netip.MustParsePrefix("198.51.100.10/32"), netip.MustParsePrefix("2001:db8:100::10/128")
	for _, p := range []netip.Prefix{v4, v6, v4} { // second v4 announce is a no-op edit
		if err := d.Announce(ctx, p); err != nil {
			t.Fatalf("announce %s: %v", p, err)
		}
	}
	conf4, _ := os.ReadFile(filepath.Join(dir, "rtbh4.conf"))
	if string(conf4) != "route 198.51.100.10/32 blackhole;\n" {
		t.Errorf("rtbh4.conf = %q", conf4)
	}
	routes, err := d.Routes(ctx)
	if err != nil || len(routes) != 2 {
		t.Fatalf("routes = %v, %v", routes, err)
	}
	if st, err := d.Status(ctx); err != nil || st.State != "established" {
		t.Errorf("status = %+v, %v", st, err)
	}
	if err := d.Withdraw(ctx, v4); err != nil {
		t.Fatal(err)
	}
	if err := d.Withdraw(ctx, v4); err != nil {
		t.Errorf("withdrawing an absent route must succeed: %v", err)
	}
	routes, _ = d.Routes(ctx)
	if len(routes) != 1 || routes[0] != v6 {
		t.Errorf("routes after withdraw = %v", routes)
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if n := strings.Count(string(calls), "configure"); n != 4 { // 3 announces + 1 effective withdraw
		t.Errorf("%d reconfigurations:\n%s", n, calls)
	}
	// The script refuses anything that is not address/length.
	for _, bad := range []string{"198.51.100.10", "198.51.100.10/32;reboot", "x/32 }; protocol"} {
		if err := exec.Command(script, "--dir="+dir, "--birdc="+birdc, "announce", bad).Run(); err == nil {
			t.Errorf("bird-rtbh.sh accepted %q", bad)
		}
	}
}

func TestFRRScript(t *testing.T) {
	dir := t.TempDir()
	vtysh := writeExec(t, dir, "vtysh", `#!/bin/sh
d=`+dir+`
cfg="$d/running"; touch "$cfg"
while [ $# -gt 0 ]; do
  if [ "$1" != "-c" ]; then shift; continue; fi
  cmd=$2; shift 2
  echo "$cmd" >> "$d/calls"
  case "$cmd" in
    "show running-config") cat "$cfg" ;;
    "no network "*) grep -vxF "  ${cmd#no }" "$cfg" > "$cfg.t"; mv "$cfg.t" "$cfg" ;;
    "network "*) echo "  $cmd" >> "$cfg" ;;
    "no ip route "*|"no ipv6 route "*) grep -vxF "${cmd#no }" "$cfg" > "$cfg.t"; mv "$cfg.t" "$cfg" ;;
    "ip route "*|"ipv6 route "*) echo "$cmd" >> "$cfg" ;;
  esac
done
`)
	script := repoScript(t, "frr-rtbh.sh")
	opt := " --vtysh=" + vtysh
	d := NewCommandDaemon(CommandConfig{
		Announce:  mustTemplate(t, script+opt+" announce {prefix} {local_as}"),
		Withdraw:  mustTemplate(t, script+opt+" withdraw {prefix} {local_as}"),
		Routes:    mustTemplate(t, script+opt+" routes"),
		NextHopV4: "192.0.2.254",
		LocalAS:   64501,
	})
	ctx := context.Background()
	v4, v6 := netip.MustParsePrefix("198.51.100.10/32"), netip.MustParsePrefix("2001:db8:100::10/128")
	for _, p := range []netip.Prefix{v4, v6} {
		if err := d.Announce(ctx, p); err != nil {
			t.Fatalf("announce %s: %v", p, err)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	for _, want := range []string{"ip route 198.51.100.10/32 blackhole", "router bgp 64501", "address-family ipv4 unicast", "network 198.51.100.10/32 route-map RTBH", "ipv6 route 2001:db8:100::10/128 blackhole", "address-family ipv6 unicast"} {
		if !strings.Contains(string(calls), want+"\n") {
			t.Errorf("vtysh never got %q:\n%s", want, calls)
		}
	}
	routes, err := d.Routes(ctx)
	if err != nil || len(routes) != 2 {
		t.Fatalf("routes = %v, %v", routes, err)
	}
	if err := d.Withdraw(ctx, v4); err != nil {
		t.Fatal(err)
	}
	if err := d.Withdraw(ctx, v4); err != nil {
		t.Errorf("withdrawing an absent route must succeed: %v", err)
	}
	running, _ := os.ReadFile(filepath.Join(dir, "running"))
	if strings.Contains(string(running), "198.51.100.10") || !strings.Contains(string(running), "2001:db8:100::10/128") {
		t.Errorf("running-config after withdraw:\n%s", running)
	}
	for _, args := range [][]string{{"announce", "198.51.100.10/32", "AS64501"}, {"announce", "198.51.100.10/32;x", "64501"}, {"announce", "198.51.100.10/32"}} {
		if err := exec.Command(script, append([]string{"--vtysh=" + vtysh}, args...)...).Run(); err == nil {
			t.Errorf("frr-rtbh.sh accepted %q", args)
		}
	}
}
