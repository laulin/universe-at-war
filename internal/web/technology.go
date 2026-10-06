package web

import (
	"fmt"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	domainbuilding "universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/phalanx"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
)

// technologyView is the common encyclopaedia entry opened from a progression
// card. The four catalogues feed one dialog, so costs, requirements and combat
// figures cannot drift between four independent pieces of markup.
type technologyView struct {
	DialogID     string
	ID           string
	Name         string
	Kind         string
	ArtCategory  string
	Description  string
	Level        int
	Owned        int64
	Requirements []requirementView
	Unlocks      []technologyUnlockView
	Progress     []technologyProgressRow
	Stats        []technologyStatView
	Inflicts     []unitPageVolley
	Suffers      []unitPageVolley
}

type technologyUnlockView struct {
	Name  string
	Level int
}

type technologyProgressRow struct {
	Level     int
	Current   bool
	Metal     int64
	Crystal   int64
	Deuterium int64
	Energy    int64
	Duration  time.Duration
	Effect    string
}

type technologyStatView struct {
	Name  string
	Value string
}

func buildingTechnology(choice appeconomy.BuildingChoice, planet appeconomy.Planet) technologyView {
	return technologyView{
		DialogID: "technology-building-" + string(choice.Definition.ID),
		ID:       string(choice.Definition.ID), Name: buildingName(choice.Definition.ID), Kind: "Bâtiment",
		ArtCategory: "building", Description: buildingRole(choice.Definition.ID), Level: choice.Level,
		Requirements: requirementViews(choice.Requirements),
		Unlocks:      technologyUnlocks(prerequisite.Building, string(choice.Definition.ID)),
		Progress:     buildingProgress(choice.Definition, planet),
	}
}

func researchTechnology(choiceLevel int, definition research.Definition, requirements []prerequisite.Resolved,
	planet appeconomy.Planet, laboratories research.Laboratories) technologyView {
	return technologyView{
		DialogID: "technology-research-" + string(definition.ID),
		ID:       string(definition.ID), Name: researchName(definition.ID), Kind: "Recherche",
		ArtCategory: "research", Description: researchRole(definition.ID), Level: choiceLevel,
		Requirements: requirementViews(requirements),
		Unlocks:      technologyUnlocks(prerequisite.Research, string(definition.ID)),
		Progress:     researchProgress(definition, choiceLevel, planet, laboratories),
	}
}

func unitTechnology(definition unit.Definition, owned int64, unitCost domaineconomy.Resources, duration time.Duration,
	planet appeconomy.Planet, speed int64, inflicts, suffers []unitPageVolley) technologyView {
	state := prerequisite.State{Buildings: planet.Levels.Generic(), Researches: planet.Researches.Generic()}
	kind := "Vaisseau"
	if definition.Family == unit.Defense {
		kind = "Défense"
	}
	stats := []technologyStatView{
		{Name: "Métal", Value: figure(unitCost.Metal)},
		{Name: "Cristal", Value: figure(unitCost.Crystal)},
		{Name: "Deutérium", Value: figure(unitCost.Deuterium)},
		{Name: "Durée unitaire", Value: duration.String()},
		{Name: "Arme", Value: figure(definition.Weapon)},
		{Name: "Bouclier", Value: figure(definition.Shield)},
		{Name: "Coque", Value: figure(definition.Hull())},
	}
	if definition.Cargo > 0 {
		stats = append(stats, technologyStatView{Name: "Fret", Value: figure(definition.Cargo)})
	}
	if speed > 0 {
		stats = append(stats, technologyStatView{Name: "Vitesse actuelle", Value: figure(speed)})
	}
	if definition.FuelConsumption > 0 {
		stats = append(stats, technologyStatView{Name: "Consommation", Value: figure(definition.FuelConsumption)})
	}
	if definition.MaximumQuantity > 0 {
		stats = append(stats, technologyStatView{Name: "Maximum", Value: figure(definition.MaximumQuantity)})
	}
	if definition.SiloSlots > 0 {
		stats = append(stats, technologyStatView{Name: "Emplacements de silo", Value: figure(int64(definition.SiloSlots))})
	}
	return technologyView{
		DialogID: "technology-" + string(definition.Family) + "-" + string(definition.ID),
		ID:       string(definition.ID), Name: unitName(definition.ID), Kind: kind,
		ArtCategory: string(definition.Family), Description: unitRole(definition.ID), Owned: owned,
		Requirements: requirementViews(prerequisite.Resolve(definition.Prerequisites, state)),
		Stats:        stats, Inflicts: inflicts, Suffers: suffers,
	}
}

