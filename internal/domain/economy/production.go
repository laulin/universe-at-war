package economy

import (
	"errors"
	"fmt"
	"math"
	"time"

	"universeatwar/internal/domain/rules"
)

const secondsPerHour int64 = 3600

// Remainders retains fractional units as unit-seconds, each in [0, 3600).
type Remainders struct {
	Metal     int64
	Crystal   int64
	Deuterium int64
}

// ProductionState is the complete state needed for deterministic settlement.
type ProductionState struct {
	Stock      Resources
	Remainder  Remainders
	ProducedAt time.Time
}

// Rates contains whole units produced per hour.
type Rates struct {
	Metal     int64
	Crystal   int64
	Deuterium int64
}

// Levels are the economy-related building levels.
type Levels struct {
	MetalMine            int
	CrystalMine          int
	DeuteriumSynthesizer int
	SolarPlant           int
	MetalStorage         int
	CrystalStorage       int
	DeuteriumTank        int
	SolarSatellites      int
}

// Energy is the instantaneous production and consumption capacity.
type Energy struct {
	Produced int64
	Consumed int64
}

// Settle advances stored resources to now using whole-second precision.
func Settle(state ProductionState, now time.Time, rates Rates, capacities Resources) (ProductionState, error) {
	now = now.UTC().Truncate(time.Second)
	state.ProducedAt = state.ProducedAt.UTC().Truncate(time.Second)
	if state.ProducedAt.IsZero() {
		return ProductionState{}, errors.New("economy: production timestamp is required")
	}
	if now.Before(state.ProducedAt) {
		return ProductionState{}, errors.New("economy: production time cannot move backwards")
	}
	if err := state.Stock.Validate(); err != nil {
		return ProductionState{}, err
	}
	if err := capacities.Validate(); err != nil || capacities.Metal == 0 || capacities.Crystal == 0 || capacities.Deuterium == 0 {
		return ProductionState{}, errors.New("economy: capacities must be positive")
	}
	if rates.Metal < 0 || rates.Crystal < 0 || rates.Deuterium < 0 {
		return ProductionState{}, errors.New("economy: rates cannot be negative")
	}
	if state.Remainder.Metal < 0 || state.Remainder.Metal >= secondsPerHour ||
		state.Remainder.Crystal < 0 || state.Remainder.Crystal >= secondsPerHour ||
		state.Remainder.Deuterium < 0 || state.Remainder.Deuterium >= secondsPerHour {
		return ProductionState{}, errors.New("economy: invalid production remainder")
	}
	elapsed := int64(now.Sub(state.ProducedAt) / time.Second)
	metal, metalRemainder, err := settleOne(state.Stock.Metal, state.Remainder.Metal, rates.Metal, capacities.Metal, elapsed)
	if err != nil {
		return ProductionState{}, err
	}
	crystal, crystalRemainder, err := settleOne(state.Stock.Crystal, state.Remainder.Crystal, rates.Crystal, capacities.Crystal, elapsed)
	if err != nil {
		return ProductionState{}, err
	}
	deuterium, deuteriumRemainder, err := settleOne(state.Stock.Deuterium, state.Remainder.Deuterium, rates.Deuterium, capacities.Deuterium, elapsed)
	if err != nil {
		return ProductionState{}, err
	}
	state.Stock = Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium}
	state.Remainder = Remainders{Metal: metalRemainder, Crystal: crystalRemainder, Deuterium: deuteriumRemainder}
	state.ProducedAt = now
	return state, nil
}

func settleOne(stock, remainder, rate, capacity, elapsed int64) (int64, int64, error) {
	if stock > capacity {
		stock = capacity
	}
	if elapsed == 0 || rate == 0 {
		return stock, remainder, nil
	}
	if rate > (math.MaxInt64-remainder)/elapsed {
		return 0, 0, errors.New("economy: production overflow")
	}
	numerator := rate*elapsed + remainder
	whole := numerator / secondsPerHour
	newRemainder := numerator % secondsPerHour
	if whole >= capacity-stock {
		return capacity, newRemainder, nil
	}
	return stock + whole, newRemainder, nil
}

