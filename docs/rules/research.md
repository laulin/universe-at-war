# Recherches — règle de fidélité

## Comportement attendu

Un joueur possède un niveau entier positif ou nul par recherche. Les niveaux
appartiennent au joueur, pas à la planète : une recherche terminée profite à tout
l'empire. Le joueur tient une file de recherche d'au plus
`progression.queue_length` ordres, dix par défaut, dont un seul se mène à la
fois, quel que soit le nombre de planètes.

Mettre une recherche en file règle d'abord la production de la planète de
lancement, vérifie les prérequis de bâtiments et de recherches — en y comptant
les niveaux que la file atteint déjà —, calcule le coût, **débite les ressources
immédiatement** sur cette planète et ajoute l'ordre à la fin de la file. La durée
n'est décidée qu'au moment où l'ordre prend la tête, avec les laboratoires du
joueur à cet instant. Le niveau ne change qu'à l'achèvement.

Un ordre s'annule à tout moment et rembourse intégralement **la planète depuis
laquelle il a été lancé**, écrêté à la capacité de ses entrepôts — annuler depuis
une autre planète ne déplace donc aucune ressource. L'annulation emporte les
niveaux suivants de la même technologie et les recherches dont l'ordre supprimé
fournissait le prérequis.

Le laboratoire ne s'améliore pas tant qu'une recherche est active ou en attente,
et réciproquement une recherche ne démarre pas tant que le laboratoire est
quelque part dans la file de construction de sa planète.

Le laboratoire de la planète de lancement détermine la durée. Le réseau de
recherche intergalactique y ajoute les laboratoires des autres planètes du
joueur, sans jamais déplacer le coût.

## Formules

Pour le niveau cible `L >= 1`, chaque composante du coût vaut :

```text
floor(base_cost * growth^(L - 1) * research_cost_multiplier)
```

La durée dépend du travail `metal_cost + crystal_cost` et du laboratoire
effectif :

```text
laboratoire_effectif = laboratoire_local
                     + somme des `réseau` meilleurs laboratoires distants éligibles
secondes = floor(3600 * travail
                 / (1000 * (1 + laboratory_bonus * laboratoire_effectif)
                    * research_speed))
durée    = max(1 seconde, secondes)
```

`réseau` est le niveau du réseau de recherche intergalactique, nul lorsque
`research_network_enabled` est faux. Un laboratoire distant n'est éligible que si
son niveau atteint le prérequis de laboratoire de la recherche lancée : un
laboratoire incapable de mener seul la recherche ne peut pas y contribuer.

La technologie graviton ne coûte aucune ressource mais exige de l'énergie
disponible sur la planète de lancement :

```text
énergie_requise = floor(300000 * 3^(L - 1) * research_cost_multiplier)
disponible      = énergie_produite - énergie_consommée
```

L'énergie n'est pas débitée : elle doit seulement être disponible à l'instant du
lancement. La durée du graviton vaut 1 seconde, plancher de la formule générale
appliquée à un travail nul.

## Catalogue initial

Les identifiants sont persistés et indépendants de la langue. La croissance vaut
2 sauf mention contraire. Les prérequis notent le niveau minimal exigé.

