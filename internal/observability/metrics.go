package observability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics counts what a local operator needs to see: how much work went
// through, how long it took and how often something failed. It holds no secret
// and no personal data, only counts.
type Metrics struct {
	requests    atomic.Int64
	requestNs   atomic.Int64
	serverFail  atomic.Int64
	events      atomic.Int64
	eventFail   atomic.Int64
	reflections atomic.Int64
	recruits    atomic.Int64

	mu       sync.Mutex
	statuses map[int]int64
}

// NewMetrics builds an empty set of counters.
func NewMetrics() *Metrics {
	return &Metrics{statuses: map[int]int64{}}
}

// Request records one served request.
func (m *Metrics) Request(status int, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.requests.Add(1)
	m.requestNs.Add(int64(elapsed))
	if status >= 500 {
		m.serverFail.Add(1)
	}
	m.mu.Lock()
	m.statuses[status]++
	m.mu.Unlock()
}

// Event records scheduled events applied and failures met.
func (m *Metrics) Event(applied int, failed bool) {
	if m == nil {
		return
	}
	if applied > 0 {
		m.events.Add(int64(applied))
	}
	if failed {
		m.eventFail.Add(1)
	}
}

// Reflection records one reflection of an artificial player.
func (m *Metrics) Reflection(count int) {
	if m == nil || count <= 0 {
		return
	}
	m.reflections.Add(int64(count))
}

// Recruit records artificial players brought into the universe.
func (m *Metrics) Recruit(count int) {
	if m == nil || count <= 0 {
		return
	}
	m.recruits.Add(int64(count))
}

// Snapshot is the readable state of the counters.
type Snapshot struct {
	Requests       int64
	AverageRequest time.Duration
	ServerFailures int64
	Events         int64
	EventFailures  int64
	Reflections    int64
	Recruits       int64
	Statuses       map[int]int64
}

// Read takes a consistent enough picture of the counters for a dashboard.
func (m *Metrics) Read() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{
		Requests:       m.requests.Load(),
		ServerFailures: m.serverFail.Load(),
		Events:         m.events.Load(),
		EventFailures:  m.eventFail.Load(),
		Reflections:    m.reflections.Load(),
		Recruits:       m.recruits.Load(),
		Statuses:       map[int]int64{},
	}
	if snapshot.Requests > 0 {
		snapshot.AverageRequest = time.Duration(m.requestNs.Load() / snapshot.Requests)
	}
	m.mu.Lock()
	for status, count := range m.statuses {
		snapshot.Statuses[status] = count
	}
	m.mu.Unlock()
	return snapshot
}

// String renders the counters as plain lines, which is all a local operator
// needs and all this build promises.
func (s Snapshot) String() string {
	statuses := make([]int, 0, len(s.Statuses))
	for status := range s.Statuses {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)
	var builder strings.Builder
	fmt.Fprintf(&builder, "requests %d\naverage_request_ms %.2f\nserver_failures %d\nevents %d\nevent_failures %d\nreflections %d\nrecruits %d\n",
		s.Requests, float64(s.AverageRequest)/float64(time.Millisecond), s.ServerFailures,
		s.Events, s.EventFailures, s.Reflections, s.Recruits)
	for _, status := range statuses {
		fmt.Fprintf(&builder, "status_%d %d\n", status, s.Statuses[status])
	}
	return builder.String()
}