// progressWindow follows the classic level table through level 15. Past that,
// it stays useful by following the player's current level for ten rows.
func progressWindow(current int) (int, int) {
	if current <= 15 {
		return 1, 15
	}
	return current, current + 9
}

func buildingProgress(definition domainbuilding.Definition, planet appeconomy.Planet) []technologyProgressRow {
	start, end := progressWindow(planet.Levels[definition.ID])
	catalogue := domainbuilding.DefaultCatalogue()
	rows := make([]technologyProgressRow, 0, end-start+1)
	for level := start; level <= end; level++ {
		cost, err := catalogue.Cost(definition.ID, level, planet.Rules.Progression.BuildingCostMultiplier)
		if err != nil {
			break
		}
		robotics := planet.Levels[domainbuilding.RoboticsFactory]
		nanites := planet.Levels[domainbuilding.NaniteFactory]
		if definition.ID == domainbuilding.RoboticsFactory {
			robotics = level - 1
		}
		if definition.ID == domainbuilding.NaniteFactory {
			nanites = level - 1
		}
		duration, err := catalogue.Duration(cost, robotics, nanites, planet.Rules.Time.BuildingSpeed)
		if err != nil {
			break
		}
		energy, effect := buildingEffect(definition.ID, level, planet)
		rows = append(rows, technologyProgressRow{
			Level: level, Current: level == planet.Levels[definition.ID],
			Metal: cost.Metal, Crystal: cost.Crystal, Deuterium: cost.Deuterium,
			Energy: energy, Duration: duration, Effect: effect,
		})
	}
	return rows
}

func buildingEffect(id domainbuilding.ID, level int, planet appeconomy.Planet) (int64, string) {
	switch id {
	case domainbuilding.MetalMine, domainbuilding.CrystalMine, domainbuilding.DeuteriumSynthesizer:
		rate, energy, err := isolatedMineOutput(id, level, planet)
		if err == nil {
			return -energy, "+" + figure(rate) + "/h"
		}
	case domainbuilding.SolarPlant:
		energy, err := domaineconomy.SolarPlantEnergy(level)
		if err == nil {
			return energy, "Produit " + figure(energy) + " énergie"
		}
	case domainbuilding.FusionReactor:
		energy, energyErr := domaineconomy.FusionReactorEnergy(level, planet.Researches[research.EnergyTechnology])
		fuel, fuelErr := domaineconomy.FusionReactorDeuterium(level, planet.Rules.Time.EconomySpeed)
		if energyErr == nil && fuelErr == nil {
			return energy, "Consomme " + figure(fuel) + " deutérium/h"
		}
	case domainbuilding.MetalStorage, domainbuilding.CrystalStorage, domainbuilding.DeuteriumTank:
		capacity, err := domaineconomy.Capacity(planet.Rules.Economy.BaseStorage, level)
		if err == nil {
			return 0, "Capacité " + figure(capacity)
		}
	case domainbuilding.RoboticsFactory, domainbuilding.Shipyard:
		return 0, fmt.Sprintf("Vitesse ×%d", level+1)
	case domainbuilding.NaniteFactory:
		if level < 63 {
			return 0, "Vitesse ×" + figure(int64(1)<<level)
		}
	case domainbuilding.ResearchLab:
		return 0, fmt.Sprintf("Laboratoire niveau %d", level)
	case domainbuilding.AllianceDepot:
		return 0, "Ravitaillement " + figure(int64(level)*10_000) + "/h"
	case domainbuilding.MissileSilo:
		return 0, fmt.Sprintf("%d emplacements", level*10)
	case domainbuilding.Terraformer:
		return 0, fmt.Sprintf("+%d cases cumulées", level*5)
	case domainbuilding.LunarBase:
		fields := planet.Rules.Expansion.BaseMoonFields + level*planet.Rules.Expansion.LunarBaseFields
		return 0, fmt.Sprintf("%d cases lunaires", fields)
	case domainbuilding.SensorPhalanx:
		return 0, fmt.Sprintf("Portée : %d systèmes", phalanx.Radius(level))
	case domainbuilding.JumpGate:
		return 0, "Recharge " + (time.Duration(planet.Rules.Expansion.JumpGateCooldownSeconds) * time.Second).String()
	}
	return 0, "—"
}

