package building

import (
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/rules"
)

func TestCost(t *testing.T) {
	catalogue := DefaultCatalogue()
	tests := []struct {
		level int
		want  economy.Resources
	}{{1, economy.Resources{Metal: 60, Crystal: 15}}, {2, economy.Resources{Metal: 90, Crystal: 22}}}
	for _, test := range tests {
		got, err := catalogue.Cost(MetalMine, test.level, 1)
		if err != nil || got != test.want {
			t.Fatalf("Cost(level %d) = %#v, %v; want %#v", test.level, got, err, test.want)
		}
	}
}

func TestDuration(t *testing.T) {
	catalogue := DefaultCatalogue()
	cost := economy.Resources{Metal: 60, Crystal: 15}
	for _, test := range []struct {
		name     string
		robotics int
		speed    float64
		want     time.Duration
	}{{"base", 0, 1, 108 * time.Second}, {"robotics", 1, 1, 54 * time.Second}, {"minimum", 0, 1000, time.Second}} {
		t.Run(test.name, func(t *testing.T) {
			got, err := catalogue.Duration(cost, test.robotics, 0, test.speed)
			if err != nil || got != test.want {
				t.Fatalf("Duration() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestValidateStart(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	levels := Levels{RoboticsFactory: 9}
	if _, err := catalogue.Plan(NaniteFactory, levels, 20, 100, configured); err == nil {
		t.Fatal("missing prerequisite accepted")
	}
	levels[RoboticsFactory] = 10
	plan, err := catalogue.Plan(NaniteFactory, levels, 20, 100, configured)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetLevel != 1 || plan.Cost != (economy.Resources{Metal: 1_000_000, Crystal: 500_000, Deuterium: 100_000}) {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := catalogue.Plan(MetalMine, Levels{}, 1, 1, configured); err == nil {
		t.Fatal("full planet accepted")
	}
}
