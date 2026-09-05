// Package simulation runs the durable scheduled-event loop without busy waiting.
package simulation

import (
	"context"
	"errors"
	"time"

	domainclock "universeatwar/internal/domain/clock"
)

type Processor interface {
	CompleteDue(context.Context, int) (int, error)
	NextDue(context.Context) (time.Time, bool, error)
}

// Worker sleeps until the nearest known event, a wake-up, or a safety rescan.
type Worker struct {
	Clock          domainclock.Clock
	Processor      Processor
	BatchSize      int
	RescanInterval time.Duration
	wake           chan struct{}
}

func NewWorker(clock domainclock.Clock, processor Processor) *Worker {
	return &Worker{Clock: clock, Processor: processor, BatchSize: 100, RescanInterval: 30 * time.Second, wake: make(chan struct{}, 1)}
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
	for {
		for {
			processed, err := w.Processor.CompleteDue(ctx, w.BatchSize)
			if err != nil {
				return err
			}
			if processed < w.BatchSize {
				break
			}
		}
		delay := w.RescanInterval
		if dueAt, ok, err := w.Processor.NextDue(ctx); err != nil {
			return err
		} else if ok {
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
