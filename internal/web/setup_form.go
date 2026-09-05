package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"universeatwar/internal/domain/rules"
)

type setupPageData struct {
	CSRFToken string
	Error     string
	Step      int
	Version   int64
	Rules     rules.Ruleset
}

func (h *Handler) renderSetup(response http.ResponseWriter, status int, data setupPageData) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = h.templates.ExecuteTemplate(response, "setup.html", data)
}

func updateRulesFromForm(step int, request *http.Request, configured *rules.Ruleset) error {
	switch step {
	case 1:
		configured.Identity.Name = requiredText(request, "name")
		configured.Identity.Language = requiredText(request, "language")
		configured.Identity.Timezone = requiredText(request, "timezone")
		configured.Identity.Description = strings.TrimSpace(request.PostFormValue("description"))
		configured.Identity.NetworkVisibility = requiredText(request, "network_visibility")
		configured.Identity.RegistrationPolicy = requiredText(request, "registration_policy")
	case 2:
		var err error
		if configured.Topology.Galaxies, err = intField(request, "galaxies"); err != nil {
			return err
		}
		if configured.Topology.SystemsPerGalaxy, err = intField(request, "systems_per_galaxy"); err != nil {
			return err
		}
		if configured.Topology.PositionsPerSystem, err = intField(request, "positions_per_system"); err != nil {
			return err
		}
		configured.Topology.CircularGalaxies = checkbox(request, "circular_galaxies")
		configured.Topology.CircularSystems = checkbox(request, "circular_systems")
		if configured.Topology.MinPlanetFields, err = intField(request, "min_planet_fields"); err != nil {
			return err
		}
		if configured.Topology.MaxPlanetFields, err = intField(request, "max_planet_fields"); err != nil {
			return err
		}
		if configured.Topology.InterSystemDistance, err = intField(request, "inter_system_distance"); err != nil {
			return err
		}
		if configured.Topology.InterGalaxyDistance, err = intField(request, "inter_galaxy_distance"); err != nil {
			return err
		}
	case 3:
		fields := []struct {
			name   string
			target *float64
		}{
			{"economy_speed", &configured.Time.EconomySpeed},
			{"building_speed", &configured.Time.BuildingSpeed},
			{"research_speed", &configured.Time.ResearchSpeed},
			{"shipyard_speed", &configured.Time.ShipyardSpeed},
			{"defense_speed", &configured.Time.DefenseSpeed},
			{"peaceful_fleet_speed", &configured.Time.PeacefulFleetSpeed},
			{"hostile_fleet_speed", &configured.Time.HostileFleetSpeed},
			{"holding_fleet_speed", &configured.Time.HoldingFleetSpeed},
			{"expedition_speed", &configured.Time.ExpeditionSpeed},
		}
		for _, field := range fields {
			value, err := floatField(request, field.name)
			if err != nil {
				return err
			}
			*field.target = value
		}
		var err error
		if configured.Time.MinimumMissionSeconds, err = intField(request, "minimum_mission_seconds"); err != nil {
			return err
		}
		configured.Time.PauseWhenEmpty = checkbox(request, "pause_when_empty")
	case 4:
		fields := []struct {
			name   string
			target *float64
		}{
			{"base_metal_per_hour", &configured.Economy.BaseMetalPerHour},
			{"base_crystal_per_hour", &configured.Economy.BaseCrystalPerHour},
			{"base_deuterium_per_hour", &configured.Economy.BaseDeuteriumPerHour},
			{"mine_production_growth", &configured.Economy.MineProductionGrowth},
			{"mine_cost_growth", &configured.Economy.MineCostGrowth},
			{"energy_consumption_growth", &configured.Economy.EnergyConsumptionGrowth},
			{"pillage_ratio", &configured.Economy.PillageRatio},
			{"deuterium_consumption", &configured.Economy.DeuteriumConsumption},
			{"low_energy_production", &configured.Economy.LowEnergyProduction},
		}
		for _, field := range fields {
			value, err := floatField(request, field.name)
			if err != nil {
				return err
			}
			*field.target = value
		}
		value, err := int64Field(request, "base_storage")
		if err != nil {
			return err
		}
		configured.Economy.BaseStorage = value
	case 5:
		var err error
		if configured.Combat.MaximumRounds, err = intField(request, "maximum_rounds"); err != nil {
			return err
		}
		fields := []struct {
			name   string
			target *float64
		}{
			{"ships_to_debris", &configured.Combat.ShipsToDebris},
			{"defenses_to_debris", &configured.Combat.DefensesToDebris},
			{"defense_rebuild_chance", &configured.Combat.DefenseRebuildChance},
			{"maximum_moon_chance", &configured.Combat.MaximumMoonChance},
			{"maximum_pillage", &configured.Combat.MaximumPillage},
		}
		for _, field := range fields {
			value, parseErr := floatField(request, field.name)
			if parseErr != nil {
				return parseErr
			}
			*field.target = value
		}
		if configured.Combat.RecyclerCapacity, err = int64Field(request, "recycler_capacity"); err != nil {
			return err
		}
		configured.Combat.MissilesEnabled = checkbox(request, "missiles_enabled")
	case 6:
		fields := []struct {
			name   string
			target *float64
		}{
			{"building_cost_multiplier", &configured.Progression.BuildingCostMultiplier},
			{"research_cost_multiplier", &configured.Progression.ResearchCostMultiplier},
			{"ship_cost_multiplier", &configured.Progression.ShipCostMultiplier},
			{"defense_cost_multiplier", &configured.Progression.DefenseCostMultiplier},
			{"laboratory_bonus", &configured.Progression.LaboratoryBonus},
		}
		for _, field := range fields {
			value, err := floatField(request, field.name)
			if err != nil {
				return err
			}
			*field.target = value
		}
		var err error
		if configured.Progression.MaximumColonies, err = intField(request, "maximum_colonies"); err != nil {
			return err
		}
		configured.Progression.ResearchNetworkEnabled = checkbox(request, "research_network_enabled")
	case 7:
		configured.Team.AlliancesEnabled = checkbox(request, "alliances_enabled")
		var err error
		if configured.Team.MaximumAllianceSize, err = intField(request, "maximum_alliance_size"); err != nil {
			return err
		}
		configured.Team.ACSEnabled = checkbox(request, "acs_enabled")
		configured.Team.GroupDefenseEnabled = checkbox(request, "group_defense_enabled")
		configured.Team.ReportSharingEnabled = checkbox(request, "report_sharing_enabled")
		configured.Team.IntelligenceSharingEnabled = checkbox(request, "intelligence_sharing_enabled")
		configured.Team.DiplomacyEnabled = checkbox(request, "diplomacy_enabled")
		configured.Team.StartMode = requiredText(request, "start_mode")
	case 8:
		var err error
		if configured.AI.Total, err = intField(request, "ai_total"); err != nil {
			return err
		}
		if configured.AI.IndependentCount, err = intField(request, "ai_independent_count"); err != nil {
			return err
		}
		if configured.AI.AllianceCount, err = intField(request, "ai_alliance_count"); err != nil {
			return err
		}
		if configured.AI.AllianceSize, err = intField(request, "ai_alliance_size"); err != nil {
			return err
		}
		configured.AI.Difficulty = requiredText(request, "ai_difficulty")
		if configured.AI.ThinkIntervalSeconds, err = intField(request, "ai_think_interval_seconds"); err != nil {
			return err
		}
		if configured.AI.ActivityStartHour, err = intField(request, "ai_activity_start_hour"); err != nil {
			return err
		}
		if configured.AI.ActivityEndHour, err = intField(request, "ai_activity_end_hour"); err != nil {
			return err
		}
		if configured.AI.InitialDevelopmentLevel, err = intField(request, "ai_initial_development_level"); err != nil {
			return err
		}
		if configured.AI.Coordination, err = floatField(request, "ai_coordination"); err != nil {
			return err
		}
		configured.AI.Diplomacy = requiredText(request, "ai_diplomacy")
	case 9:
		var err error
		if configured.Protection.BeginnerProtectionPoints, err = int64Field(request, "beginner_protection_points"); err != nil {
			return err
		}
		if configured.Protection.MaximumPointRatio, err = floatField(request, "maximum_point_ratio"); err != nil {
			return err
		}
		if configured.Protection.AccountsPerInstallation, err = intField(request, "accounts_per_installation"); err != nil {
			return err
		}
		configured.Protection.VacationModeEnabled = checkbox(request, "vacation_mode_enabled")
		if configured.Protection.InactiveAfterDays, err = intField(request, "inactive_after_days"); err != nil {
			return err
		}
		if configured.Protection.DeleteAfterDays, err = intField(request, "delete_after_days"); err != nil {
			return err
		}
		if configured.Protection.PlayerLimit, err = intField(request, "player_limit"); err != nil {
			return err
		}
		configured.Identity.RegistrationPolicy = requiredText(request, "registration_policy")
	default:
		return fmt.Errorf("étape de configuration inconnue")
	}
	return nil
}

func requiredText(request *http.Request, name string) string {
	return strings.TrimSpace(request.PostFormValue(name))
}

func intField(request *http.Request, name string) (int, error) {
	value, err := strconv.Atoi(request.PostFormValue(name))
	if err != nil {
		return 0, fmt.Errorf("le champ %s doit être un nombre entier", name)
	}
	return value, nil
}

func int64Field(request *http.Request, name string) (int64, error) {
	value, err := strconv.ParseInt(request.PostFormValue(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("le champ %s doit être un nombre entier", name)
	}
	return value, nil
}

func floatField(request *http.Request, name string) (float64, error) {
	value, err := strconv.ParseFloat(request.PostFormValue(name), 64)
	if err != nil {
		return 0, fmt.Errorf("le champ %s doit être un nombre", name)
	}
	return value, nil
}

func checkbox(request *http.Request, name string) bool {
	return request.PostFormValue(name) == "on"
}
