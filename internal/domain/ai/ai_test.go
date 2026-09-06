package ai

import (
	"testing"
	"time"
)

func TestArchetypesAreDistinctAndComplete(t *testing.T) {
	seen := map[Preferences]Archetype{}
	for _, archetype := range Archetypes() {
		if !archetype.Valid() {
			t.Fatalf("%s is listed but unknown", archetype)
		}
		weights := archetype.Preferences()
		if weights.Economy <= 0 || weights.Economy > 1 || weights.SafetyMargin < 1 ||
			weights.Probes <= 0 || weights.DefenceShare < 0 || weights.DefenceShare > 1 {
			t.Fatalf("%s has unusable weights: %+v", archetype, weights)
		}
		if other, clash := seen[weights]; clash {
			t.Fatalf("%s and %s are the same character", archetype, other)
		}
		seen[weights] = archetype
	}
	if len(Archetypes()) != 8 {
		t.Fatalf("archetypes = %d, want the eight of the specification", len(Archetypes()))
	}
	// An unknown archetype falls back on the most careful character.
	if Archetype("bulldozer").Valid() {
		t.Fatal("an unknown archetype passed for known")
	}
	if Archetype("bulldozer").Preferences() != CautiousMiner.Preferences() {
		t.Fatal("an unknown archetype must behave like the cautious miner")
	}
	// The raider strikes on thinner evidence than the turtle, by construction.
	if Raider.Preferences().RaidThreshold >= Turtle.Preferences().RaidThreshold {
		t.Fatal("a raider hesitates more than a turtle")
	}
}

func TestActivityWindowSleepsAndWakesUp(t *testing.T) {
	day := func(hour int) time.Time {
		return time.Date(2042, time.September, 10, hour, 30, 0, 0, time.UTC)
	}
	cases := []struct {
		name   string
		window Window
		hour   int
		awake  bool
		next   time.Time
	}{
		{"before opening", Window{Start: 8, End: 23}, 6, false, time.Date(2042, time.September, 10, 8, 0, 0, 0, time.UTC)},
		{"inside", Window{Start: 8, End: 23}, 12, true, day(12)},
		{"after closing", Window{Start: 8, End: 23}, 23, false, time.Date(2042, time.September, 11, 8, 0, 0, 0, time.UTC)},
		{"across midnight, late", Window{Start: 22, End: 4}, 23, true, day(23)},
		{"across midnight, early", Window{Start: 22, End: 4}, 2, true, day(2)},
		{"across midnight, closed", Window{Start: 22, End: 4}, 10, false, time.Date(2042, time.September, 10, 22, 0, 0, 0, time.UTC)},
		{"always open", Window{Start: 0, End: 0}, 3, true, day(3)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			at := day(testCase.hour)
			if got := testCase.window.Awake(at); got != testCase.awake {
				t.Fatalf("Awake(%v) = %v, want %v", at, got, testCase.awake)
			}
			if got := testCase.window.NextOpening(at); !got.Equal(testCase.next) {
				t.Fatalf("NextOpening(%v) = %v, want %v", at, got, testCase.next)
			}
		})
	}
	if (Window{Start: -1, End: 5}).Valid() || (Window{Start: 0, End: 24}).Valid() {
		t.Fatal("an impossible hour passed for valid")
	}
}

func TestNextThinkIsSpreadYetReproducible(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	interval := 5 * time.Minute
	first := NextThink(now, interval, 7, 1)
	if !first.Equal(NextThink(now, interval, 7, 1)) {
		t.Fatal("the same seed and tick gave two different schedules")
	}
	if first.Equal(NextThink(now, interval, 7, 2)) {
		t.Fatal("two consecutive ticks fall on the very same instant")
	}
	if first.Equal(NextThink(now, interval, 8, 1)) {
		t.Fatal("two different players think in lockstep")
	}
	// The jitter never leaves the documented band.
	for tick := int64(0); tick < 500; tick++ {
		delay := NextThink(now, interval, 7, tick).Sub(now)
		if delay < time.Duration(float64(interval)*(1-JitterAmplitude))-time.Second ||
			delay > time.Duration(float64(interval)*(1+JitterAmplitude))+time.Second {
			t.Fatalf("tick %d fell outside the jitter band: %v", tick, delay)
		}
	}
	if NextThink(now, 0, 7, 1).Before(now.Add(time.Second)) {
		t.Fatal("an absent interval must still move time forward")
	}
}

func TestFreshnessDecaysAndExpires(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	recent := time.Hour
	cases := []struct {
		age  time.Duration
		want float64
	}{
		{0, 1},
		{recent, 1},
		{recent * 2, 1 - 1.0/11},
		{recent * StaleFactor, 0},
		{recent * 100, 0},
	}
	for _, testCase := range cases {
		got := Fresh(now.Add(-testCase.age), now, recent)
		if got < testCase.want-1e-9 || got > testCase.want+1e-9 {
			t.Fatalf("Fresh(age=%v) = %v, want %v", testCase.age, got, testCase.want)
		}
	}
	if Fresh(now.Add(time.Hour), now, recent) != 0 {
		t.Fatal("an observation from the future is worth nothing")
	}
	if Fresh(now, now, 0) != 0 {
		t.Fatal("without a recency threshold nothing can be trusted")
	}
}

func TestProfileRefusesWhatItCannotRun(t *testing.T) {
	valid := Profile{Archetype: Raider, Window: Window{Start: 8, End: 23}, Interval: time.Minute}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	broken := valid
	broken.Archetype = "bulldozer"
	if err := broken.Validate(); err != ErrUnknownArchetype {
		t.Fatalf("unknown archetype error = %v", err)
	}
	broken = valid
	broken.Window = Window{Start: 8, End: 99}
	if err := broken.Validate(); err != ErrInvalidWindow {
		t.Fatalf("impossible window error = %v", err)
	}
	broken = valid
	broken.Interval = 0
	if err := broken.Validate(); err != ErrInvalidInterval {
		t.Fatalf("absent interval error = %v", err)
	}
}
