package server

import "testing"

func TestServerStateTransitions(t *testing.T) {
	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{name: "bootstrap starts setup", from: BootstrapPending, to: SetupInProgress, want: true},
		{name: "setup starts universe", from: SetupInProgress, to: Running, want: true},
		{name: "running pauses", from: Running, to: Paused, want: true},
		{name: "paused resumes", from: Paused, to: Running, want: true},
		{name: "running enters maintenance", from: Running, to: Maintenance, want: true},
		{name: "paused enters maintenance", from: Paused, to: Maintenance, want: true},
		{name: "maintenance restores running", from: Maintenance, to: Running, want: true},
		{name: "maintenance restores paused", from: Maintenance, to: Paused, want: true},
		{name: "cannot return to bootstrap", from: Running, to: BootstrapPending, want: false},
		{name: "cannot skip setup", from: BootstrapPending, to: Running, want: false},
		{name: "same state is not a transition", from: Running, to: Running, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("CanTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestStateValidation(t *testing.T) {
	for _, state := range []State{BootstrapPending, SetupInProgress, Running, Paused, Maintenance} {
		if err := state.Validate(); err != nil {
			t.Errorf("State(%q).Validate() error = %v", state, err)
		}
	}

	if err := State("unknown").Validate(); err == nil {
		t.Fatal("State(unknown).Validate() error = nil, want error")
	}
}
