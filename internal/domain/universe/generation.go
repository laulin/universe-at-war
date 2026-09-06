package universe

import (
	"errors"

	"universeatwar/internal/domain/random"
)

// Characteristics are the physical traits of a body, drawn once at creation and
// never changed afterwards.
type Characteristics struct {
	TotalFields        int
	MinimumTemperature int
	MaximumTemperature int
}

// temperatureSpread is the gap between the coldest and the warmest point of a
// planet, and temperatureStep how much one position closer to the star warms it.
const (
	temperatureSpread = 40
	temperatureStep   = 10
)

// Generate draws the traits of a new planet. The same coordinate and the same
// source always give the same planet, so a colonisation is replayable.
func Generate(at Coordinate, minimumFields, maximumFields, positionsPerSystem int, source random.Source) (Characteristics, error) {
	if minimumFields <= 0 || maximumFields < minimumFields || positionsPerSystem <= 0 {
		return Characteristics{}, errors.New("universe: invalid generation bounds")
	}
	if source == nil {
		return Characteristics{}, errors.New("universe: no randomness source")
	}
	centre := (positionsPerSystem + 1) / 2
	maximumTemperature := 40 - (at.Position-centre)*temperatureStep
	fields := minimumFields
	if spread := maximumFields - minimumFields + 1; spread > 1 {
		fields += source.IntN(spread)
	}
	return Characteristics{
		TotalFields:        fields,
		MinimumTemperature: maximumTemperature - temperatureSpread,
		MaximumTemperature: maximumTemperature,
	}, nil
}
