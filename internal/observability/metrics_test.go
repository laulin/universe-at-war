package observability

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMetricsCountWhatWentThroughAndNothingElse(t *testing.T) {
	metrics := NewMetrics()
	metrics.Request(200, 10*time.Millisecond)
	metrics.Request(200, 30*time.Millisecond)
	metrics.Request(500, 5*time.Millisecond)
	metrics.Event(3, false)
	metrics.Event(0, true)
	metrics.Reflection(2)

	snapshot := metrics.Read()
	if snapshot.Requests != 3 || snapshot.ServerFailures != 1 {
		t.Fatalf("requests = %d, failures = %d", snapshot.Requests, snapshot.ServerFailures)
	}
	if snapshot.AverageRequest != 15*time.Millisecond {
		t.Fatalf("average = %v, want 15ms", snapshot.AverageRequest)
	}
	if snapshot.Events != 3 || snapshot.EventFailures != 1 || snapshot.Reflections != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Statuses[200] != 2 || snapshot.Statuses[500] != 1 {
		t.Fatalf("statuses = %v", snapshot.Statuses)
	}

	rendered := snapshot.String()
	for _, expected := range []string{"requests 3", "server_failures 1", "events 3", "status_200 2"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("the counters do not report %q: %s", expected, rendered)
		}
	}
	// Nothing but counts: no path, no account, no token could ever land here.
	if strings.Contains(rendered, "/") || strings.Contains(rendered, "token") {
		t.Fatalf("the counters carry more than counts: %s", rendered)
	}

	// A nil set of counters is a no-op, so wiring stays optional.
	var absent *Metrics
	absent.Request(200, time.Second)
	absent.Event(1, true)
	absent.Reflection(1)
	if absent.Read().Requests != 0 {
		t.Fatal("a nil metrics set counted something")
	}
}

func TestMetricsAreSafeUnderConcurrency(t *testing.T) {
	metrics := NewMetrics()
	var waiting sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			for index := 0; index < 200; index++ {
				metrics.Request(200, time.Millisecond)
				metrics.Event(1, false)
			}
		}()
	}
	waiting.Wait()
	snapshot := metrics.Read()
	if snapshot.Requests != 1600 || snapshot.Events != 1600 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
