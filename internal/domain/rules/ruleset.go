// Package rules defines versioned universe configuration independently from
// persistence and HTTP forms.
package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// CurrentSchemaVersion is the ruleset document layout produced by this build.
// Older documents decode on top of the current defaults; a newer one is
// refused so a downgrade never corrupts a saved universe.
const CurrentSchemaVersion = 4

// DefaultCatalogueVersion names the content catalogue shipped with this build.
const DefaultCatalogueVersion = "classic-1"

// ErrFutureSchema reports a document written by a newer application.
var ErrFutureSchema = errors.New("rules: document schema is newer than this application")

// Ruleset groups every setup category. JSON field names are stable persisted
// identifiers, never translated labels.
type Ruleset struct {
	SchemaVersion int                 `json:"schema_version"`
	Identity      IdentitySettings    `json:"identity"`
	Topology      TopologySettings    `json:"topology"`
	Time          TimeSettings        `json:"time"`
	Economy       EconomySettings     `json:"economy"`
	Combat        CombatSettings      `json:"combat"`
	Espionage     EspionageSettings   `json:"espionage"`
	Expansion     ExpansionSettings   `json:"expansion"`
	Progression   ProgressionSettings `json:"progression"`
	Team          TeamSettings        `json:"team"`
	AI            AISettings          `json:"ai"`
	Protection    ProtectionSettings  `json:"protection"`
}

type IdentitySettings struct {
	Name               string `json:"name"`
	Language           string `json:"language"`
	Timezone           string `json:"timezone"`
	Description        string `json:"description"`
	NetworkVisibility  string `json:"network_visibility"`
	RegistrationPolicy string `json:"registration_policy"`
}

type TopologySettings struct {
	Galaxies            int  `json:"galaxies"`
	SystemsPerGalaxy    int  `json:"systems_per_galaxy"`
	PositionsPerSystem  int  `json:"positions_per_system"`
	CircularGalaxies    bool `json:"circular_galaxies"`
	CircularSystems     bool `json:"circular_systems"`
	MinPlanetFields     int  `json:"min_planet_fields"`
	MaxPlanetFields     int  `json:"max_planet_fields"`
	InterSystemDistance int  `json:"inter_system_distance"`
	InterGalaxyDistance int  `json:"inter_galaxy_distance"`
}

type TimeSettings struct {
	EconomySpeed          float64 `json:"economy_speed"`
	BuildingSpeed         float64 `json:"building_speed"`
	ResearchSpeed         float64 `json:"research_speed"`
	ShipyardSpeed         float64 `json:"shipyard_speed"`
	DefenseSpeed          float64 `json:"defense_speed"`
	PeacefulFleetSpeed    float64 `json:"peaceful_fleet_speed"`
	HostileFleetSpeed     float64 `json:"hostile_fleet_speed"`
	HoldingFleetSpeed     float64 `json:"holding_fleet_speed"`
	ExpeditionSpeed       float64 `json:"expedition_speed"`
	MinimumMissionSeconds int     `json:"minimum_mission_seconds"`
	PauseWhenEmpty        bool    `json:"pause_when_empty"`
}

type EconomySettings struct {
	BaseMetalPerHour        float64 `json:"base_metal_per_hour"`
	BaseCrystalPerHour      float64 `json:"base_crystal_per_hour"`
	BaseDeuteriumPerHour    float64 `json:"base_deuterium_per_hour"`
	MineProductionGrowth    float64 `json:"mine_production_growth"`
	MineCostGrowth          float64 `json:"mine_cost_growth"`
	EnergyConsumptionGrowth float64 `json:"energy_consumption_growth"`
	BaseStorage             int64   `json:"base_storage"`
	PillageRatio            float64 `json:"pillage_ratio"`
	DeuteriumConsumption    float64 `json:"deuterium_consumption"`
	LowEnergyProduction     float64 `json:"low_energy_production"`
}