| Identifiant | Coût M/C/D | Croissance | Prérequis |
| --- | ---: | ---: | --- |
| `energy_technology` | 0/800/400 | 2 | laboratoire 1 |
| `laser_technology` | 200/100/0 | 2 | laboratoire 1, énergie 2 |
| `ion_technology` | 1 000/300/100 | 2 | laboratoire 4, laser 5, énergie 4 |
| `hyperspace_technology` | 0/4 000/2 000 | 2 | laboratoire 7, énergie 5, bouclier 5 |
| `plasma_technology` | 2 000/4 000/1 000 | 2 | laboratoire 4, énergie 8, laser 10, ions 5 |
| `combustion_drive` | 400/0/600 | 2 | laboratoire 1, énergie 1 |
| `impulse_drive` | 2 000/4 000/600 | 2 | laboratoire 2, énergie 1 |
| `hyperspace_drive` | 10 000/20 000/6 000 | 2 | laboratoire 7, hyperespace 3 |
| `espionage_technology` | 200/1 000/200 | 2 | laboratoire 3 |
| `computer_technology` | 0/400/600 | 2 | laboratoire 1 |
| `astrophysics` | 4 000/8 000/4 000 | 1,75 | laboratoire 3, espionnage 4, impulsion 3 |
| `intergalactic_research_network` | 240 000/400 000/160 000 | 2 | laboratoire 10, ordinateur 8, hyperespace 8 |
| `weapons_technology` | 800/200/0 | 2 | laboratoire 4 |
| `shielding_technology` | 200/600/0 | 2 | laboratoire 6, énergie 3 |
| `armour_technology` | 1 000/0/0 | 2 | laboratoire 2 |
| `graviton_technology` | énergie 300 000 | 3 | laboratoire 12 |

## Effets exposés au domaine

Les effets sont calculés depuis les seuls niveaux, sans dépendre du ruleset, et
sont consommés par les jalons suivants :

| Effet | Formule |
| --- | --- |
| emplacements de flotte | `1 + ordinateur` |
| bonus de moteur | `1 + 0,1 * combustion`, `1 + 0,2 * impulsion`, `1 + 0,3 * hyperespace` |
| facteur d'armes, de bouclier, de protection | `1 + 0,1 * niveau` |
| emplacements de colonie | `(astrophysique + 1) / 2`, borné par le ruleset |
| emplacements d'expédition | `floor(racine(astrophysique))` |
| niveau d'espionnage | `espionnage` |
| taille du réseau | `réseau de recherche intergalactique` |

## Ordre et idempotence

L'événement d'achèvement porte la clé `research-complete:<file>` et la priorité
documentée dans les événements planifiés. À sa livraison, la file est relue : une
file qui n'est plus active fait de l'événement un succès sans effet, si bien
qu'une redélivrance après incident ne peut pas incrémenter deux fois. Le coût, la
durée et la version du ruleset sont capturés au démarrage : changer une règle ne
modifie jamais une recherche déjà lancée.

Le laboratoire de la planète de lancement ne peut pas être amélioré pendant une
recherche, et une recherche ne peut pas démarrer pendant l'amélioration de ce
laboratoire. Les deux refus sont explicites et symétriques.

## Exemples de référence

Technologie énergétique niveau 1, multiplicateur 1 : 0 métal, 800 cristal,
400 deutérium. Avec un laboratoire 1, un bonus de laboratoire 1 et une vitesse 1,
la durée vaut `floor(3600 * 800 / (1000 * 2))`, soit 1 440 secondes. Avec un
réseau de niveau 1 et un laboratoire distant de niveau 3, le laboratoire effectif
vaut 4 et la durée tombe à 576 secondes. Astrophysique niveau 2 coûte
7 000/14 000/7 000. Graviton niveau 1 exige 300 000 d'énergie disponible et dure
1 seconde ; niveau 2, 900 000.

## Cas limites et invariants

Le niveau cible vaut exactement le niveau courant plus un. Un prérequis manquant,
une ressource manquante ou une énergie insuffisante refusent le démarrage sans
aucune mutation. Deux recherches simultanées sont impossibles, y compris avec
plusieurs planètes. Un coût ou une durée non représentable est refusé, jamais
saturé silencieusement. Le graphe de prérequis est acyclique et validé à
l'activation d'un ruleset.

## Tests de référence

Tables de coûts aux niveaux bas et élevés ; tables de durée avec et sans réseau ;
exclusion d'un laboratoire distant trop faible ; refus d'un cycle et d'un
prérequis manquant ; impossibilité de deux recherches concurrentes ; double
dépense et double soumission ; achèvement exactement une fois après redélivrance
simulée ; conservation des horaires d'une recherche en cours après changement de
ruleset ; fuzz des formules de coût.
