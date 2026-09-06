package fleet

import (
	"errors"
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

func limitsOf(topology rules.TopologySettings) universe.Limits {
	return universe.Limits{Galaxies: topology.Galaxies, Systems: topology.SystemsPerGalaxy, Positions: topology.PositionsPerSystem}
}

func coordinate(t *testing.T, value string, topology rules.TopologySettings) universe.Coordinate {
	t.Helper()
	parsed, err := universe.ParseCoordinate(value, limitsOf(topology))
	if err != nil {
		t.Fatalf("ParseCoordinate(%q) error = %v", value, err)
	}
	return parsed
}

func TestDistanceFollowsTheTopology(t *testing.T) {
	topology := rules.Default().Topology
	tests := []struct {
		from, to string
		want     int64
	}{
		{from: "1:1:8", to: "1:1:10", want: 1010},
		{from: "1:1:8", to: "1:5:8", want: 3080},
		{from: "1:100:8", to: "1:1:8", want: 2795},
		{from: "1:1:8", to: "2:1:8", want: 20000},
		{from: "3:1:8", to: "1:1:8", want: 20000},
		{from: "1:1:8", to: "1:1:8", want: 5},
	}
	for _, test := range tests {
		t.Run(test.from+" to "+test.to, func(t *testing.T) {
			from := coordinate(t, test.from, topology)
			to := coordinate(t, test.to, topology)
			got, err := Distance(from, to, topology)
			if err != nil || got != test.want {
				t.Fatalf("Distance() = %d %v, want %d", got, err, test.want)
			}
			back, err := Distance(to, from, topology)
			if err != nil || back != test.want {
				t.Fatalf("Distance() reversed = %d %v, want %d", back, err, test.want)
			}
		})
	}

	linear := topology
	linear.CircularSystems = false
	from := coordinate(t, "1:100:8", topology)
	to := coordinate(t, "1:1:8", topology)
	if got, err := Distance(from, to, linear); err != nil || got != 2700+95*99 {
		t.Fatalf("linear distance = %d %v, want %d", got, err, 2700+95*99)
	}
	if _, err := Distance(universe.Coordinate{Galaxy: 9, System: 1, Position: 1}, to, topology); err == nil {
		t.Fatal("Distance() accepted a coordinate outside the universe")
	}
}

func FuzzDistanceIsSymmetricAndPositive(f *testing.F) {
	f.Add(1, 1, 8, 2, 50, 3)
	f.Fuzz(func(t *testing.T, galaxyA, systemA, positionA, galaxyB, systemB, positionB int) {
		topology := rules.Default().Topology
		first := universe.Coordinate{Galaxy: galaxyA, System: systemA, Position: positionA}
		second := universe.Coordinate{Galaxy: galaxyB, System: systemB, Position: positionB}
		limits := limitsOf(topology)
		if first.Validate(limits) != nil || second.Validate(limits) != nil {
			return
		}
		forward, err := Distance(first, second, topology)
		if err != nil {
			t.Fatalf("Distance() error = %v", err)
		}
		backward, err := Distance(second, first, topology)
		if err != nil {
			t.Fatalf("reversed Distance() error = %v", err)
		}
		if forward <= 0 || forward != backward {
			t.Fatalf("Distance() = %d and %d", forward, backward)
		}
	})
}

func TestUnitSpeedAppliesDrivesAndUpgrades(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	tests := []struct {
		name   string
		id     unit.ID
		levels research.Levels
		want   int64
	}{
		{name: "no technology", id: unit.LightFighter, levels: research.Levels{}, want: 12500},
		{name: "combustion three", id: unit.LightFighter, levels: research.Levels{research.CombustionDrive: 3}, want: 16250},
		{name: "small cargo before upgrade", id: unit.SmallCargo, levels: research.Levels{research.ImpulseDrive: 4}, want: 5000},
		{name: "small cargo after upgrade", id: unit.SmallCargo, levels: research.Levels{research.ImpulseDrive: 5}, want: 20000},
		{name: "recycler prefers hyperspace", id: unit.Recycler, levels: research.Levels{research.ImpulseDrive: 17, research.HyperspaceDrive: 15}, want: 33000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition, known := catalogue.Definition(test.id)
			if !known {
				t.Fatalf("unknown unit %s", test.id)
			}
			got, err := UnitSpeed(definition, test.levels)
			if err != nil || got != test.want {
				t.Fatalf("UnitSpeed() = %d %v, want %d", got, err, test.want)
			}
		})
	}

	satellite, _ := catalogue.Definition(unit.SolarSatellite)
	if _, err := UnitSpeed(satellite, research.Levels{}); !errors.Is(err, ErrImmobileUnit) {
		t.Fatalf("UnitSpeed(satellite) error = %v, want ErrImmobileUnit", err)
	}
}

