package simulation

import (
	"context"
	"sync"
	"testing"
	"time"

	appclock "universeatwar/internal/clock"
)

type processorStub struct {
	mu        sync.Mutex
	processed int
	due       time.Time
	calls     chan struct{}
}

func (p *processorStub) CompleteDue(context.Context, int) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.processed++
	select {
	case p.calls <- struct{}{}:
	default:
	}
	return 0, nil
}

func (p *processorStub) NextDue(context.Context) (time.Time, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.due, !p.due.IsZero(), nil
}

func TestWorkerCanBeWokenAndStops(t *testing.T) {
	processor := &processorStub{calls: make(chan struct{}, 4)}
	worker := NewWorker(appclock.NewFake(time.Now()), processor)
	worker.RescanInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	<-processor.calls
	worker.Wake()
	select {
	case <-processor.calls:
	case <-time.After(time.Second):
		t.Fatal("worker did not react to wake-up")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