// CalculateRates calculates current hourly production and the energy balance.
func CalculateRates(configured rules.Ruleset, levels Levels, maximumTemperature int) (Rates, Energy, error) {
	if err := configured.Validate(); err != nil {
		return Rates{}, Energy{}, err
	}
	if levels.MetalMine < 0 || levels.CrystalMine < 0 || levels.DeuteriumSynthesizer < 0 || levels.SolarPlant < 0 || levels.SolarSatellites < 0 {
		return Rates{}, Energy{}, errors.New("economy: building levels cannot be negative")
	}
	growth := configured.Economy.MineProductionGrowth
	metalMine, err := floored(30 * float64(levels.MetalMine) * math.Pow(growth, float64(levels.MetalMine)) * configured.Time.EconomySpeed)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	crystalMine, err := floored(20 * float64(levels.CrystalMine) * math.Pow(growth, float64(levels.CrystalMine)) * configured.Time.EconomySpeed)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	temperatureFactor := math.Max(0, 1.44-0.004*float64(maximumTemperature))
	deuteriumMine, err := floored(10 * float64(levels.DeuteriumSynthesizer) * math.Pow(growth, float64(levels.DeuteriumSynthesizer)) * configured.Time.EconomySpeed * temperatureFactor)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	consumed, err := sumFlooredEnergy(levels, configured.Economy.EnergyConsumptionGrowth)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	produced, err := SolarPlantEnergy(levels.SolarPlant)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	satellites, err := floored(float64(levels.SolarSatellites) * satelliteEnergy(maximumTemperature))
	if err != nil {
		return Rates{}, Energy{}, err
	}
	if produced > math.MaxInt64-satellites {
		return Rates{}, Energy{}, errors.New("economy: energy overflow")
	}
	produced += satellites
	factor := 1.0
	if consumed > 0 && produced < consumed {
		factor = math.Max(configured.Economy.LowEnergyProduction, float64(produced)/float64(consumed))
	}
	baseMetal, err := floored(configured.Economy.BaseMetalPerHour * configured.Time.EconomySpeed)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	baseCrystal, err := floored(configured.Economy.BaseCrystalPerHour * configured.Time.EconomySpeed)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	baseDeuterium, err := floored(configured.Economy.BaseDeuteriumPerHour * configured.Time.EconomySpeed)
	if err != nil {
		return Rates{}, Energy{}, err
	}
	return Rates{
		Metal:     baseMetal + int64(math.Floor(float64(metalMine)*factor)),
		Crystal:   baseCrystal + int64(math.Floor(float64(crystalMine)*factor)),
		Deuterium: baseDeuterium + int64(math.Floor(float64(deuteriumMine)*factor)),
	}, Energy{Produced: produced, Consumed: consumed}, nil
}

// satelliteEnergy is the energy one solar satellite produces, bounded so a very
// hot or very cold planet stays within the classic range.
func satelliteEnergy(maximumTemperature int) float64 {
	energy := math.Floor((float64(maximumTemperature) + 160) / 6)
	return math.Min(50, math.Max(0, energy))
}

// MineEnergy is the energy one mine draws at a level. The three mines share the
// formula, and it is the only thing on a planet that consumes energy.
func MineEnergy(level int, growth float64) (int64, error) {
	if level < 0 {
		return 0, errors.New("economy: building levels cannot be negative")
	}
	return floored(10 * float64(level) * math.Pow(growth, float64(level)))
}

// SolarPlantEnergy is the energy a solar plant yields at a level. Its growth is
// part of the formula rather than of the ruleset, as the classic rules have it.
func SolarPlantEnergy(level int) (int64, error) {
	if level < 0 {
		return 0, errors.New("economy: building levels cannot be negative")
	}
	return floored(20 * float64(level) * math.Pow(1.1, float64(level)))
}

func sumFlooredEnergy(levels Levels, growth float64) (int64, error) {
	var total int64
	for _, level := range []int{levels.MetalMine, levels.CrystalMine, levels.DeuteriumSynthesizer} {
		value, err := MineEnergy(level, growth)
		if err != nil || value > math.MaxInt64-total {
			return 0, errors.New("economy: energy overflow")
		}
		total += value
	}
	return total, nil
}

// Capacity returns the configured base storage doubled for every level.
func Capacity(base int64, level int) (int64, error) {
	if base <= 0 || level < 0 || level >= 63 || base > math.MaxInt64>>level {
		return 0, errors.New("economy: invalid or overflowing storage capacity")
	}
	return base << level, nil
}

func floored(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxInt64 {
		return 0, fmt.Errorf("economy: non-representable value %v", value)
	}
	return int64(math.Floor(value)), nil
}