func TestFleetSpeedIsTheSlowestUnit(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	levels := research.Levels{research.CombustionDrive: 1}
	speed, err := FleetSpeed(Composition{unit.LightFighter: 3, unit.SmallCargo: 1}, catalogue, levels)
	if err != nil {
		t.Fatalf("FleetSpeed() error = %v", err)
	}
	if speed != 5500 {
		t.Fatalf("FleetSpeed() = %d, want the small cargo speed 5500", speed)
	}
	if _, err := FleetSpeed(Composition{}, catalogue, levels); !errors.Is(err, ErrEmptyComposition) {
		t.Fatalf("FleetSpeed(empty) error = %v", err)
	}
	if _, err := FleetSpeed(Composition{unit.RocketLauncher: 1}, catalogue, levels); !errors.Is(err, ErrImmobileUnit) {
		t.Fatalf("FleetSpeed(defense) error = %v", err)
	}
	if _, err := FleetSpeed(Composition{unit.LightFighter: -1}, catalogue, levels); !errors.Is(err, ErrInvalidComposition) {
		t.Fatalf("FleetSpeed(negative) error = %v", err)
	}
}

func TestDurationAndFuelReferenceValues(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	levels := research.Levels{}
	fighter := Composition{unit.LightFighter: 1}
	cargos := Composition{unit.SmallCargo: 2}
	tests := []struct {
		name        string
		composition Composition
		distance    int64
		percent     int
		want        time.Duration
		wantFuel    int64
	}{
		{name: "neighbour at full speed", composition: fighter, distance: 1010, percent: 100, want: 324 * time.Second, wantFuel: 3},
		{name: "neighbour at half speed", composition: fighter, distance: 1010, percent: 50, want: 639 * time.Second, wantFuel: 2},
		{name: "two cargos", composition: cargos, distance: 3080, percent: 100, want: 878 * time.Second, wantFuel: 8},
		{name: "another galaxy", composition: fighter, distance: 20000, percent: 100, want: 1410 * time.Second, wantFuel: 46},
		{name: "same position keeps the minimum", composition: fighter, distance: 5, percent: 100, want: 60 * time.Second, wantFuel: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			speed, err := FleetSpeed(test.composition, catalogue, levels)
			if err != nil {
				t.Fatalf("FleetSpeed() error = %v", err)
			}
			duration, err := Duration(test.distance, speed, test.percent, 1, time.Minute)
			if err != nil || duration != test.want {
				t.Fatalf("Duration() = %v %v, want %v", duration, err, test.want)
			}
			fuel, err := Fuel(test.composition, catalogue, levels, test.distance, test.percent, 1)
			if err != nil || fuel != test.wantFuel {
				t.Fatalf("Fuel() = %d %v, want %d", fuel, err, test.wantFuel)
			}
		})
	}

	speed, _ := FleetSpeed(fighter, catalogue, levels)
	if duration, err := Duration(1010, speed, 100, 2, time.Minute); err != nil || duration != 162*time.Second {
		t.Fatalf("Duration() at universe speed two = %v %v", duration, err)
	}
	for _, percent := range []int{0, 5, 55, 110} {
		if _, err := Duration(1010, speed, percent, 1, time.Minute); !errors.Is(err, ErrInvalidSpeed) {
			t.Fatalf("Duration(%d%%) error = %v, want ErrInvalidSpeed", percent, err)
		}
	}
}

func TestCapacityRemovesTheFuel(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	capacity, err := Capacity(Composition{unit.SmallCargo: 2}, catalogue)
	if err != nil || capacity != 10000 {
		t.Fatalf("Capacity() = %d %v, want 10000", capacity, err)
	}
}

