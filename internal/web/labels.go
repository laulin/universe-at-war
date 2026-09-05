package web

import (
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
)

// This file holds every player-facing label. Identifiers stay in the domain and
// are never translated, so a persisted value never depends on the language.

func buildingName(id building.ID) string {
	names := map[building.ID]string{
		building.MetalMine: "Mine de métal", building.CrystalMine: "Mine de cristal",
		building.DeuteriumSynthesizer: "Synthétiseur de deutérium", building.SolarPlant: "Centrale solaire",
		building.MetalStorage: "Hangar de métal", building.CrystalStorage: "Hangar de cristal",
		building.DeuteriumTank: "Réservoir de deutérium", building.RoboticsFactory: "Usine de robots",
		building.NaniteFactory: "Usine de nanites", building.Shipyard: "Chantier spatial",
		building.ResearchLab: "Laboratoire de recherche", building.MissileSilo: "Silo à missiles",
		building.Terraformer: "Terraformeur",
	}
	if name := names[id]; name != "" {
		return name
	}
	return string(id)
}

func researchName(id research.ID) string {
	names := map[research.ID]string{
		research.EnergyTechnology: "Technologie énergétique", research.LaserTechnology: "Technologie laser",
		research.IonTechnology: "Technologie à ions", research.HyperspaceTechnology: "Technologie hyperespace",
		research.PlasmaTechnology: "Technologie plasma", research.CombustionDrive: "Réacteur à combustion",
		research.ImpulseDrive: "Réacteur à impulsion", research.HyperspaceDrive: "Propulsion hyperespace",
		research.EspionageTechnology: "Technologie d'espionnage", research.ComputerTechnology: "Technologie ordinateur",
		research.Astrophysics: "Astrophysique", research.IntergalacticResearchNetwork: "Réseau de recherche intergalactique",
		research.WeaponsTechnology: "Technologie armes", research.ShieldingTechnology: "Technologie bouclier",
		research.ArmourTechnology: "Protection des vaisseaux", research.GravitonTechnology: "Technologie graviton",
	}
	if name := names[id]; name != "" {
		return name
	}
	return string(id)
}

func unitName(id unit.ID) string {
	names := map[unit.ID]string{
		unit.SmallCargo: "Petit transporteur", unit.LargeCargo: "Grand transporteur",
		unit.LightFighter: "Chasseur léger", unit.HeavyFighter: "Chasseur lourd",
		unit.Cruiser: "Croiseur", unit.Battleship: "Vaisseau de bataille",
		unit.ColonyShip: "Vaisseau de colonisation", unit.Recycler: "Recycleur",
		unit.EspionageProbe: "Sonde d'espionnage", unit.Bomber: "Bombardier",
		unit.SolarSatellite: "Satellite solaire", unit.Destroyer: "Destructeur",
		unit.Deathstar: "Étoile de la mort", unit.Battlecruiser: "Traqueur",
		unit.RocketLauncher: "Lanceur de missiles", unit.LightLaser: "Artillerie laser légère",
		unit.HeavyLaser: "Artillerie laser lourde", unit.GaussCannon: "Canon de Gauss",
		unit.IonCannon: "Artillerie à ions", unit.PlasmaTurret: "Lanceur de plasma",
		unit.SmallShieldDome: "Petit bouclier planétaire", unit.LargeShieldDome: "Grand bouclier planétaire",
		unit.AntiBallisticMissile: "Missile d'interception", unit.InterplanetaryMissile: "Missile interplanétaire",
	}
	if name := names[id]; name != "" {
		return name
	}
	return string(id)
}

// requirementName describes one missing prerequisite in the player's language.
func requirementName(requirement prerequisite.Requirement) string {
	name := requirement.ID
	switch requirement.Kind {
	case prerequisite.Building:
		name = buildingName(building.ID(requirement.ID))
	case prerequisite.Research:
		name = researchName(research.ID(requirement.ID))
	}
	return name + " niveau " + itoa(requirement.Level)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
