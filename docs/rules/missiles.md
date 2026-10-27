# Missiles interplanétaires

Le tir part du corps sélectionné dans la vue galaxie. Une planète peut lancer
des missiles sur une planète ou une lune adverse. Le bouton reste grisé lorsque
le stock est vide, le silo insuffisant, la cible hors de portée, les missiles
désactivés ou le corps de départ lunaire. Le formulaire propose une quantité et
une défense prioritaire, puis une confirmation indiquant que le tir est
irréversible. La préparation ne consomme aucun missile.

## Portée et trajet

Le départ nécessite un silo de niveau 4, une impulsion de niveau 1 et les
missiles déjà livrés. La capacité du silo reste celle du chantier :
`intercepteurs + 2 × missiles interplanétaires ≤ 10 × niveau du silo`.
Le tir libère immédiatement les emplacements occupés par les missiles envoyés.

```text
portée = 5 × niveau_impulsion − 1
distance = écart entre les systèmes
durée_secondes = max(1, floor((30 + 60 × distance) / hostile_fleet_speed))
```

Les deux corps doivent être dans la même galaxie. Si les systèmes sont
circulaires, l'écart est le minimum des deux chemins autour de la galaxie.
La position dans un système ne modifie pas la durée : un tir dans son propre
système dure 30 secondes à vitesse 1. Le minimum des missions de flotte ne
s'applique pas aux missiles.

La date d'arrivée et la version du ruleset sont enregistrées au départ et ne
changent plus. Une désactivation ultérieure des missiles bloque les nouveaux
tirs et laisse les salves engagées atteindre leur cible. Un tir n'occupe aucun
emplacement de flotte, ne brûle aucun carburant supplémentaire, n'a pas de
retour et ne peut pas être rappelé. Il n'apparaît pas dans les scans de phalange.

## Impact

L'inventaire de la cible est lu à l'impact. Un missile d'interception neutralise
exactement un missile attaquant et les deux sont consommés, quelles que soient
les technologies. Pour une lune, les intercepteurs sont prélevés sur sa planète
parente ; les défenses de cette planète restent intactes.

```text
interceptés = min(missiles_envoyés, intercepteurs_disponibles)
arme_unitaire = round_half_up(12 000 × (1 + armes_attaquant / 10))
dégâts = (missiles_envoyés − interceptés) × arme_unitaire
coque_unitaire = round_half_up(((métal_base + cristal_base) / 10)
                              × (1 + protection_défenseur / 10))
```

Les technologies sont celles présentes à l'impact. Les boucliers et leur
technologie sont ignorés, y compris pour les petits et grands dômes. La défense
prioritaire reçoit les dégâts en premier ; le reliquat parcourt les autres
types dans un ordre aléatoire reproductible, déterminé par la seed de la salve.
Un type trop résistant pour le reliquat est conservé et les types suivants sont
essayés. Les unités partiellement endommagées ne conservent pas ces dommages
après la salve. Les missiles interplanétaires stockés sont attaqués uniquement
après destruction de toutes les autres défenses ; ils ne peuvent pas être
choisis comme priorité.

Les pertes ne sont jamais reconstruites et ne génèrent ni débris ni lune. Les
vaisseaux, bâtiments et ressources ne sont pas touchés. À technologies égales,
un missile non intercepté détruit 60 lanceurs de missiles ou 8 missiles
interplanétaires stockés lorsque les autres défenses ont disparu.

## Persistance et rapports

Le débit du stock, la salve et son événement d'impact sont enregistrés dans une
même transaction. Une clé d'idempotence ne peut représenter qu'un seul tir et
une même requête rejouée ne dépense rien de plus. Les salves sont indépendantes
de la table des flottes : les points des missiles tirés disparaissent dès leur
départ et aucune mission de retour ne peut les recréditer.

L'événement `missile_impact` a la priorité 10, comme les combats : à échéance et
priorité égales, l'identifiant de l'événement départage les actions. Deux salves
successives voient donc les stocks et défenses laissés par la précédente.
Le règlement du chantier livre les unités achevées à l'instant de l'impact,
comme lors d'une arrivée de flotte. Un échec technique annule l'ensemble de
l'impact ; une reprise ne double ni les pertes ni les rapports.

L’identité de la cible inclut sa date de création, afin qu’une nouvelle colonie
réutilisant son identifiant ne reçoive pas la salve. Une cible disparue,
déplacée ou devenue propre provoque l'abandon du tir sans
remboursement. Un rapport d'abandon est adressé à l'attaquant. Sinon, les deux
joueurs reçoivent un rapport indiquant l'origine, la cible, les missiles envoyés,
les interceptions et les pertes. Les défenses restantes sont communiquées
uniquement si un missile a franchi l'interception ; aucun inventaire de flotte,
stock de ressources, technologie ou choix prioritaire n'est révélé. Le rapport
du défenseur est une notification hostile.

Les salves sortantes et entrantes sont visibles sur la page de missiles et dans
le suivi des flottes ; les salves entrantes alimentent aussi les alertes des
corps. Les IA bénéficient automatiquement de leurs intercepteurs. La décision
de lancer leurs propres salves reste une évolution séparée.

## Références

- [Guide OGame des missiles](https://board.en.ogame.gameforge.com/index.php?thread/810633-guide-09-ipm-guide/) : parcours depuis la galaxie, portée, priorité, interception, lunes et rapports.
- [Guide OGame Origin](https://board.origin.ogame.gameforge.com/index.php/Thread/794-Guide-11-IPM-guide/) : formule de trajet à 60 secondes par système, absence de débris et de slots, missiles stockés.
- [Annonce OGame 5.8.5](https://board.en.ogame.gameforge.com/index.php?pageNo=9&thread%2F718944-old-updates%2F=) : portée circulaire des missiles lorsque les systèmes circulaires sont activés.

Les guides historiques divergent sur le temps de vol et l'ordre secondaire.
Cette implémentation retient la formule `30 + 60 × distance` du guide Origin,
adaptée au coefficient de vitesse hostile du jeu, et l'ordre secondaire
aléatoire du guide anglais révisé.