func isolatedMineOutput(id domainbuilding.ID, level int, planet appeconomy.Planet) (int64, int64, error) {
	baseRates, _, err := domaineconomy.CalculateRates(planet.Rules, domaineconomy.Levels{}, planet.MaximumTemperature)
	if err != nil {
		return 0, 0, err
	}
	levels := domaineconomy.Levels{SolarPlant: 100, FusionFuelAvailable: false}
	switch id {
	case domainbuilding.MetalMine:
		levels.MetalMine = level
	case domainbuilding.CrystalMine:
		levels.CrystalMine = level
	case domainbuilding.DeuteriumSynthesizer:
		levels.DeuteriumSynthesizer = level
	}
	rates, energy, err := domaineconomy.CalculateRates(planet.Rules, levels, planet.MaximumTemperature)
	if err != nil {
		return 0, 0, err
	}
	switch id {
	case domainbuilding.MetalMine:
		return rates.Metal - baseRates.Metal, energy.Consumed, nil
	case domainbuilding.CrystalMine:
		return rates.Crystal - baseRates.Crystal, energy.Consumed, nil
	default:
		return rates.Deuterium - baseRates.Deuterium, energy.Consumed, nil
	}
}

func researchProgress(definition research.Definition, current int, planet appeconomy.Planet,
	laboratories research.Laboratories) []technologyProgressRow {
	start, end := progressWindow(current)
	if definition.MaximumLevel > 0 && end > definition.MaximumLevel {
		end = definition.MaximumLevel
	}
	catalogue := research.DefaultCatalogue()
	rows := make([]technologyProgressRow, 0, max(0, end-start+1))
	for level := start; level <= end; level++ {
		cost, energy, err := catalogue.Cost(definition.ID, level, planet.Rules.Progression.ResearchCostMultiplier)
		if err != nil {
			break
		}
		duration, _, err := catalogue.DurationFor(definition.ID, cost, laboratories, planet.Rules)
		if err != nil {
			break
		}
		rows = append(rows, technologyProgressRow{
			Level: level, Current: level == current, Metal: cost.Metal, Crystal: cost.Crystal,
			Deuterium: cost.Deuterium, Energy: -energy, Duration: duration,
			Effect: researchEffect(definition.ID, level, planet),
		})
	}
	return rows
}

func researchEffect(id research.ID, level int, planet appeconomy.Planet) string {
	switch id {
	case research.CombustionDrive:
		return fmt.Sprintf("Vitesse +%d %%", level*10)
	case research.ImpulseDrive:
		return fmt.Sprintf("Vitesse +%d %%", level*20)
	case research.HyperspaceDrive:
		return fmt.Sprintf("Vitesse +%d %%", level*30)
	case research.WeaponsTechnology, research.ShieldingTechnology, research.ArmourTechnology:
		return fmt.Sprintf("Bonus +%d %%", level*10)
	case research.ComputerTechnology:
		return fmt.Sprintf("%d flottes simultanées", level+1)
	case research.Astrophysics:
		levels := research.Levels{research.Astrophysics: level}
		return fmt.Sprintf("%d colonies · %d expéditions", levels.ColonySlots(planet.Rules.Progression.MaximumColonies), levels.ExpeditionSlots())
	case research.IntergalacticResearchNetwork:
		return fmt.Sprintf("%d laboratoires distants", level)
	case research.EspionageTechnology:
		return fmt.Sprintf("Niveau d'espionnage %d", level)
	case research.EnergyTechnology:
		return fmt.Sprintf("Rendement fusion +%d points", level)
	default:
		return fmt.Sprintf("Niveau %d", level)
	}
}