type CombatSettings struct {
	MaximumRounds        int     `json:"maximum_rounds"`
	ShipsToDebris        float64 `json:"ships_to_debris"`
	DefensesToDebris     float64 `json:"defenses_to_debris"`
	DefenseRebuildChance float64 `json:"defense_rebuild_chance"`
	MaximumMoonChance    float64 `json:"maximum_moon_chance"`
	MaximumPillage       float64 `json:"maximum_pillage"`
	RecyclerCapacity     int64   `json:"recycler_capacity"`
	MissilesEnabled      bool    `json:"missiles_enabled"`
}

// EspionageSettings tunes what a spy report reveals and how it is noticed.
type EspionageSettings struct {
	ResourcesThreshold  int     `json:"resources_threshold"`
	FleetThreshold      int     `json:"fleet_threshold"`
	DefensesThreshold   int     `json:"defenses_threshold"`
	BuildingsThreshold  int     `json:"buildings_threshold"`
	ResearchThreshold   int     `json:"research_threshold"`
	DetectionBase       float64 `json:"detection_base"`
	RecentReportSeconds int     `json:"recent_report_seconds"`
}

// ExpansionSettings tunes moons, the phalanx and the jump gate.
type ExpansionSettings struct {
	BaseMoonFields          int   `json:"base_moon_fields"`
	LunarBaseFields         int   `json:"lunar_base_fields"`
	PhalanxScanCost         int64 `json:"phalanx_scan_cost"`
	JumpGateCooldownSeconds int   `json:"jump_gate_cooldown_seconds"`
}

type ProgressionSettings struct {
	BuildingCostMultiplier float64 `json:"building_cost_multiplier"`
	ResearchCostMultiplier float64 `json:"research_cost_multiplier"`
	ShipCostMultiplier     float64 `json:"ship_cost_multiplier"`
	DefenseCostMultiplier  float64 `json:"defense_cost_multiplier"`
	LaboratoryBonus        float64 `json:"laboratory_bonus"`
	ResearchNetworkEnabled bool    `json:"research_network_enabled"`
	MaximumColonies        int     `json:"maximum_colonies"`
	CatalogueVersion       string  `json:"catalogue_version"`
}

type TeamSettings struct {
	AlliancesEnabled           bool   `json:"alliances_enabled"`
	MaximumAllianceSize        int    `json:"maximum_alliance_size"`
	ACSEnabled                 bool   `json:"acs_enabled"`
	GroupDefenseEnabled        bool   `json:"group_defense_enabled"`
	ReportSharingEnabled       bool   `json:"report_sharing_enabled"`
	IntelligenceSharingEnabled bool   `json:"intelligence_sharing_enabled"`
	DiplomacyEnabled           bool   `json:"diplomacy_enabled"`
	StartMode                  string `json:"start_mode"`
}

type AISettings struct {
	Total                   int     `json:"total"`
	IndependentCount        int     `json:"independent_count"`
	AllianceCount           int     `json:"alliance_count"`
	AllianceSize            int     `json:"alliance_size"`
	Difficulty              string  `json:"difficulty"`
	ThinkIntervalSeconds    int     `json:"think_interval_seconds"`
	ActivityStartHour       int     `json:"activity_start_hour"`
	ActivityEndHour         int     `json:"activity_end_hour"`
	InitialDevelopmentLevel int     `json:"initial_development_level"`
	Coordination            float64 `json:"coordination"`
	Diplomacy               string  `json:"diplomacy"`
}

type ProtectionSettings struct {
	BeginnerProtectionPoints int64   `json:"beginner_protection_points"`
	MaximumPointRatio        float64 `json:"maximum_point_ratio"`
	AccountsPerInstallation  int     `json:"accounts_per_installation"`
	VacationModeEnabled      bool    `json:"vacation_mode_enabled"`
	InactiveAfterDays        int     `json:"inactive_after_days"`
	DeleteAfterDays          int     `json:"delete_after_days"`
	PlayerLimit              int     `json:"player_limit"`
}

