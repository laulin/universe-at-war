# Lunes — règle de fidélité

## Comportement attendu

Une lune est un corps céleste distinct de sa planète : elle a ses propres cases,
ses propres bâtiments, son propre stock et sa propre flotte stationnée. Rien
n'est partagé avec la planète qui l'accompagne, sinon la position.

Une lune ne produit aucune ressource. Elle peut en recevoir, en stocker et en
envoyer, mais rien n'y pousse. Elle n'a pas de hangar et ne connaît donc aucun
plafond de stockage : ce qu'on y dépose y reste, ce qui permet d'y financer des
bâtiments bien plus chers que la capacité d'une planète neuve.

Une position ne porte qu'une seule lune. Une seconde tentative de création sur
une position qui en possède déjà une échoue sans effet.

## Création

Une lune naît d'un combat, jamais d'une construction. La chance est calculée à
partir des débris que la bataille vient de créer :

```text
chance = min(chance_maximale, floor((débris_métal + débris_cristal) / 100000) / 100)
```

Le tirage utilise la source dérivée de la seed persistée de la flotte
attaquante, consommée juste après la résolution du combat. Rejouer la bataille
avec la même seed rejoue donc aussi la création de la lune. Le résultat, la
chance et la seed sont journalisés.

## Cases

```text
cases_totales = cases_de_base_lunaires + niveau_base_lunaire * cases_par_niveau
```

Par défaut une lune naît avec une case et chaque niveau de base lunaire en ajoute
trois. Une lune sans base lunaire ne peut donc accueillir qu'un seul bâtiment.

## Bâtiments lunaires

Les bâtiments lunaires ne se construisent que sur une lune, et les bâtiments
planétaires ne se construisent que sur une planète. Le catalogue en distingue
explicitement le placement.

| Identifiant | Coût M/C/D | Croissance | Rôle |
| --- | ---: | ---: | --- |
| `lunar_base` | 20 000/40 000/20 000 | 2 | ajoute des cases à la lune |
| `sensor_phalanx` | 20 000/40 000/20 000 | 2 | observe les flottes alentour |
| `jump_gate` | 2 000 000/4 000 000/2 000 000 | 2 | transfert instantané entre lunes |

La durée de construction suit la formule des bâtiments, avec l'usine de robots et
l'usine de nanites de la lune elle-même, c'est-à-dire zéro tant qu'elles n'y
existent pas.

## Cas limites et invariants

Le stock, l'inventaire et la file d'une lune ne se confondent jamais avec ceux de
sa planète. Une lune ne produit rien, même avec des mines héritées : elle n'en a
pas. Détruire une lune n'est pas encore possible. Une redélivrance de l'événement
de combat ne crée pas une seconde lune.

## Tests de référence

Chance bornée et reproductible à seed égale ; une seule lune par position ;
redélivrance idempotente ; absence de production sur une lune ; refus d'un
bâtiment planétaire sur une lune et d'un bâtiment lunaire sur une planète ;
cases apportées par la base lunaire.
