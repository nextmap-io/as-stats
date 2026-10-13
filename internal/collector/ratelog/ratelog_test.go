package ratelog

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func TestLimiterSuppressesAndReports(t *testing.T) {
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})

	l := New(1, 50*time.Millisecond)
	for i := 0; i < 100; i++ {
		l.Printf("decode error %d", i)
	}
	time.Sleep(60 * time.Millisecond)
	l.Printf("decode error last")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2: %q", len(lines), lines)
	}
	if lines[0] != "decode error 0" {
		t.Errorf("first line = %q", lines[0])
	}
	if lines[1] != "decode error last (99 similar lines suppressed)" {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestLimiterBurst(t *testing.T) {
	var buf bytes.Buffer
	out, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
	})

	l := New(3, time.Hour)
	for i := 0; i < 10; i++ {
		l.Printf("template %d", i)
	}
	if n := strings.Count(buf.String(), "\n"); n != 3 {
		t.Fatalf("logged %d lines, want the burst of 3", n)
	}
}
