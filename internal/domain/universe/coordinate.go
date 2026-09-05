// Package universe defines topology values independently from persistence and presentation.
package universe

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Limits contains the inclusive coordinate bounds of a universe.
type Limits struct {
	Galaxies  int
	Systems   int
	Positions int
}

// Coordinate identifies one position using stable one-based indexes.
type Coordinate struct {
	Galaxy   int
	System   int
	Position int
}

// ParseCoordinate parses the canonical galaxy:system:position notation.
func ParseCoordinate(value string, limits Limits) (Coordinate, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return Coordinate{}, errors.New("universe: coordinate must have three parts")
	}
	values := [3]int{}
	for index, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return Coordinate{}, errors.New("universe: coordinate is not canonical")
		}
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return Coordinate{}, fmt.Errorf("universe: coordinate part %d is invalid", index+1)
		}
		values[index] = parsed
	}
	coordinate := Coordinate{Galaxy: values[0], System: values[1], Position: values[2]}
	if err := coordinate.Validate(limits); err != nil {
		return Coordinate{}, err
	}
	return coordinate, nil
}

// Validate checks a coordinate against configured bounds.
func (c Coordinate) Validate(limits Limits) error {
	if limits.Galaxies <= 0 || limits.Systems <= 0 || limits.Positions <= 0 {
		return errors.New("universe: coordinate limits must be positive")
	}
	if c.Galaxy < 1 || c.Galaxy > limits.Galaxies ||
		c.System < 1 || c.System > limits.Systems ||
		c.Position < 1 || c.Position > limits.Positions {
		return errors.New("universe: coordinate is outside topology")
	}
	return nil
}

func (c Coordinate) String() string {
	return fmt.Sprintf("%d:%d:%d", c.Galaxy, c.System, c.Position)
}
