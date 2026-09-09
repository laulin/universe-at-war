package ai

import (
	"testing"
	"time"
)

func referenceInstant() time.Time {
	return time.Date(2042, time.September, 10, 11, 0, 0, 0, time.UTC)
}

func TestAnAllianceAskedToActAsOneAlwaysAnswers(t *testing.T) {
	for tick := int64(0); tick < 50; tick++ {
		if !Cooperates(1234, tick, 1) {
			t.Fatalf("a fully coordinated member sat out tick %d", tick)
		}
		if Cooperates(1234, tick, 0) {
			t.Fatalf("an uncoordinated member answered at tick %d", tick)
		}
	}
	// Degrees outside the ratio are read as the nearest end rather than refused.
	if !Cooperates(1234, 0, 1.5) || Cooperates(1234, 0, -1) {
		t.Fatal("a degree outside the ratio was not read as the nearest end")
	}
}

func TestTheSameReflectionAlwaysAnswersTheSameWay(t *testing.T) {
	for tick := int64(0); tick < 20; tick++ {
		first := Cooperates(99, tick, .5)
		for again := 0; again < 5; again++ {
			if Cooperates(99, tick, .5) != first {
				t.Fatalf("tick %d answered two ways", tick)
			}
		}
	}
}

// TestTheDegreeIsTheShareOfCallsAnswered is what the setting promises an
// administrator: half means about half.
func TestTheDegreeIsTheShareOfCallsAnswered(t *testing.T) {
	for _, degree := range []float64{.25, .5, .75} {
		answered := 0
		const draws = 2000
		for tick := int64(0); tick < draws; tick++ {
			if Cooperates(4242, tick, degree) {
				answered++
			}
		}
		share := float64(answered) / draws
		if share < degree-.05 || share > degree+.05 {
			t.Fatalf("a degree of %.2f answered %.3f of its calls", degree, share)
		}
	}
}

// TestCooperationIsNotTiedToTheScheduleJitter guards the one thing that would
// quietly ruin the draw: both are taken from the same seed and tick.
func TestCooperationIsNotTiedToTheScheduleJitter(t *testing.T) {
	same := 0
	const draws = 500
	for tick := int64(0); tick < draws; tick++ {
		// The jitter of this very tick, reduced to the same coin.
		jitter := NextThink(referenceInstant(), 10*time.Minute, 7, tick)
		early := jitter.Unix()%2 == 0
		if early == Cooperates(7, tick, .5) {
			same++
		}
	}
	if share := float64(same) / draws; share > .60 || share < .40 {
		t.Fatalf("cooperation follows the schedule jitter: they agreed %.2f of the time", share)
	}
}