func TestStateMachineAllowsOnlyDocumentedTransitions(t *testing.T) {
	allowed := map[[2]State]bool{
		{Outbound, Returning}: true, {Outbound, Completed}: true, {Outbound, Recalled}: true, {Outbound, Destroyed}: true,
		{Returning, Completed}: true, {Returning, Destroyed}: true,
		{Recalled, Completed}: true, {Recalled, Destroyed}: true,
	}
	states := []State{Outbound, Returning, Recalled, Completed, Destroyed}
	for _, from := range states {
		for _, to := range states {
			if got := CanTransition(from, to); got != allowed[[2]State{from, to}] {
				t.Fatalf("CanTransition(%s, %s) = %v", from, to, got)
			}
		}
	}
	if !Outbound.Recallable() || Returning.Recallable() || Recalled.Recallable() {
		t.Fatal("only an outbound fleet may be recalled")
	}
	if !Completed.Terminal() || !Destroyed.Terminal() || Outbound.Terminal() {
		t.Fatal("terminal states are wrong")
	}
	if !Outbound.InFlight() || !Returning.InFlight() || !Recalled.InFlight() || Completed.InFlight() {
		t.Fatal("in-flight states are wrong")
	}
}

func TestMissionProperties(t *testing.T) {
	if !MissionTransport.Returns() || MissionDeploy.Returns() {
		t.Fatal("only a transport comes back")
	}
	if MissionTransport.SpeedClass() != Peaceful || MissionDeploy.SpeedClass() != Peaceful {
		t.Fatal("both milestone four missions are peaceful")
	}
	if !MissionDeploy.TargetsOwnBody() || MissionTransport.TargetsOwnBody() {
		t.Fatal("only a deployment must target a body of the player")
	}
	if Mission("raid").Valid() {
		t.Fatal("an unknown mission must not validate")
	}
}

func TestRecallReturnMirrorsTheTravelledTime(t *testing.T) {
	departure := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	if got := RecallReturn(departure, departure.Add(100*time.Second)); !got.Equal(departure.Add(200 * time.Second)) {
		t.Fatalf("RecallReturn() = %v", got)
	}
	if got := RecallReturn(departure, departure.Add(-time.Second)); !got.Equal(departure) {
		t.Fatalf("RecallReturn() before departure = %v", got)
	}
}

func TestPlanLaunchValidatesEverything(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	configured := rules.Default()
	topology := configured.Topology
	origin := coordinate(t, "1:1:8", topology)
	target := coordinate(t, "1:1:10", topology)
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	context := Context{
		Catalogue: catalogue,
		Levels:    research.Levels{},
		Inventory: unit.Inventory{unit.SmallCargo: 5},
		Stock:     economy.Resources{Metal: 1000, Crystal: 1000, Deuterium: 100},
		Rules:     configured,
		Now:       now,
	}
	request := LaunchRequest{
		Origin: origin, Target: target, TargetKind: TargetPlanet, Mission: MissionTransport,
		Composition: Composition{unit.SmallCargo: 2}, Cargo: economy.Resources{Metal: 900}, Percent: 100,
	}

	plan, err := PlanLaunch(request, context)
	if err != nil {
		t.Fatalf("PlanLaunch() error = %v", err)
	}
	if plan.Fuel != 3 || plan.Capacity != 10000-3 || plan.Duration != 507*time.Second {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.ReturnsAt == nil || !plan.ReturnsAt.Equal(plan.ArrivesAt.Add(plan.Duration)) {
		t.Fatalf("plan return = %v", plan.ReturnsAt)
	}
	if plan.Debit != (economy.Resources{Metal: 900, Deuterium: 3}) {
		t.Fatalf("plan debit = %v", plan.Debit)
	}

	tests := []struct {
		name    string
		mutate  func(*LaunchRequest, *Context)
		wantErr error
	}{
		{name: "empty composition", mutate: func(r *LaunchRequest, _ *Context) { r.Composition = Composition{} }, wantErr: ErrEmptyComposition},
		{name: "more ships than owned", mutate: func(r *LaunchRequest, _ *Context) { r.Composition = Composition{unit.SmallCargo: 6} }, wantErr: ErrInsufficientUnits},
		{name: "cargo above capacity", mutate: func(r *LaunchRequest, _ *Context) { r.Cargo = economy.Resources{Metal: 10000} }, wantErr: ErrCargoExceedsCapacity},
		{name: "cargo above stock", mutate: func(r *LaunchRequest, _ *Context) { r.Cargo = economy.Resources{Crystal: 2000} }, wantErr: economy.ErrInsufficientResources},
		{name: "no deuterium", mutate: func(_ *LaunchRequest, c *Context) { c.Stock = economy.Resources{Metal: 1000} }, wantErr: ErrInsufficientFuel},
		{name: "no free slot", mutate: func(_ *LaunchRequest, c *Context) { c.ActiveFleets = 1 }, wantErr: ErrNoFleetSlot},
		{name: "unknown mission", mutate: func(r *LaunchRequest, _ *Context) { r.Mission = "raid" }, wantErr: ErrInvalidMission},
		{name: "target outside the universe", mutate: func(r *LaunchRequest, _ *Context) {
			r.Target = universe.Coordinate{Galaxy: 9, System: 1, Position: 1}
		}, wantErr: ErrInvalidTarget},
		{name: "invalid speed", mutate: func(r *LaunchRequest, _ *Context) { r.Percent = 55 }, wantErr: ErrInvalidSpeed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutatedRequest := request
			mutatedContext := context
			test.mutate(&mutatedRequest, &mutatedContext)
			if _, err := PlanLaunch(mutatedRequest, mutatedContext); !errors.Is(err, test.wantErr) {
				t.Fatalf("PlanLaunch() error = %v, want %v", err, test.wantErr)
			}
		})
	}

	deployment := request
	deployment.Mission = MissionDeploy
	plan, err = PlanLaunch(deployment, context)
	if err != nil {
		t.Fatalf("PlanLaunch(deploy) error = %v", err)
	}
	if plan.ReturnsAt != nil {
		t.Fatalf("a deployment must not schedule a return: %v", plan.ReturnsAt)
	}
}

