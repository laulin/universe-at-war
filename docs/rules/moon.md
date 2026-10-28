# Lunes — règle de fidélité

## Comportement attendu

Une lune est un corps céleste distinct de sa planète : elle a ses propres cases,
ses propres bâtiments, son propre stock et sa propre flotte stationnée. Rien
n'est partagé avec la planète qui l'accompagne, sinon la position.

Une lune ne produit aucune ressource. Elle peut en recevoir, en stocker et en
envoyer, mais rien n'y pousse. Son stockage est sans limite, avec ou sans
hangars : ce qu'on y dépose y reste, ce qui permet d'y financer des
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
attaquante, ou de l'opération pour un combat groupé, consommée juste après la résolution du combat. Rejouer la bataille
avec la même seed rejoue donc aussi la création de la lune. Le résultat, la
chance et la seed sont journalisés. La lune appartient au propriétaire de la
planète attaquée, même si le défenseur perd. Le temps de jeu ne déclenche pas de
tirage ; 20 % est un plafond par combat, pas une garantie après cinq combats.

Le rapport indique : lune créée, tirage échoué, débris insuffisants, lune déjà
présente ou combat sur une lune. Un ancien rapport sans résultat enregistré
affiche explicitement cette absence, sans inventer un échec. Seul le
propriétaire voit un lien de gestion de sa lune.

## Cases

```text
cases_totales = cases_de_base_lunaires + niveau_base_lunaire * cases_par_niveau
```

Par défaut une lune naît avec une case et chaque niveau de base lunaire en ajoute
trois, tout en consommant une case. Une lune sans base lunaire ne peut donc
accueillir qu'un seul bâtiment. La base reste constructible ou améliorable sur
une lune pleine : son agrandissement évite de bloquer les lunes déjà occupées.
Les autres constructions exigent une case libre, réservations de file incluses.

## Bâtiments lunaires

Le catalogue distingue les bâtiments réservés à une planète, à une lune et
ceux autorisés sur les deux. Les installations propres aux lunes sont :

| Identifiant | Coût M/C/D | Croissance | Rôle |
| --- | ---: | ---: | --- |
| `lunar_base` | 20 000/40 000/20 000 | 2 | ajoute des cases à la lune |
| `sensor_phalanx` | 20 000/40 000/20 000 | 2 | observe les flottes alentour |
| `jump_gate` | 2 000 000/4 000 000/2 000 000 | 2 | transfert instantané entre lunes |

L'usine de robots, le chantier spatial et les trois hangars sont également
constructibles sur les lunes, aux coûts et prérequis du catalogue commun.
Chaque bâtiment utilise une case par niveau. Les hangars n'augmentent pas la
production d'une lune et son stockage reste sans limite : leur fiche prévient
de cette absence de gain. Mines, centrales, laboratoire, nanites, terraformation,
dépôt d'alliance et silo restent réservés aux planètes.

La durée de construction utilise l'usine de robots de la lune elle-même. Le
chantier utilise ses propres niveaux et produit localement vaisseaux et défenses
compatibles ; une production empêche son amélioration jusqu'à sa fin ou son
annulation. Les missiles nécessitant un silo restent indisponibles.

## Missions et interface

La page `/planets/{id}` affiche les installations, stocks et cases de la lune,
sa planète mère, le ravitaillement, la phalange et la porte de saut. La navigation
regroupe chaque lune avec sa planète ; les recherches renvoient à la planète.

Le formulaire de flotte distingue planète et lune, même à coordonnées égales.
Transport, stationnement permanent, défense alliée, attaque et espionnage peuvent
cibler une lune. La destination est conservée pendant la confirmation et
l'arrivée relit le type de corps. Les flottes alliées défendent uniquement le
corps qu'elles ont rejoint. Les opérations d'attaque groupée restent limitées
aux planètes dans le schéma actuel ; l'interface ne les propose pas pour une lune.

## Cas limites et invariants

Le stock, l'inventaire et la file d'une lune ne se confondent jamais avec ceux de
sa planète. Une lune ne produit rien, même avec des mines héritées : elle n'en a
pas. Détruire une lune n'est pas encore possible. Une redélivrance de l'événement
de combat ne crée pas une seconde lune.

## Tests de référence

Chance bornée et reproductible à seed égale ; une seule lune par position ;
redélivrance idempotente ; absence de production sur une lune ; refus d'un
bâtiment planétaire sur une lune et d'un bâtiment lunaire sur une planète ;
cases apportées par la base lunaire ; récupération d'une lune pleine ; transport
et stationnement planète/lune sans mélange de stocks ; chantier lunaire ;
réussite et échec déterministes à 20 % ; résultat visible et anciens rapports
compatibles ; illustrations réelles pour tous les bâtiments.
