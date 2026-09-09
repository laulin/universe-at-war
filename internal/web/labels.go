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
		building.Terraformer: "Terraformeur", building.LunarBase: "Base lunaire",
		building.SensorPhalanx: "Phalange de capteurs", building.JumpGate: "Porte de saut",
	}
	if name := names[id]; name != "" {
		return name
	}
	return string(id)
}

// buildingRole says what a building is for, in one or two sentences. A player
// reading a catalogue of sixteen entries needs to know what each one does
// before its price means anything, and the entry that touches the energy
// balance says so, because the card puts a figure of energy next to it.
func buildingRole(id building.ID) string {
	roles := map[building.ID]string{
		building.MetalMine:            "Extrait le métal, la ressource de base de toute construction. Chaque niveau produit davantage et consomme plus d'énergie.",
		building.CrystalMine:          "Extrait le cristal, qu'exigent l'électronique, les recherches et les vaisseaux les plus avancés. Chaque niveau consomme plus d'énergie.",
		building.DeuteriumSynthesizer: "Condense le deutérium, carburant des flottes et matière des recherches lourdes. Son rendement dépend de la température du monde, et chaque niveau consomme plus d'énergie.",
		building.SolarPlant:           "Produit l'énergie que les trois mines consomment. Sans énergie suffisante, elles tournent au ralenti et la production s'effondre.",
		building.MetalStorage:         "Double la capacité de stockage du métal à chaque niveau. Ce qui dépasse la capacité est perdu.",
		building.CrystalStorage:       "Double la capacité de stockage du cristal à chaque niveau. Ce qui dépasse la capacité est perdu.",
		building.DeuteriumTank:        "Double la capacité de stockage du deutérium à chaque niveau. Ce qui dépasse la capacité est perdu.",
		building.RoboticsFactory:      "Accélère toutes les constructions du monde, la sienne comprise, et ouvre l'accès au chantier spatial puis à l'usine de nanites.",
		building.NaniteFactory:        "Divise par deux la durée de chaque construction et de chaque production à chaque niveau. Le bâtiment le plus cher du catalogue.",
		building.Shipyard:             "Construit les vaisseaux et les défenses. Chaque niveau accélère la production, et le silo à missiles en dépend.",
		building.ResearchLab:          "Mène les recherches et fixe leur vitesse. Il ne peut pas être agrandi pendant qu'une recherche est en cours.",
		building.MissileSilo:          "Abrite les missiles d'interception et les missiles interplanétaires. Chaque niveau ajoute des emplacements.",
		building.Terraformer:          "Gagne des cases constructibles sur un monde devenu trop petit. Il n'y a pas d'autre moyen d'en obtenir.",
		building.LunarBase:            "Rend la lune habitable et fixe le nombre de cases qu'elle offre. Rien d'autre ne peut y être bâti avant elle.",
		building.SensorPhalanx:        "Observe les mouvements de flotte autour des mondes voisins. Chaque niveau étend sa portée, et chaque observation coûte du deutérium.",
		building.JumpGate:             "Déplace une flotte d'une lune à une autre sans temps de trajet. Les deux lunes doivent en être équipées.",
	}
	return roles[id]
}

// facilityBusyReason says which activity is holding an installation, since the
// player has to finish or cancel it before the upgrade can be ordered.
func facilityBusyReason(id building.ID) string {
	switch id {
	case building.ResearchLab:
		return "Une recherche occupe ce laboratoire : terminez-la ou annulez-la d'abord."
	case building.Shipyard, building.NaniteFactory:
		return "Une production occupe cette installation : terminez-la ou annulez-la d'abord."
	default:
		return "Une autre progression occupe cette installation."
	}
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
	return requirementLabel(requirement) + " niveau " + itoa(requirement.Level)
}

// requirementLabel names a prerequisite without its level, for a card that
// shows the level reached beside the level required rather than in a sentence.
func requirementLabel(requirement prerequisite.Requirement) string {
	switch requirement.Kind {
	case prerequisite.Building:
		return buildingName(building.ID(requirement.ID))
	case prerequisite.Research:
		return researchName(research.ID(requirement.ID))
	}
	return requirement.ID
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
