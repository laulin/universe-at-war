# Vaisseaux et défenses — règle de fidélité

## Comportement attendu

Une planète possède une quantité entière positive ou nulle par unité. Les
vaisseaux et les défenses tiennent chacun leur file, d'au plus
`progression.queue_length` lots, dix par défaut, et avancent en parallèle : un
lot de chasseurs ne retient plus un lanceur de missiles. Un lot est une quantité
d'un seul modèle.

Passer une commande règle d'abord la production, vérifie les prérequis, la
quantité demandée, les limites de l'unité — en y comptant ce que les files
doivent encore, de sorte qu'un second dôme de protection ne se glisse pas
derrière le premier — et le coût total, **débite les ressources immédiatement**
et ajoute le lot à la fin de sa file. La durée unitaire d'un lot n'est décidée
qu'au moment où il prend la tête.

Un lot s'annule à tout moment et rembourse les unités que le chantier devait
encore ; celles déjà livrées restent acquises. Le remboursement est écrêté à la
capacité des entrepôts.

Le chantier spatial et l'usine de nanites ne s'améliorent pas tant qu'un lot est
en cours ou en attente, et réciproquement aucun lot ne se commande tant que l'un
des deux est quelque part dans la file de construction de la planète.

Les unités sont livrées au fur et à mesure : à chaque règlement, la commande
livre les unités entièrement produites depuis son démarrage. Une unité livrée est
immédiatement utilisable, avant la fin du lot. L'événement d'achèvement livre le
reliquat et clôt la commande, qui est alors la seule source de clôture.

## Formules

Le coût unitaire vaut, composante par composante :

```text
floor(base_cost * multiplicateur)
```

Le multiplicateur est `ship_cost_multiplier` pour les vaisseaux et
`defense_cost_multiplier` pour les défenses. Le coût total est le coût unitaire
multiplié par la quantité ; un dépassement de capacité entière refuse la
commande, il n'est jamais saturé.

La durée unitaire dépend du travail `metal_cost + crystal_cost` du coût
unitaire :

```text
secondes = floor(3600 * travail
                 / (2500 * (1 + chantier) * 2^nanites * vitesse))
durée_unitaire = max(1 seconde, secondes)
durée_totale   = durée_unitaire * quantité
```

La vitesse est `shipyard_speed` pour les vaisseaux et `defense_speed` pour les
défenses. La livraison à un instant donné vaut :

```text
produites = min(quantité, floor((instant - départ) / durée_unitaire))
à_livrer  = produites - déjà_livrées
```

L'arithmétique est entière et monotone : deux règlements rapprochés livrent
exactement ce qu'un seul règlement aurait livré.

## Limites de quantité

Une commande porte de 1 à 1 000 000 unités. Les boucliers planétaires sont
uniques : la quantité possédée, en cours de production et commandée ne peut pas
dépasser un. Les missiles occupent des emplacements de silo : le silo offre
`10 * niveau` emplacements, un missile d'interception en occupe un et un missile
interplanétaire deux.

## Catalogue initial

Les identifiants sont persistés et indépendants de la langue.

### Vaisseaux

| Identifiant | Coût M/C/D | Bouclier | Arme | Cargo | Vitesse | Moteur | Carburant | Prérequis |
| --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | --- |
| `small_cargo` | 2 000/2 000/0 | 10 | 5 | 5 000 | 5 000 | combustion | 10 | chantier 2, combustion 2 |
| `large_cargo` | 6 000/6 000/0 | 25 | 5 | 25 000 | 7 500 | combustion | 50 | chantier 4, combustion 6 |
| `light_fighter` | 3 000/1 000/0 | 10 | 50 | 50 | 12 500 | combustion | 20 | chantier 1, combustion 1 |
| `heavy_fighter` | 6 000/4 000/0 | 25 | 150 | 100 | 10 000 | impulsion | 75 | chantier 3, protection 2, impulsion 2 |
| `cruiser` | 20 000/7 000/2 000 | 50 | 400 | 800 | 15 000 | impulsion | 300 | chantier 5, impulsion 4, ions 2 |
| `battleship` | 45 000/15 000/0 | 200 | 1 000 | 1 500 | 10 000 | hyperespace | 500 | chantier 7, propulsion hyperespace 4 |
| `colony_ship` | 10 000/20 000/10 000 | 100 | 50 | 7 500 | 2 500 | impulsion | 1 000 | chantier 4, impulsion 3 |
| `recycler` | 10 000/6 000/2 000 | 10 | 1 | 20 000 | 2 000 | combustion | 300 | chantier 4, combustion 6, bouclier 2 |
| `espionage_probe` | 0/1 000/0 | 0 | 0 | 5 | 100 000 000 | combustion | 1 | chantier 3, combustion 3, espionnage 2 |
| `bomber` | 50 000/25 000/15 000 | 500 | 1 000 | 500 | 4 000 | impulsion | 1 000 | chantier 8, impulsion 6, plasma 5 |
| `solar_satellite` | 0/2 000/500 | 1 | 1 | 0 | 0 | — | 0 | chantier 1 |
| `destroyer` | 60 000/50 000/15 000 | 500 | 2 000 | 2 000 | 5 000 | hyperespace | 1 000 | chantier 9, propulsion hyperespace 6, hyperespace 5 |
| `deathstar` | 5 000 000/4 000 000/1 000 000 | 50 000 | 200 000 | 1 000 000 | 100 | hyperespace | 1 | chantier 12, propulsion hyperespace 7, hyperespace 6, graviton 1 |
| `battlecruiser` | 30 000/40 000/15 000 | 400 | 700 | 750 | 10 000 | hyperespace | 250 | chantier 8, hyperespace 5, propulsion hyperespace 5, laser 12 |

