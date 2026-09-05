# Bâtiments et coûts — règle de fidélité

## Comportement attendu

Une planète possède un niveau entier positif ou nul par bâtiment. Une seule
construction de bâtiment peut être active par planète. Démarrer une construction
règle d'abord la production, vérifie les prérequis, les cases libres et le coût,
débite les ressources, crée la file et son événement planifié dans une même
transaction. Le niveau ne change qu'à l'achèvement.

Le catalogue initial contient les mines de métal/cristal/deutérium, la centrale
solaire, les trois stockages, l'usine de robots, l'usine de nanites, le chantier
spatial, le laboratoire, le silo à missiles et le terraformeur. Les bâtiments
lunaires seront ajoutés avec les lunes.

## Formules

Pour construire le niveau cible `L >= 1`, chaque composante du coût vaut :

```text
floor(base_cost * growth^(L - 1) * building_cost_multiplier)
```

Le facteur de croissance est propre au bâtiment. La durée est :

```text
work    = metal_cost + crystal_cost
seconds = floor(3600 * work
                / (2500 * (1 + robotics_level) * 2^nanite_level
                   * building_speed))
duration = max(1 seconde, seconds)
```

Le chantier, le laboratoire et les autres bâtiments n'accélèrent pas les
bâtiments. La construction consomme une case ; le terraformeur augmente le total
de 5 cases à son achèvement.

## Catalogue initial

| Identifiant | Coût M/C/D | Croissance | Prérequis |
| --- | ---: | ---: | --- |
| `metal_mine` | 60/15/0 | 1,5 | — |
| `crystal_mine` | 48/24/0 | 1,6 | — |
| `deuterium_synthesizer` | 225/75/0 | 1,5 | — |
| `solar_plant` | 75/30/0 | 1,5 | — |
| `metal_storage` | 1 000/0/0 | 2 | — |
| `crystal_storage` | 1 000/500/0 | 2 | — |
| `deuterium_tank` | 1 000/1 000/0 | 2 | — |
| `robotics_factory` | 400/120/200 | 2 | — |
| `nanite_factory` | 1 000 000/500 000/100 000 | 2 | usine robots 10 |
| `shipyard` | 400/200/100 | 2 | usine robots 2 |
| `research_lab` | 200/400/200 | 2 | — |
| `missile_silo` | 20 000/20 000/1 000 | 2 | chantier 1 |
| `terraformer` | 0/50 000/100 000 | 2 | nanites 1 |

Les identifiants sont persistés et ne dépendent pas de la langue d'affichage.

## Ordre et idempotence

Les événements sont sélectionnés par `(due_at, priority, id)`. Une fin de
construction utilise la clé `building-complete:<queue-id>` et vérifie encore que
la file est active. L'incrément de niveau, la suppression logique de la file,
l'écriture `building_completed` et le passage de l'événement à `completed` sont
atomiques. Une reprise après crash ne peut donc incrémenter qu'une fois.

## Exemples de référence

- Mine de métal niveau 1, multiplicateur 1 : 60 métal et 15 cristal.
- Mine de métal niveau 2 : 90 métal et 22 cristal (22,5 arrondi vers le bas).
- Mine de métal niveau 1 sans robot, vitesse 1 : `floor(3600 * 75 / 2500)`, soit
  108 secondes.
- Avec usine de robots niveau 1, la même construction dure 54 secondes.

## Cas limites et invariants

- niveau cible exactement égal au niveau courant + 1 ;
- coûts et durées calculés avec le ruleset capturé au démarrage ;
- refus si prérequis, ressources ou case manquent ;
- jamais deux files actives sur une planète ;
- aucun débit sans création de file et d'événement ;
- aucune seconde consommation de case lors d'une reprise d'événement ;
- coût ou durée non représentable rejeté, jamais saturé silencieusement.

## Tests de référence

- table de coûts des niveaux 1, 2 et élevés ;
- influence robots, nanites, vitesse et durée minimale ;
- prérequis et cases ;
- démarrage atomique et refus pour ressources insuffisantes ;
- double soumission et double dépense concurrente ;
- achèvement exactement une fois, y compris après reprise simulée ;
- ordre stable de deux événements au même instant.
