package economy

import (
	"testing"
	"time"

	"universeatwar/internal/domain/rules"
)

func TestSettleTwoHoursOffline(t *testing.T) {
	start := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	state := ProductionState{Stock: Resources{Metal: 500, Crystal: 500}, ProducedAt: start}
	got, err := Settle(state, start.Add(2*time.Hour), Rates{Metal: 30, Crystal: 15}, Resources{Metal: 10_000, Crystal: 10_000, Deuterium: 10_000})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stock != (Resources{Metal: 560, Crystal: 530}) {
		t.Fatalf("stock = %#v", got.Stock)
	}
}

func TestSettleKeepsFractionAcrossReads(t *testing.T) {
	start := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	state := ProductionState{ProducedAt: start}
	capacities := Resources{Metal: 10, Crystal: 10, Deuterium: 10}
	one, err := Settle(state, start.Add(30*time.Minute), Rates{Metal: 1}, capacities)
	if err != nil {
		t.Fatal(err)
	}
	if one.Stock.Metal != 0 || one.Remainder.Metal != 1800 {
		t.Fatalf("first settlement = %#v", one)
	}
	two, err := Settle(one, start.Add(time.Hour), Rates{Metal: 1}, capacities)
	if err != nil {
		t.Fatal(err)
	}
	if two.Stock.Metal != 1 || two.Remainder.Metal != 0 {
		t.Fatalf("second settlement = %#v", two)
	}
}

func TestSettleCapsStockAndRejectsClockRollback(t *testing.T) {
	start := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	state := ProductionState{Stock: Resources{Metal: 9}, ProducedAt: start}
	got, err := Settle(state, start.Add(time.Hour), Rates{Metal: 100}, Resources{Metal: 10, Crystal: 10, Deuterium: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stock.Metal != 10 {
		t.Fatalf("metal = %d, want 10", got.Stock.Metal)
	}
	if _, err := Settle(got, start, Rates{}, Resources{Metal: 10, Crystal: 10, Deuterium: 10}); err == nil {
		t.Fatal("clock rollback accepted")
	}
}

func TestCalculateRatesAndEnergy(t *testing.T) {
	configured := rules.Default()
	levels := Levels{MetalMine: 1, CrystalMine: 1, DeuteriumSynthesizer: 1, SolarPlant: 1}
	rates, energy, err := CalculateRates(configured, levels, 40)
	if err != nil {
		t.Fatal(err)
	}
	if energy.Produced != 22 || energy.Consumed != 33 {
		t.Fatalf("energy = %#v", energy)
	}
	if rates.Metal != 52 || rates.Crystal != 29 || rates.Deuterium != 9 {
		t.Fatalf("rates = %#v", rates)
	}
}

func TestCapacity(t *testing.T) {
	for _, test := range []struct {
		level int
		want  int64
	}{{0, 10_000}, {1, 20_000}, {4, 160_000}} {
		got, err := Capacity(10_000, test.level)
		if err != nil || got != test.want {
			t.Fatalf("Capacity(%d) = %d, %v; want %d", test.level, got, err, test.want)
		}
	}
}

func BenchmarkSettleLazy(b *testing.B) {
	start := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)
	state := ProductionState{Stock: Resources{Metal: 500, Crystal: 500}, ProducedAt: start}
	rates := Rates{Metal: 12_345, Crystal: 6_789, Deuterium: 2_345}
	capacities := Resources{Metal: 1_000_000_000, Crystal: 1_000_000_000, Deuterium: 1_000_000_000}
	b.ReportAllocs()
	for range b.N {
		if _, err := Settle(state, start.Add(30*24*time.Hour), rates, capacities); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSolarSatellitesProduceEnergyFromTemperature(t *testing.T) {
	configured := rules.Default()
	tests := []struct {
		name        string
		satellites  int
		temperature int
		want        int64
	}{
		{name: "no satellite", satellites: 0, temperature: 40, want: 0},
		{name: "temperate planet", satellites: 10, temperature: 40, want: 330},
		{name: "frozen planet gives nothing", satellites: 10, temperature: -200, want: 0},
		{name: "scorching planet is capped", satellites: 2, temperature: 200, want: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, energy, err := CalculateRates(configured, Levels{SolarSatellites: test.satellites}, test.temperature)
			if err != nil {
				t.Fatalf("CalculateRates() error = %v", err)
			}
			if energy.Produced != test.want {
				t.Fatalf("energy produced = %d, want %d", energy.Produced, test.want)
			}
		})
	}

	_, energy, err := CalculateRates(configured, Levels{SolarPlant: 1, SolarSatellites: 1}, 40)
	if err != nil {
		t.Fatalf("CalculateRates() error = %v", err)
	}
	if energy.Produced != 22+33 {
		t.Fatalf("plant and satellites = %d, want 55", energy.Produced)
	}
	if _, _, err := CalculateRates(configured, Levels{SolarSatellites: -1}, 40); err == nil {
		t.Fatal("CalculateRates() accepted a negative satellite count")
	}
}
