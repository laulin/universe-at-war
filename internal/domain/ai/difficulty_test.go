package ai

import "testing"

func TestAnEasyUniverseMakesTimiderPlayersAndAHardOneBolder(t *testing.T) {
	for _, archetype := range Archetypes() {
		t.Run(string(archetype), func(t *testing.T) {
			normal := archetype.Preferences().At(Normal)
			easy := archetype.Preferences().At(Easy)
			hard := archetype.Preferences().At(Hard)

			// An easy player wants a wider margin, fears more, expects less and
			// looks less closely, so it goes out less often and worse informed.
			if !(easy.SafetyMargin > normal.SafetyMargin && easy.Caution > normal.Caution &&
				easy.RaidThreshold > normal.RaidThreshold && easy.Greed < normal.Greed) {
				t.Fatalf("easy is not timider than normal: %+v against %+v", easy, normal)
			}
			if !(hard.SafetyMargin < normal.SafetyMargin && hard.Caution < normal.Caution &&
				hard.RaidThreshold < normal.RaidThreshold && hard.Greed > normal.Greed) {
				t.Fatalf("hard is not bolder than normal: %+v against %+v", hard, normal)
			}
			if easy.Probes >= normal.Probes || hard.Probes <= normal.Probes {
				t.Fatalf("probes did not follow the difficulty: %d, %d, %d", easy.Probes, normal.Probes, hard.Probes)
			}
			if easy.Probes < 1 {
				t.Fatalf("an easy player was left unable to look at all: %d probes", easy.Probes)
			}
		})
	}
}

// TestDifficultyLeavesWhatIsNotItsBusinessAlone guards the two deliberate
// omissions: how fast a player develops belongs to its character, and the
// fleetsave is a rule of playing properly rather than a dial.
func TestDifficultyLeavesWhatIsNotItsBusinessAlone(t *testing.T) {
	for _, archetype := range Archetypes() {
		reference := archetype.Preferences()
		for _, difficulty := range Difficulties() {
			bent := archetype.Preferences().At(difficulty)
			if bent.Economy != reference.Economy || bent.DefenceShare != reference.DefenceShare {
				t.Fatalf("%s at %s changed how it develops: %+v", archetype, difficulty, bent)
			}
			if bent.Fleetsave != reference.Fleetsave {
				t.Fatalf("%s at %s changed its fleetsave: %s", archetype, difficulty, bent.Fleetsave)
			}
		}
	}
}

func TestNormalAndUnreadableDifficultiesLeaveACharacterAsItIs(t *testing.T) {
	for _, archetype := range Archetypes() {
		reference := archetype.Preferences()
		for _, difficulty := range []Difficulty{Normal, "", "impossible"} {
			if bent := archetype.Preferences().At(difficulty); bent != reference {
				t.Fatalf("%s at %q = %+v, want it untouched", archetype, difficulty, bent)
			}
		}
	}
}

func TestOnlyTheThreeKnownDifficultiesAreValid(t *testing.T) {
	for _, difficulty := range Difficulties() {
		if !difficulty.Valid() {
			t.Fatalf("Difficulties() offers %q, which Valid() refuses", difficulty)
		}
	}
	for _, unknown := range []Difficulty{"", "impossible", "EASY"} {
		if unknown.Valid() {
			t.Fatalf("Valid() accepted %q", unknown)
		}
	}
}

// TestAProfileCarriesTheDifficultyIntoItsPreferences proves the brain needs no
// change: every place it asks a profile what it prefers now gets the answer the
// universe asked for.
func TestAProfileCarriesTheDifficultyIntoItsPreferences(t *testing.T) {
	profile := Profile{Archetype: Raider, Difficulty: Hard}
	if profile.Preferences() != Raider.Preferences().At(Hard) {
		t.Fatalf("a profile did not carry its difficulty: %+v", profile.Preferences())
	}
	plain := Profile{Archetype: Raider}
	if plain.Preferences() != Raider.Preferences() {
		t.Fatalf("a profile without a difficulty did not play as normal: %+v", plain.Preferences())
	}
}

func TestAProfileCanOverrideItsArchetype(t *testing.T) {
	custom := Raider.Tuning()
	custom.Greed = 2.5
	custom.SearchRadius = 25
	profile := Profile{Archetype: CautiousMiner, Custom: &custom}
	if profile.Preferences().Greed != 2.5 || profile.Behaviour().SearchRadius != 25 {
		t.Fatalf("custom behaviour was ignored: %+v", profile.Behaviour())
	}
	custom.SearchRadius = 0
	if err := custom.Validate(); err == nil {
		t.Fatal("an unbounded custom map scan was accepted")
	}
}