func technologyUnlocks(kind prerequisite.Kind, id string) []technologyUnlockView {
	var result []technologyUnlockView
	appendIf := func(name string, requirements []prerequisite.Requirement) {
		for _, requirement := range requirements {
			if requirement.Kind == kind && requirement.ID == id {
				result = append(result, technologyUnlockView{Name: name, Level: requirement.Level})
				return
			}
		}
	}
	for _, definition := range domainbuilding.DefaultCatalogue().Definitions() {
		appendIf(buildingName(definition.ID), definition.Prerequisites)
	}
	for _, definition := range research.DefaultCatalogue().Definitions() {
		appendIf(researchName(definition.ID), definition.Prerequisites)
	}
	catalogue := unit.DefaultCatalogue()
	for _, family := range []unit.Family{unit.Ship, unit.Defense} {
		for _, definition := range catalogue.Definitions(family) {
			appendIf(unitName(definition.ID), definition.Prerequisites)
		}
	}
	return result
}

// unitRole gives the modal a useful first answer before its numeric sheet.
func unitRole(id unit.ID) string {
	roles := map[unit.ID]string{
		unit.SmallCargo:            "Transport rapide pour les petites cargaisons et les débuts d'empire.",
		unit.LargeCargo:            "Transport à grande capacité pour les échanges et les déplacements de ressources.",
		unit.LightFighter:          "Chasseur économique employé en nombre pour absorber et infliger des tirs.",
		unit.HeavyFighter:          "Chasseur renforcé, plus résistant et mieux armé que le modèle léger.",
		unit.Cruiser:               "Vaisseau polyvalent particulièrement efficace contre les chasseurs légers et les lanceurs de missiles.",
		unit.Battleship:            "Vaisseau de ligne rapide, solide et doté d'une importante puissance de feu.",
		unit.ColonyShip:            "Fonde une nouvelle colonie sur une position libre.",
		unit.Recycler:              "Collecte le métal et le cristal des champs de débris.",
		unit.EspionageProbe:        "Observe un monde adverse ; la technologie d'espionnage décide de la précision du rapport.",
		unit.Bomber:                "Vaisseau lourd spécialisé dans la destruction des défenses planétaires.",
		unit.SolarSatellite:        "Produit de l'énergie selon la température du monde mais ne peut pas se déplacer.",
		unit.Destroyer:             "Vaisseau lourd à la coque, aux boucliers et à l'armement considérables.",
		unit.Deathstar:             "Plateforme de combat ultime, extrêmement lente mais capable d'anéantir presque toute opposition.",
		unit.Battlecruiser:         "Chasseur lourd rapide conçu pour affronter les grands vaisseaux de ligne.",
		unit.RocketLauncher:        "Défense simple et peu coûteuse, utile en grand nombre.",
		unit.LightLaser:            "Défense laser légère au bon rapport entre coût et puissance.",
		unit.HeavyLaser:            "Version renforcée du laser, plus résistante et plus puissante.",
		unit.GaussCannon:           "Canon électromagnétique infligeant de lourds dégâts aux vaisseaux intermédiaires.",
		unit.IonCannon:             "Défense dotée d'un bouclier élevé et d'une arme ionique.",
		unit.PlasmaTurret:          "Défense planétaire la plus puissante contre les vaisseaux lourds.",
		unit.SmallShieldDome:       "Bouclier planétaire compact ; un seul exemplaire peut être construit.",
		unit.LargeShieldDome:       "Bouclier planétaire majeur ; un seul exemplaire peut être construit.",
		unit.AntiBallisticMissile:  "Intercepte un missile interplanétaire ennemi avant son impact.",
		unit.InterplanetaryMissile: "Frappe et détruit les défenses d'une planète à distance.",
	}
	return roles[id]
}
