# Expéditions — règle de fidélité

## Comportement attendu

Une expédition envoie une flotte au-delà de la dernière planète d'un système,
sur une position qui n'appartient à personne, et la fait attendre là un moment
avant de rentrer. Ce qu'elle y trouve est tiré au sort, une fois, avec une seed
persistée : la même expédition rejouée donne exactement le même résultat.

L'expédition ne remplace ni le PvP ni le développement : elle est bornée par les
emplacements d'expédition de l'astrophysique, par la soute de la flotte envoyée
et par un facteur du ruleset. Elle peut aussi coûter la flotte.

## Destination

La position d'expédition d'un système est celle qui suit sa dernière planète :

```text
position_d_expédition = positions_par_système + 1
```

Aucun corps n'y existe et aucun n'y sera jamais créé. Une expédition vers toute
autre position est refusée. La distance se calcule comme pour n'importe quelle
mission, en traitant cette position comme une position ordinaire.

## Conditions

- la mission est activée dans le ruleset ;
- la flotte compte au moins un vaisseau capable de porter quelque chose ;
- le joueur a un emplacement d'expédition libre :
  `floor(√astrophysique)` moins les expéditions déjà en vol ;
- le carburant, le cargo et les emplacements de flotte suivent les règles
  ordinaires.

## Durée

```text
aller    = durée de mission ordinaire vers la position d'expédition
attente  = heures_d_expédition
retour   = aller
```

L'attente est fixée par le ruleset et vaut une heure par défaut. Le résultat est
tiré à la fin de l'attente, pas à l'arrivée : c'est ce qui se passe là-bas qui
compte.

## Table des résultats

Le tirage suit des poids configurables dont la somme fait le total :

| Résultat | Poids | Effet |
| --- | ---: | --- |
| `resources` | 30 | une trouvaille de métal, cristal et deutérium |
| `nothing` | 25 | rien du tout |
| `ships` | 10 | quelques vaisseaux ramenés |
| `delay` | 10 | le retour est retardé |
| `pirates` | 10 | une bataille contre des pirates |
| `aliens` | 5 | une bataille contre des aliens, plus forts |
| `losses` | 5 | une partie de la flotte est perdue |
| `rare` | 5 | une trouvaille exceptionnelle |

Le tirage se fait sur la somme des poids : un poids nul retire simplement le
résultat de la table. Une table entièrement nulle est refusée par le ruleset.

## Effets

```text
capacité      = soute totale de la flotte − cargo déjà embarqué
trouvaille    = floor(capacité × facteur_de_ressources)
trouvaille_rare = floor(capacité × facteur_rare)
```

La trouvaille est répartie moitié métal, un tiers cristal, un sixième
deutérium, puis plafonnée par la capacité restante : une expédition ne ramène
jamais plus que ce qu'elle peut porter.

- `ships` : la flotte gagne `floor(capacité × facteur_de_vaisseaux / coût)`
  petits transporteurs, au moins un, au plus ce que la table permet.
- `delay` : le retour est repoussé de `facteur_de_retard × durée d'attente`.
- `losses` : une part de chaque type de vaisseau est perdue, arrondie vers le
  bas, jamais la totalité de la flotte.
- `pirates` et `aliens` : une vraie bataille, résolue par le même moteur que
  toute autre, contre une flotte engendrée depuis la seed de l'expédition. Les
  pertes sont réelles, les débris tombent sur la position d'expédition, et une
  flotte anéantie ne rentre pas.

## Seed et rejouabilité

La seed de l'expédition est celle de la flotte, tirée au lancement et
persistée. Le tirage du résultat, la composition des pirates et la bataille
consomment la même source, dans cet ordre : rejouer une expédition depuis sa
seed rejoue le résultat entier.

Le rapport d'expédition est immuable et écrit à la résolution, comme tout autre
rapport.

## Cas limites et invariants

Une expédition sans emplacement libre est refusée au lancement. Une flotte
anéantie sur place ne rentre pas et ne ramène rien. Une trouvaille ne dépasse
jamais la capacité restante. Le résultat est appliqué dans la même transaction
que la programmation du retour : une redélivrance de l'événement ne double
jamais une trouvaille. Aucune expédition ne crée de vaisseau au-delà de ce que
la table autorise.

## Tests de référence

Distribution des tirages sur un grand nombre de seeds ; reproductibilité à seed
égale ; refus sans emplacement ; plafonnement par la soute ; bataille de pirates
avec pertes réelles ; retard appliqué au retour ; redélivrance idempotente ;
rapport immuable.