// Default provides a conservative local campaign profile.
func Default() Ruleset {
	return Ruleset{
		SchemaVersion: CurrentSchemaVersion,
		Identity: IdentitySettings{
			Name: "Universe At War", Language: "fr", Timezone: "Europe/Paris",
			Description: "Univers spatial persistant local", NetworkVisibility: "local",
			RegistrationPolicy: "closed",
		},
		Topology: TopologySettings{
			Galaxies: 3, SystemsPerGalaxy: 100, PositionsPerSystem: 15,
			CircularGalaxies: true, CircularSystems: true,
			MinPlanetFields: 120, MaxPlanetFields: 260,
			InterSystemDistance: 95, InterGalaxyDistance: 20000,
		},
		Time: TimeSettings{
			EconomySpeed: 1, BuildingSpeed: 1, ResearchSpeed: 1, ShipyardSpeed: 1,
			DefenseSpeed: 1, PeacefulFleetSpeed: 1, HostileFleetSpeed: 1,
			HoldingFleetSpeed: 1, ExpeditionSpeed: 1, MinimumMissionSeconds: 60,
		},
		Economy: EconomySettings{
			BaseMetalPerHour: 30, BaseCrystalPerHour: 15, BaseDeuteriumPerHour: 0,
			MineProductionGrowth: 1.1, MineCostGrowth: 1.5, EnergyConsumptionGrowth: 1.1,
			BaseStorage: 10000, PillageRatio: .5, DeuteriumConsumption: 1,
			LowEnergyProduction: .5,
		},
		Combat: CombatSettings{
			MaximumRounds: 6, ShipsToDebris: .3, DefensesToDebris: 0,
			DefenseRebuildChance: .7, MaximumMoonChance: .2, MaximumPillage: .5,
			RecyclerCapacity: 20000, MissilesEnabled: true,
		},
		Espionage: EspionageSettings{
			ResourcesThreshold: 1, FleetThreshold: 2, DefensesThreshold: 3,
			BuildingsThreshold: 4, ResearchThreshold: 5,
			DetectionBase: .0025, RecentReportSeconds: 3600,
		},
		Expansion: ExpansionSettings{
			BaseMoonFields: 1, LunarBaseFields: 3,
			PhalanxScanCost: 5000, JumpGateCooldownSeconds: 3600,
		},
		Progression: ProgressionSettings{
			BuildingCostMultiplier: 1, ResearchCostMultiplier: 1, ShipCostMultiplier: 1,
			DefenseCostMultiplier: 1, LaboratoryBonus: 1, ResearchNetworkEnabled: true,
			MaximumColonies: 9, CatalogueVersion: DefaultCatalogueVersion,
		},
		Team: TeamSettings{
			AlliancesEnabled: true, MaximumAllianceSize: 20, ACSEnabled: true,
			GroupDefenseEnabled: true, ReportSharingEnabled: true,
			IntelligenceSharingEnabled: true, DiplomacyEnabled: true, StartMode: "free",
		},
		AI: AISettings{
			Total: 4, IndependentCount: 2, AllianceCount: 1, AllianceSize: 2,
			Difficulty: "normal", ThinkIntervalSeconds: 300, ActivityStartHour: 8,
			ActivityEndHour: 23, InitialDevelopmentLevel: 0, Coordination: .5,
			Diplomacy: "dynamic",
		},
		Protection: ProtectionSettings{
			BeginnerProtectionPoints: 5000, MaximumPointRatio: 5,
			AccountsPerInstallation: 1, VacationModeEnabled: true,
			InactiveAfterDays: 7, DeleteAfterDays: 35, PlayerLimit: 32,
		},
	}
}

