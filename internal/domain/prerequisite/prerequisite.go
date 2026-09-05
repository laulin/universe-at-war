// Package prerequisite expresses the requirement graph shared by buildings,
// researches and units without making those catalogues depend on each other.
package prerequisite

import (
	"errors"
	"fmt"
)

// Kind separates the catalogues a requirement may point at.
type Kind string

const (
	Building Kind = "building"
	Research Kind = "research"
)

var (
	ErrUnmet       = errors.New("prerequisite: requirements are not met")
	ErrCycle       = errors.New("prerequisite: requirement graph contains a cycle")
	ErrUnknownNode = errors.New("prerequisite: requirement references an unknown node")
)

// Node identifies one catalogue entry.
type Node struct {
	Kind Kind
	ID   string
}

// Requirement is a minimum completed level of one catalogue entry.
type Requirement struct {
	Kind  Kind
	ID    string
	Level int
}

// Node returns the graph node this requirement points at.
func (r Requirement) Node() Node {
	return Node{Kind: r.Kind, ID: r.ID}
}

// Levels maps stable identifiers of one catalogue to completed levels.
type Levels map[string]int

// State is everything an actor has completed.
type State struct {
	Buildings  Levels
	Researches Levels
}

// Level returns the completed level of one catalogue entry.
func (s State) Level(kind Kind, id string) int {
	switch kind {
	case Building:
		return s.Buildings[id]
	case Research:
		return s.Researches[id]
	default:
		return 0
	}
}

// Unmet lists the requirements the state does not satisfy, in catalogue order so
// the interface always presents them the same way.
func Unmet(requirements []Requirement, state State) []Requirement {
	var missing []Requirement
	for _, requirement := range requirements {
		if state.Level(requirement.Kind, requirement.ID) < requirement.Level {
			missing = append(missing, requirement)
		}
	}
	return missing
}

// Check reports the first unmet requirement, if any.
func Check(requirements []Requirement, state State) error {
	missing := Unmet(requirements, state)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s %s level %d", ErrUnmet, missing[0].Kind, missing[0].ID, missing[0].Level)
}

// ValidateGraph refuses a catalogue whose requirements are unknown or circular.
// Nodes map to the entries they depend on.
func ValidateGraph(edges map[Node][]Node) error {
	remaining := make(map[Node]int, len(edges))
	for node, dependencies := range edges {
		for _, dependency := range dependencies {
			if _, known := edges[dependency]; !known {
				return fmt.Errorf("%w: %s %s depends on %s %s", ErrUnknownNode, node.Kind, node.ID, dependency.Kind, dependency.ID)
			}
		}
		remaining[node] = len(dependencies)
	}
	ready := make([]Node, 0, len(edges))
	for node, count := range remaining {
		if count == 0 {
			ready = append(ready, node)
		}
	}
	resolved := 0
	for len(ready) > 0 {
		current := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		resolved++
		for node, dependencies := range edges {
			for _, dependency := range dependencies {
				if dependency != current {
					continue
				}
				remaining[node]--
				if remaining[node] == 0 {
					ready = append(ready, node)
				}
			}
		}
	}
	if resolved != len(edges) {
		return ErrCycle
	}
	return nil
}
