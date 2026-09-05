# Distances — règle de fidélité

## Comportement attendu

La distance entre deux positions dépend de la topologie active. Elle est entière,
strictement positive, symétrique, et ne dépend d'aucun état de jeu : deux
coordonnées suffisent à la calculer.

## Formules

```text
galaxies différentes : inter_galaxy_distance * delta_galaxie
même galaxie         : 2700 + inter_system_distance * delta_systeme
même système         : 1000 + 5 * |position_a - position_b|
même position        : 5
```

Les écarts sont circulaires lorsque la topologie l'est :

```text
delta = |a - b|
si circulaire : delta = min(delta, total - delta)
```

La distance de 5 sépare deux corps d'une même position, par exemple une planète
et sa lune ou un champ de débris. Elle garantit une durée non nulle sans rendre
le trajet gratuit.

## Exemples de référence

Avec la topologie par défaut (3 galaxies, 100 systèmes, 15 positions,
circulaire, 95 entre systèmes, 20 000 entre galaxies) :

| Départ | Arrivée | Distance |
| --- | --- | ---: |
| 1:1:8 | 1:1:10 | 1 010 |
| 1:1:8 | 1:5:8 | 3 080 |
| 1:100:8 | 1:1:8 | 2 795 |
| 1:1:8 | 2:1:8 | 20 000 |
| 3:1:8 | 1:1:8 | 20 000 |
| 1:1:8 | 1:1:8 | 5 |

Sans circularité des systèmes, `1:100:8 → 1:1:8` vaut `2700 + 95 * 99`, soit
12 105.

## Cas limites et invariants

Une coordonnée hors des limites de l'univers est refusée. La distance est
toujours strictement positive, si bien qu'aucune mission n'a une durée nulle.
Elle est symétrique : le retour coûte le même trajet que l'aller.

## Tests de référence

Table des six cas ci-dessus, avec et sans circularité ; fuzz vérifiant la
symétrie, la positivité et le refus des coordonnées invalides.
