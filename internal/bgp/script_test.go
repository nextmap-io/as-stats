package bgp

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// emptyBlockStore implements BlockStore and returns no active blocks.
type emptyBlockStore struct{}

func (s *emptyBlockStore) ListActiveBlocks(ctx context.Context) ([]model.BGPBlock, error) {
	return nil, nil
}

func TestScriptBlocker_AnnounceWithdraw(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell commands require unix")
	}

	sb, err := NewScript(Config{
		AnnounceCmd: "echo announce {ip}",
		WithdrawCmd: "echo withdraw {ip}",
		Community:   "65535:666",
		NextHop:     "192.0.2.1",
		PeerAddress: "192.0.2.2",
		PeerAS:      64500,
		LocalAS:     64500,
	}, &emptyBlockStore{}) // empty store — no startup reload
	if err != nil {
		t.Fatalf("NewScript: %v", err)
	}
	defer sb.Stop()

	ctx := context.Background()
	ip := net.ParseIP("198.51.100.1")

	// Announce
	if err := sb.Announce(ctx, ip, 0, "test"); err != nil {
		t.Fatalf("Announce: %v", err)
	}

	list, err := sb.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 route after Announce, got %d", len(list))
	}
	if list[0].Target.String() != ip.String() {
		t.Errorf("expected target %s, got %s", ip, list[0].Target)
	}

	// Withdraw
	if err := sb.Withdraw(ctx, ip); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	list, err = sb.List(ctx)
	if err != nil {
		t.Fatalf("List after Withdraw: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 routes after Withdraw, got %d", len(list))
	}
}

func TestScriptBlocker_AnnounceIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell commands require unix")
	}

	sb, err := NewScript(Config{
		AnnounceCmd: "echo announce {ip}",
		WithdrawCmd: "echo withdraw {ip}",
		Community:   "65535:666",
		NextHop:     "192.0.2.1",
		PeerAddress: "192.0.2.2",
		PeerAS:      64500,
		LocalAS:     64500,
	}, &emptyBlockStore{})
	if err != nil {
		t.Fatalf("NewScript: %v", err)
	}
	defer sb.Stop()

	ctx := context.Background()
	ip := net.ParseIP("198.51.100.2")

	// Announce the same IP twice
	if err := sb.Announce(ctx, ip, 0, "first"); err != nil {
		t.Fatalf("first Announce: %v", err)
	}
	if err := sb.Announce(ctx, ip, 0, "second"); err != nil {
		t.Fatalf("second Announce: %v", err)
	}

	list, err := sb.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected exactly 1 route (idempotent), got %d", len(list))
	}
}

func TestScriptBlocker_Status(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell commands require unix")
	}

	t.Run("empty status cmd", func(t *testing.T) {
		sb, err := NewScript(Config{
			AnnounceCmd: "echo announce {ip}",
			WithdrawCmd: "echo withdraw {ip}",
			StatusCmd:   "",
			Community:   "65535:666",
			NextHop:     "192.0.2.1",
			PeerAddress: "192.0.2.2",
			PeerAS:      64500,
			LocalAS:     64500,
		}, &emptyBlockStore{})
		if err != nil {
			t.Fatalf("NewScript: %v", err)
		}
		defer sb.Stop()

		st, err := sb.Status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !st.Enabled {
			t.Error("expected Enabled=true for ScriptBlocker")
		}
		if st.State != "unknown" {
			t.Errorf("expected State=%q without status cmd, got %q", "unknown", st.State)
		}
		if st.PeerAS != 64500 {
			t.Errorf("expected PeerAS=64500, got %d", st.PeerAS)
		}
	})

	t.Run("with status cmd", func(t *testing.T) {
		sb, err := NewScript(Config{
			AnnounceCmd: "echo announce {ip}",
			WithdrawCmd: "echo withdraw {ip}",
			StatusCmd:   "echo established",
			Community:   "65535:666",
			NextHop:     "192.0.2.1",
			PeerAddress: "192.0.2.2",
			PeerAS:      64500,
			LocalAS:     64500,
		}, &emptyBlockStore{})
		if err != nil {
			t.Fatalf("NewScript: %v", err)
		}
		defer sb.Stop()

		st, err := sb.Status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if st.State != "established" {
			t.Errorf("expected State=%q, got %q", "established", st.State)
		}
	})
}

func TestScriptBlocker_CommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell commands require unix")
	}

	sb, err := NewScript(Config{
		AnnounceCmd: "false", // always exits 1
		WithdrawCmd: "echo withdraw {ip}",
		Community:   "65535:666",
		NextHop:     "192.0.2.1",
		PeerAddress: "192.0.2.2",
		PeerAS:      64500,
		LocalAS:     64500,
	}, &emptyBlockStore{})
	if err != nil {
		t.Fatalf("NewScript: %v", err)
	}
	defer sb.Stop()

	err = sb.Announce(context.Background(), net.ParseIP("198.51.100.3"), time.Hour, "should fail")
	if err == nil {
		t.Fatal("expected Announce to return an error when command fails")
	}

	// Verify the route was NOT added to the in-memory list
	list, _ := sb.List(context.Background())
	if len(list) != 0 {
		t.Errorf("expected 0 routes after failed Announce, got %d", len(list))
	}
}
