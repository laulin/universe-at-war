package prerequisite

import (
	"errors"
	"strings"
	"testing"
)

func TestUnmetReportsMissingRequirementsInOrder(t *testing.T) {
	state := State{
		Buildings:  Levels{"research_lab": 3},
		Researches: Levels{"energy_technology": 1},
	}
	requirements := []Requirement{
		{Kind: Building, ID: "research_lab", Level: 3},
		{Kind: Research, ID: "energy_technology", Level: 2},
		{Kind: Research, ID: "laser_technology", Level: 1},
	}

	if missing := Unmet(requirements[:1], state); len(missing) != 0 {
		t.Fatalf("Unmet() = %v, want none", missing)
	}
	missing := Unmet(requirements, state)
	if len(missing) != 2 || missing[0].ID != "energy_technology" || missing[1].ID != "laser_technology" {
		t.Fatalf("Unmet() = %v", missing)
	}

	err := Check(requirements, state)
	if !errors.Is(err, ErrUnmet) || !strings.Contains(err.Error(), "energy_technology") {
		t.Fatalf("Check() error = %v", err)
	}
	if err := Check(requirements[:1], state); err != nil {
		t.Fatalf("Check() on satisfied requirements error = %v", err)
	}
}

func TestValidateGraphRejectsUnknownNodesSelfEdgesAndCycles(t *testing.T) {
	node := func(id string) Node { return Node{Kind: Research, ID: id} }
	tests := []struct {
		name  string
		edges map[Node][]Node
		want  error
	}{
		{
			name:  "acyclic graph",
			edges: map[Node][]Node{node("a"): {node("b")}, node("b"): {node("c")}, node("c"): nil},
		},
		{
			name:  "unknown dependency",
			edges: map[Node][]Node{node("a"): {node("missing")}},
			want:  ErrUnknownNode,
		},
		{
			name:  "self edge",
			edges: map[Node][]Node{node("a"): {node("a")}},
			want:  ErrCycle,
		},
		{
			name:  "cycle",
			edges: map[Node][]Node{node("a"): {node("b")}, node("b"): {node("c")}, node("c"): {node("a")}},
			want:  ErrCycle,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateGraph(test.edges)
			if test.want == nil {
				if err != nil {
					t.Fatalf("ValidateGraph() error = %v, want none", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("ValidateGraph() error = %v, want %v", err, test.want)
			}
		})
	}
}
