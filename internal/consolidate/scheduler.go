package consolidate

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler runs consolidation on a timer.
type Scheduler struct {
	consolidator *Consolidator
	interval     time.Duration
	minMemories  int
	stopCh       chan struct{}
	logger       *slog.Logger
}

// NewScheduler creates a consolidation scheduler.
// Default: every 30 minutes, minimum 2 memories.
func NewScheduler(c *Consolidator, interval time.Duration, minMemories int, logger *slog.Logger) *Scheduler {
	if interval <= 0 { interval = 30 * time.Minute }
	if minMemories <= 0 { minMemories = 2 }
	if logger == nil { logger = slog.Default() }

	return &Scheduler{
		consolidator: c,
		interval:     interval,
		minMemories:  minMemories,
		stopCh:       make(chan struct{}),
		logger:       logger,
	}
}

// Start begins the consolidation timer in a background goroutine.
func (s *Scheduler) Start() {
	s.logger.Info("consolidation scheduler started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.run()
			case <-s.stopCh:
				ticker.Stop()
				s.logger.Info("consolidation scheduler stopped")
				return
			}
		}
	}()
}

// Stop stops the scheduler. Safe to call multiple times.
func (s *Scheduler) Stop() {
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}
}

func (s *Scheduler) run() {
	ctx := context.Background()

	count, err := s.consolidator.store.CountUnconsolidated()
	if err != nil {
		s.logger.Error("count unconsolidated failed", "error", err)
		return
	}

	if count < int64(s.minMemories) {
		s.logger.Info("skipping consolidation", "unconsolidated", count, "min_required", s.minMemories)
		return
	}

	s.logger.Info("running consolidation", "unconsolidated", count)

	result, err := s.consolidator.Consolidate(ctx)
	if err != nil {
		s.logger.Error("consolidation failed", "error", err)
		return
	}

	if result != nil {
		s.logger.Info("consolidation completed",
			"id", result.ID,
			"insight", result.Insight,
		)
	}
}
