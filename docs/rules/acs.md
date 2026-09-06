# Attaques et défenses groupées — règle de fidélité

## Comportement attendu

Une opération groupée réunit plusieurs flottes de la même alliance sur une même
cible, à la même seconde, pour ne livrer qu'un seul combat. Elle ne crée aucun
pouvoir nouveau : chaque flotte part de sa propre planète, consomme son propre
carburant et subit ses propres pertes.

Une opération a un propriétaire, une cible, un état et une heure d'arrivée. Les
seules personnes qui peuvent y joindre une flotte sont les membres de l'alliance
du propriétaire.

## Cycle de vie

```text
forming → locked → resolved
forming → cancelled
```

Une opération naît `forming` quand son propriétaire lance la première flotte.
Elle passe `locked` au moment de l'arrivée, juste avant la résolution : plus
personne ne rejoint, plus personne ne se retire. Elle devient `resolved` une
fois le combat livré, ou `cancelled` si toutes ses flottes sont reparties avant
l'arrivée.

## Synchronisation des arrivées

Toutes les flottes d'une opération arrivent ensemble, à l'heure de l'opération.

Quand une flotte rejoint :

- si son trajet propre l'amènerait **après** l'heure de l'opération, l'opération
  est retardée à cette nouvelle heure et toutes les flottes déjà engagées voient
  leur arrivée repoussée d'autant. Une flotte lente ralentit donc tout le monde,
  et le joueur le voit avant de confirmer ;
- si son trajet propre l'amènerait **avant**, elle ralentit pour arriver à
  l'heure de l'opération.

Une flotte ne rejoint jamais une opération dont l'heure d'arrivée est déjà
passée. Le retour de chaque flotte est calculé depuis l'heure d'arrivée du
groupe, avec sa propre durée de trajet.

## Fenêtre d'ajout et de retrait

Une flotte peut rejoindre ou se retirer tant que l'opération est `forming`,
c'est-à-dire tant que l'arrivée n'est pas due. Se retirer, c'est rappeler sa
flotte : elle rentre pour le temps déjà parcouru, comme n'importe quel rappel.
Le propriétaire qui retire sa propre flotte ne dissout pas l'opération tant
qu'une autre flotte y reste ; l'opération est annulée quand la dernière part.

Un participant exclu de l'alliance ou parti de lui-même garde la flotte qu'il a
déjà engagée : on ne retire pas des vaisseaux du ciel par une décision
administrative. Il ne peut simplement plus en ajouter.

## Résolution

À l'heure de l'opération, un seul combat est résolu. Les attaquants sont les
flottes de l'opération, une partie par flotte, chacune avec les technologies de
son propriétaire. Les défenseurs sont la planète visée et les flottes alliées
qui y stationnent en défense groupée.

La seed du combat est celle de l'opération, tirée à sa création et persistée :
l'opération entière est rejouable.

Une cible disparue avant l'arrivée annule l'opération et renvoie toutes ses
flottes. Une opération dont toutes les flottes sont reparties ne livre pas de
combat.

## Partage du butin et des débris

Le butin est calculé une fois, sur la capacité restante cumulée des survivants
attaquants. Il est ensuite réparti entre les flottes survivantes
proportionnellement à leur capacité restante, la plus grosse part revenant à qui
a le plus de place. Les restes de division vont aux plus grandes capacités
d'abord, si bien que la somme distribuée égale exactement le butin.

Les débris tombent au sol comme après n'importe quelle bataille : ils
n'appartiennent à personne et se disputent aux recycleurs.

## Rapports

Chaque participant reçoit son propre rapport de la même bataille, avec la même
issue, les mêmes rounds et les mêmes pertes de tous les camps : ce qu'un
participant a pu observer, les autres l'ont observé aussi. Le défenseur reçoit
le sien, marqué hostile.

## Défense groupée

Une flotte envoyée en `hold` sur une planète alliée y reste jusqu'à une heure de
fin choisie au départ, puis rentre. Cette heure doit tomber après l'arrivée et
au plus tard `maximum_hold_hours` après elle ; toute autre valeur est refusée au
lancement. La destination doit appartenir au joueur lui-même ou à un membre de
son alliance, et la défense groupée doit être activée dans le ruleset.

Tant qu'elle est là, la flotte défend : elle compte parmi les défenseurs de tout
combat qui se résout sur cette position, avec les technologies de son
propriétaire, et elle subit ses propres pertes. Une flotte anéantie en défendant
ne rentre jamais : sa fin de garde est annulée avec elle. Une flotte qui survit
repart à la fin de sa garde et vole vers son origine pour la durée de son propre
trajet.

Une flotte en défense ne pille pas et ne recycle pas. La planète reste maîtresse
de ses ressources : la défense groupée protège, elle ne partage pas.

## Cas limites et invariants

Une opération ne réunit que des flottes de la même alliance. Une flotte
n'appartient qu'à une opération. Le combat ne crée ni ne détruit de vaisseaux
hors de ses propres pertes. Le butin distribué ne dépasse jamais le butin
calculé, ni la capacité de qui le reçoit. Une redélivrance de l'événement
d'arrivée ne livre pas un second combat.

## Tests de référence

Arrivée simultanée de trois flottes ; flotte lente retardant tout le groupe ;
retrait à la limite de la fenêtre ; refus de rejoindre après l'arrivée ; refus
d'un non-membre ; participant sorti de l'alliance ; cible disparue ; combat
multi-acteurs avec conservation des vaisseaux ; répartition exacte du butin ;
défense groupée participant à la bataille ; redélivrance idempotente.