func TestMissionCompositionRules(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	tests := []struct {
		name        string
		mission     Mission
		composition Composition
		wantErr     bool
	}{
		{name: "espionage flies probes only", mission: MissionEspionage, composition: Composition{unit.EspionageProbe: 3}},
		{name: "espionage refuses an escort", mission: MissionEspionage, composition: Composition{unit.EspionageProbe: 3, unit.LightFighter: 1}, wantErr: true},
		{name: "espionage needs a probe", mission: MissionEspionage, composition: Composition{unit.LightFighter: 1}, wantErr: true},
		{name: "recycling needs a recycler", mission: MissionRecycle, composition: Composition{unit.SmallCargo: 2}, wantErr: true},
		{name: "recycling accepts an escort", mission: MissionRecycle, composition: Composition{unit.Recycler: 1, unit.LightFighter: 4}},
		{name: "an attack needs a weapon", mission: MissionAttack, composition: Composition{unit.EspionageProbe: 10}, wantErr: true},
		{name: "an attack flies fighters", mission: MissionAttack, composition: Composition{unit.LightFighter: 10}},
		{name: "a transport carries anything", mission: MissionTransport, composition: Composition{unit.SmallCargo: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateComposition(test.mission, test.composition, catalogue)
			if test.wantErr && !errors.Is(err, ErrCompositionMismatch) {
				t.Fatalf("ValidateComposition() error = %v, want ErrCompositionMismatch", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidateComposition() error = %v", err)
			}
		})
	}
}

func TestMissionTargetsAndSpeedClasses(t *testing.T) {
	if MissionAttack.SpeedClass() != Hostile {
		t.Fatal("an attack flies at hostile speed")
	}
	for _, mission := range []Mission{MissionTransport, MissionDeploy, MissionEspionage, MissionRecycle} {
		if mission.SpeedClass() != Peaceful {
			t.Fatalf("%s must fly at peaceful speed", mission)
		}
	}
	if MissionRecycle.Target() != TargetDebris {
		t.Fatal("a recycling aims at a debris field")
	}
	if !MissionAttack.TargetsForeignBody() || !MissionEspionage.TargetsForeignBody() {
		t.Fatal("an attack and an espionage aim at somebody else")
	}
	if MissionTransport.TargetsForeignBody() || MissionDeploy.TargetsForeignBody() {
		t.Fatal("a transport may aim anywhere")
	}
	if !MissionAttack.Returns() || !MissionEspionage.Returns() || !MissionRecycle.Returns() {
		t.Fatal("only a deployment stays on its destination")
	}
}

func TestColonizationNeedsAColonyShipAndAnEmptyPosition(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	if err := ValidateComposition(MissionColonize, Composition{unit.SmallCargo: 1}, catalogue); !errors.Is(err, ErrCompositionMismatch) {
		t.Fatalf("colonising without a colony ship error = %v", err)
	}
	if err := ValidateComposition(MissionColonize, Composition{unit.ColonyShip: 1, unit.SmallCargo: 3}, catalogue); err != nil {
		t.Fatalf("colonising with an escort error = %v", err)
	}
	if MissionColonize.Target() != TargetEmpty {
		t.Fatalf("a colonisation aims at %q, want an empty position", MissionColonize.Target())
	}
	if !MissionColonize.Returns() || MissionColonize.TargetsForeignBody() || MissionColonize.TargetsOwnBody() {
		t.Fatal("a colonisation comes home and belongs to nobody yet")
	}
}
