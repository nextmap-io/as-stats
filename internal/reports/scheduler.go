package reports

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// SchedulerStore is the persistence surface the scheduler needs.
type SchedulerStore interface {
	ListReportSchedules(ctx context.Context) ([]model.ReportSchedule, error)
	MarkReportRun(ctx context.Context, id string, ts time.Time) error
}

// Service runs the report scheduler loop: every minute it finds enabled, due
// schedules, generates + sends them, and stamps last_run_at. It also serves the
// "send test report now" path via SendNow.
type Service struct {
	store  SchedulerStore
	gen    *Generator
	sender *Sender

	mu    sync.Mutex
	fired map[string]time.Time // schedule ID → occurrence already sent (in-memory dedupe)
}

// NewService wires a scheduler Service.
func NewService(store SchedulerStore, gen *Generator, sender *Sender) *Service {
	return &Service{
		store:  store,
		gen:    gen,
		sender: sender,
		fired:  make(map[string]time.Time),
	}
}

// Run ticks every minute until ctx is cancelled, firing due schedules. It runs
// one pass immediately on start (catching up any missed occurrence).
func (svc *Service) Run(ctx context.Context) {
	log.Printf("reports: scheduler started")
	svc.runDue(ctx, time.Now().UTC())

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("reports: scheduler stopped")
			return
		case t := <-ticker.C:
			svc.runDue(ctx, t.UTC())
		}
	}
}

// runDue evaluates all schedules at `now` and fires those that are due.
func (svc *Service) runDue(ctx context.Context, now time.Time) {
	schedules, err := svc.store.ListReportSchedules(ctx)
	if err != nil {
		log.Printf("reports: list schedules failed: %v", err)
		return
	}
	for _, s := range schedules {
		if !s.Enabled {
			continue
		}
		if !isDue(s, now, s.LastRunAt) {
			continue
		}
		occ := mostRecentOccurrence(s, now)
		if svc.alreadyFired(s.ID, occ) {
			continue
		}

		if err := svc.deliver(ctx, s, occ); err != nil {
			log.Printf("reports: schedule %q (%s) delivery failed: %v", s.Name, s.ID, err)
			continue
		}
		svc.markFired(s.ID, occ)
		if err := svc.store.MarkReportRun(ctx, s.ID, now); err != nil {
			log.Printf("reports: mark run for %q failed: %v", s.ID, err)
		}
		log.Printf("reports: sent %q to configured recipients (occurrence %s)", s.Name, occ.Format(time.RFC3339))
	}
}

// deliver renders and sends a schedule for the given evaluation time.
func (svc *Service) deliver(ctx context.Context, s model.ReportSchedule, at time.Time) error {
	recipients, err := ParseRecipients(s.Recipients)
	if err != nil {
		return err
	}
	rendered, err := svc.gen.Generate(ctx, s, at)
	if err != nil {
		return err
	}
	return svc.sender.Send(recipients, rendered, s.Format)
}

// SendNow renders and sends a schedule immediately (used by the test endpoint).
// It does not touch last_run_at or the in-memory dedupe map.
func (svc *Service) SendNow(ctx context.Context, s model.ReportSchedule) error {
	return svc.deliver(ctx, s, time.Now().UTC())
}

func (svc *Service) alreadyFired(id string, occ time.Time) bool {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	prev, ok := svc.fired[id]
	return ok && prev.Equal(occ)
}

func (svc *Service) markFired(id string, occ time.Time) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	svc.fired[id] = occ
}
