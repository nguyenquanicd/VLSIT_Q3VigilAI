package pipeline

import (
	"context"
	"hash/fnv"
	"runtime/debug"
	"sync"
	"time"

	"q3vnlaw/internal/store"
)

// Scheduler decides when to scan.
type Scheduler struct {
	Engine   *Engine
	Notifier *Notifier
	St       *store.Store
	Now      func() time.Time
	// Tick is how often the schedule is evaluated.
	Tick time.Duration
	// Retry is the minimum gap between two scheduled attempts, so a machine
	// without network does not retry every tick.
	Retry time.Duration

	mu          sync.Mutex
	nextAttempt time.Time
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// jitter spreads a source's interval by up to ±10% so scans do not all land
// on the hour. It is stable for one (source, last scan) pair, so the answer
// does not flip between two ticks.
func jitter(src store.Source) float64 {
	h := fnv.New32a()
	h.Write([]byte(src.LastScanAt))
	h.Write([]byte{byte(src.ID), byte(src.ID >> 8)})
	return 0.9 + 0.2*float64(h.Sum32()%1000)/1000
}

// Due returns the enabled sources whose interval has elapsed. catchup is
// true when a source is more than two intervals late, i.e. the machine was
// asleep or off: all missed rounds collapse into this single scan.
func (s *Scheduler) Due(now time.Time) (ids []int64, catchup bool) {
	list, err := s.St.Sources()
	if err != nil {
		return nil, false
	}
	for _, src := range list {
		if !src.Enabled {
			continue
		}
		interval := time.Duration(max(src.IntervalMinutes, 5)) * time.Minute
		last := store.ParseTime(src.LastScanAt)
		if last.IsZero() {
			ids = append(ids, src.ID)
			continue
		}
		late := now.Sub(last)
		if float64(late) >= float64(interval)*jitter(src) {
			ids = append(ids, src.ID)
			if late > 2*interval {
				catchup = true
			}
		}
	}
	return ids, catchup
}

// Step evaluates the schedule once and runs a scan when one is due. It
// returns true when a scan ran.
func (s *Scheduler) Step(ctx context.Context) bool {
	s.Notifier.Flush() // also delivers what quiet hours held back
	now := s.now()
	if !InWindow(now, s.St.Setting("active_start"), s.St.Setting("active_end")) || s.Engine.Running() {
		return false
	}
	s.mu.Lock()
	wait := now.Before(s.nextAttempt)
	s.mu.Unlock()
	if wait {
		return false
	}
	ids, catchup := s.Due(now)
	if len(ids) == 0 {
		return false
	}
	retry := s.Retry
	if retry <= 0 {
		retry = 5 * time.Minute
	}
	s.mu.Lock()
	s.nextAttempt = now.Add(retry)
	s.mu.Unlock()
	trigger := "schedule"
	if catchup {
		trigger = "catchup"
	}
	if _, err := s.Engine.Scan(ctx, trigger, ids); err != nil {
		return false
	}
	s.afterScan(true)
	return true
}

// Run evaluates the schedule until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick <= 0 {
		tick = 30 * time.Second
	}
	// A short delay lets the app finish starting before the first scan.
	select {
	case <-ctx.Done():
		return
	case <-time.After(min(tick, 10*time.Second)):
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		s.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ScanNow runs a manual scan of all enabled sources (or the given ones),
// ignoring the schedule and the active hours.
func (s *Scheduler) ScanNow(ctx context.Context, only []int64) (store.Scan, error) {
	sc, err := s.Engine.Scan(ctx, "manual", only)
	if err == nil {
		shown := s.Notifier.Flush()
		s.Notifier.ScanSummary(sc.AlertsNew, sc.SourcesFailed, shown)
		s.afterScan(true)
	}
	return sc, err
}

// afterScan announces what a scan found and hands the memory it used back
// to the operating system: the app then sits idle for an hour, and should
// not keep the scan's working memory while it does.
func (s *Scheduler) afterScan(system bool) {
	s.Notifier.Flush()
	if system {
		s.Notifier.SystemCheck()
	}
	debug.FreeOSMemory()
}

// Backfill applies a new or edited topic to the items already collected. It
// waits for a running scan to finish first, then announces what it found.
func (s *Scheduler) Backfill(ctx context.Context, topicID int64) {
	for i := 0; i < 120; i++ {
		_, err := s.Engine.Backfill(ctx, topicID)
		if err != ErrBusy {
			if err == nil {
				s.afterScan(false)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