// Validate rejects configurations that would make the universe inconsistent.
func (r Ruleset) Validate() error {
	if r.SchemaVersion < 1 || r.SchemaVersion > CurrentSchemaVersion {
		return ErrFutureSchema
	}
	if strings.TrimSpace(r.Identity.Name) == "" {
		return errors.New("rules: universe name is required")
	}
	if strings.TrimSpace(r.Identity.Language) == "" {
		return errors.New("rules: language is required")
	}
	if _, err := time.LoadLocation(r.Identity.Timezone); err != nil {
		return errors.New("rules: timezone is unknown")
	}
	if !oneOf(r.Identity.NetworkVisibility, "local", "private_network") {
		return errors.New("rules: invalid network visibility")
	}
	if !oneOf(r.Identity.RegistrationPolicy, "closed", "open", "invitation") {
		return errors.New("rules: invalid registration policy")
	}
	if r.Topology.Galaxies <= 0 || r.Topology.SystemsPerGalaxy <= 0 || r.Topology.PositionsPerSystem <= 0 {
		return errors.New("rules: topology dimensions must be positive")
	}
	if r.Topology.MinPlanetFields <= 0 || r.Topology.MaxPlanetFields < r.Topology.MinPlanetFields {
		return errors.New("rules: invalid planet field range")
	}
	if r.Topology.InterSystemDistance <= 0 || r.Topology.InterGalaxyDistance <= 0 {
		return errors.New("rules: distances must be positive")
	}
	speeds := []struct {
		name  string
		value float64
	}{
		{"economy", r.Time.EconomySpeed},
		{"building", r.Time.BuildingSpeed},
		{"research", r.Time.ResearchSpeed},
		{"shipyard", r.Time.ShipyardSpeed},
		{"defense", r.Time.DefenseSpeed},
		{"peaceful fleet", r.Time.PeacefulFleetSpeed},
		{"hostile fleet", r.Time.HostileFleetSpeed},
		{"holding fleet", r.Time.HoldingFleetSpeed},
		{"expedition", r.Time.ExpeditionSpeed},
	}
	for _, speed := range speeds {
		if !positiveFinite(speed.value) || speed.value > 1000 {
			return fmt.Errorf("rules: %s speed must be between 0 and 1000", speed.name)
		}
	}
	if r.Time.MinimumMissionSeconds <= 0 {
		return errors.New("rules: minimum mission duration must be positive")
	}
	if !nonNegativeFinite(r.Economy.BaseMetalPerHour) || !nonNegativeFinite(r.Economy.BaseCrystalPerHour) || !nonNegativeFinite(r.Economy.BaseDeuteriumPerHour) {
		return errors.New("rules: base production cannot be negative")
	}
	if !positiveFinite(r.Economy.MineProductionGrowth) || !positiveFinite(r.Economy.MineCostGrowth) || r.Economy.MineCostGrowth <= 1 || !positiveFinite(r.Economy.EnergyConsumptionGrowth) {
		return errors.New("rules: invalid economy growth")
	}
	if r.Economy.BaseStorage <= 0 || !nonNegativeFinite(r.Economy.DeuteriumConsumption) || !ratio(r.Economy.PillageRatio) || !ratio(r.Economy.LowEnergyProduction) {
		return errors.New("rules: invalid economy capacity or ratio")
	}
	if r.Combat.MaximumRounds <= 0 || r.Combat.RecyclerCapacity <= 0 ||
		!ratio(r.Combat.ShipsToDebris) || !ratio(r.Combat.DefensesToDebris) ||
		!ratio(r.Combat.DefenseRebuildChance) || !ratio(r.Combat.MaximumMoonChance) ||
		!ratio(r.Combat.MaximumPillage) {
		return errors.New("rules: invalid combat setting")
	}
	thresholds := []int{
		r.Espionage.ResourcesThreshold, r.Espionage.FleetThreshold, r.Espionage.DefensesThreshold,
		r.Espionage.BuildingsThreshold, r.Espionage.ResearchThreshold,
	}
	for index, threshold := range thresholds {
		if threshold < 0 {
			return errors.New("rules: espionage thresholds cannot be negative")
		}
		if index > 0 && threshold < thresholds[index-1] {
			return errors.New("rules: espionage thresholds must not decrease")
		}
	}
	if !ratio(r.Espionage.DetectionBase) || r.Espionage.RecentReportSeconds <= 0 {
		return errors.New("rules: invalid espionage detection or freshness")
	}
	if r.Expansion.BaseMoonFields <= 0 || r.Expansion.LunarBaseFields <= 0 ||
		r.Expansion.PhalanxScanCost < 0 || r.Expansion.JumpGateCooldownSeconds <= 0 {
		return errors.New("rules: invalid expansion setting")
	}
	for _, multiplier := range []float64{
		r.Progression.BuildingCostMultiplier, r.Progression.ResearchCostMultiplier,
		r.Progression.ShipCostMultiplier, r.Progression.DefenseCostMultiplier,
		r.Progression.LaboratoryBonus,
	} {
		if !positiveFinite(multiplier) {
			return errors.New("rules: progression multipliers must be positive")
		}
	}
	if r.Progression.MaximumColonies <= 0 {
		return errors.New("rules: maximum colonies must be positive")
	}
	if strings.TrimSpace(r.Progression.CatalogueVersion) == "" {
		return errors.New("rules: catalogue version is required")
	}
	if !oneOf(r.Team.StartMode, "free", "predefined_teams", "humans_vs_ai", "pvpve") {
		return errors.New("rules: invalid team start mode")
	}
	if !r.Team.AlliancesEnabled && (r.Team.ACSEnabled || r.Team.GroupDefenseEnabled || r.Team.IntelligenceSharingEnabled) {
		return errors.New("rules: alliance features require alliances")
	}
	if r.Team.AlliancesEnabled && r.Team.MaximumAllianceSize <= 0 {
		return errors.New("rules: alliance size must be positive")
	}
	if r.AI.Total < 0 || r.AI.IndependentCount < 0 || r.AI.AllianceCount < 0 || r.AI.AllianceSize < 0 ||
		r.AI.IndependentCount+r.AI.AllianceCount*r.AI.AllianceSize > r.AI.Total {
		return errors.New("rules: invalid AI population")
	}
	if !oneOf(r.AI.Difficulty, "easy", "normal", "hard") || !oneOf(r.AI.Diplomacy, "none", "static", "dynamic") {
		return errors.New("rules: invalid AI policy")
	}
	if r.AI.ThinkIntervalSeconds <= 0 || r.AI.ActivityStartHour < 0 || r.AI.ActivityStartHour > 23 ||
		r.AI.ActivityEndHour < 1 || r.AI.ActivityEndHour > 24 || r.AI.InitialDevelopmentLevel < 0 || !ratio(r.AI.Coordination) {
		return errors.New("rules: invalid AI timing or coordination")
	}
	if r.Protection.BeginnerProtectionPoints < 0 || r.Protection.MaximumPointRatio < 1 ||
		r.Protection.AccountsPerInstallation <= 0 || r.Protection.InactiveAfterDays < 0 ||
		r.Protection.DeleteAfterDays < r.Protection.InactiveAfterDays || r.Protection.PlayerLimit <= 0 {
		return errors.New("rules: invalid player protection")
	}
	return nil
}

// Encode returns the canonical persisted representation.
func Encode(r Ruleset) ([]byte, error) {
	r.SchemaVersion = CurrentSchemaVersion
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(r)
}

// Decode strictly parses a persisted ruleset on top of the current defaults, so
// a document written before a section existed keeps decoding. Unknown fields
// stay refused: they can only come from a newer, incompatible application.
func Decode(document []byte) (Ruleset, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	ruleset := Default()
	ruleset.SchemaVersion = 1
	if err := decoder.Decode(&ruleset); err != nil {
		return Ruleset{}, fmt.Errorf("rules: decode: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Ruleset{}, err
	}
	if ruleset.SchemaVersion > CurrentSchemaVersion {
		return Ruleset{}, ErrFutureSchema
	}
	ruleset.SchemaVersion = CurrentSchemaVersion
	if err := ruleset.Validate(); err != nil {
		return Ruleset{}, err
	}
	return ruleset, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("rules: document contains trailing data")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func ratio(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func positiveFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func nonNegativeFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