### Défenses

| Identifiant | Coût M/C/D | Bouclier | Arme | Limites | Prérequis |
| --- | ---: | ---: | ---: | --- | --- |
| `rocket_launcher` | 2 000/0/0 | 20 | 80 | — | chantier 1 |
| `light_laser` | 1 500/500/0 | 25 | 100 | — | chantier 2, énergie 1, laser 3 |
| `heavy_laser` | 6 000/2 000/0 | 100 | 250 | — | chantier 4, énergie 3, laser 6 |
| `gauss_cannon` | 20 000/15 000/2 000 | 200 | 1 100 | — | chantier 6, énergie 6, armes 3, bouclier 1 |
| `ion_cannon` | 2 000/6 000/0 | 500 | 150 | — | chantier 4, ions 4 |
| `plasma_turret` | 50 000/50 000/30 000 | 300 | 3 000 | — | chantier 8, plasma 7 |
| `small_shield_dome` | 10 000/10 000/0 | 2 000 | 1 | un seul | chantier 1, bouclier 2 |
| `large_shield_dome` | 50 000/50 000/0 | 10 000 | 1 | un seul | chantier 6, bouclier 6 |
| `anti_ballistic_missile` | 8 000/0/2 000 | 1 | 1 | 1 emplacement de silo | chantier 1, silo 2 |
| `interplanetary_missile` | 12 500/2 500/10 000 | 1 | 12 000 | 2 emplacements de silo | chantier 1, silo 4, impulsion 1 |

Le feu rapide, la coque et les facteurs technologiques ne servent qu'au moteur de
combat ; ils sont décrits dans la fiche de combat. La coque vaut
`(métal + cristal) / 10` du coût de base.

## Satellites solaires

Un satellite solaire produit de l'énergie sans consommer de case :

```text
énergie_par_satellite = min(50, max(0, floor((température_maximale + 160) / 6)))
```

Les satellites livrés pendant un intervalle comptent à partir du règlement
suivant : la production d'un intervalle utilise le nombre de satellites connu à
son début. L'écart est borné par la durée d'une commande et ne concerne qu'une
planète dont la production de satellites est en cours.

## Ordre et idempotence

L'événement d'achèvement porte la clé `production-complete:<commande>` et la
priorité documentée dans les événements planifiés. À sa livraison, la commande
est relue : une commande qui n'est plus active fait de l'événement un succès sans
effet. Le coût unitaire, la durée unitaire et la version du ruleset sont capturés
au démarrage.

Le chantier spatial et l'usine de nanites ne peuvent pas être améliorés pendant
une commande, et une commande ne peut pas démarrer pendant leur amélioration.

## Exemples de référence

Un chasseur léger coûte 3 000 métal et 1 000 cristal ; avec un chantier 1, aucune
nanite et une vitesse 1, sa durée unitaire vaut
`floor(3600 * 4000 / (2500 * 2))`, soit 2 880 secondes. Trois chasseurs coûtent
9 000/3 000/0 et durent 8 640 secondes ; après 5 760 secondes, deux sont livrés.
Un lance-missiles dure 1 440 secondes dans les mêmes conditions. Sur une planète
dont la température maximale vaut 40, chaque satellite produit 33 d'énergie.

## Cas limites et invariants

Une quantité nulle, négative, supérieure au plafond ou dont le coût déborde est
refusée sans mutation. Un inventaire n'est jamais négatif, y compris sous
contrainte de base. Aucune unité n'apparaît sans débit correspondant, et une
commande close a livré exactement sa quantité. Deux commandes concurrentes sur la
même planète n'en laissent qu'une active.

## Tests de référence

Tables de coûts et de durées unitaires ; coût total exact et refus de
débordement ; livraison incrémentale aux bornes exactes ; limites d'unicité et de
silo ; prérequis manquants ; double soumission et double dépense concurrente ;
achèvement exactement une fois après redélivrance simulée ; conservation des
coûts d'une commande en cours après changement de ruleset ; énergie des
satellites.
