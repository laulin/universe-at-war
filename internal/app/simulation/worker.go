// Package simulation runs the durable scheduled-event loop without busy waiting.
package simulation

import (
	"context"
	"errors"
	"log/slog"
	"time"

	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/observability"
)

type Processor interface {
	CompleteDue(context.Context, int) (int, error)
	NextDue(context.Context) (time.Time, bool, error)
}

// Thinker runs the reflections the artificial players owe. It acts through the
// ordinary player use cases, which open their own transactions, so it runs
// after the batch of events rather than inside one.
type Thinker interface {
	ThinkDue(context.Context, int) (int, error)
}

// Worker sleeps until the nearest known event, a wake-up, or a safety rescan.
type Worker struct {
	Clock          domainclock.Clock
	Processor      Processor
	Thinker        Thinker
	BatchSize      int
	RescanInterval time.Duration
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	wake           chan struct{}
}

func NewWorker(clock domainclock.Clock, processor Processor) *Worker {
	return &Worker{
		Clock:          clock,
		Processor:      processor,
		BatchSize:      100,
		RescanInterval: 30 * time.Second,
		Logger:         slog.New(slog.DiscardHandler),
		wake:           make(chan struct{}, 1),
	}
}

func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Clock == nil || w.Processor == nil || w.BatchSize <= 0 || w.RescanInterval <= 0 || w.wake == nil {
		return errors.New("simulation: incomplete worker dependencies")
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	for {
		// A batch that fails is logged and retried at the next wake-up: a
		// transient database error must not stop the simulation for good.
		failed := false
		for !failed {
			processed, err := w.Processor.CompleteDue(ctx, w.BatchSize)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				logger.Error("simulation batch failed", "error", err.Error())
				failed = true
				break
			}
			if processed < w.BatchSize {
				break
			}
		}
		// Once the world has settled, whoever owes a reflection takes it. A
		// failure here costs one tick, never the loop.
		if w.Thinker != nil && !failed {
			if thought, err := w.Thinker.ThinkDue(ctx, w.BatchSize); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				logger.Error("artificial reflection failed", "error", err.Error())
			} else {
				w.Metrics.Reflection(thought)
			}
		}
		delay := w.RescanInterval
		if dueAt, ok, err := w.Processor.NextDue(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			logger.Error("simulation schedule unavailable", "error", err.Error())
		} else if ok && !failed {
			untilDue := dueAt.Sub(w.Clock.Now().UTC())
			if untilDue < 0 {
				untilDue = 0
			}
			if untilDue < delay {
				delay = untilDue
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-w.wake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}
}
