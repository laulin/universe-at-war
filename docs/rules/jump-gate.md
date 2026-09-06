# Porte de saut — règle de fidélité

## Comportement attendu

Une porte de saut relie deux lunes du même joueur. Elle déplace des vaisseaux
d'une lune à l'autre instantanément, sans trajet et sans carburant : c'est une
exception assumée au moteur de flotte, et c'est pour cela qu'elle est bornée par
un temps de recharge.

Seuls des vaisseaux passent. Aucune ressource n'est transférée : un saut ne
remplace pas un transport.

## Conditions

Les deux corps doivent être des lunes appartenant au joueur, chacune équipée
d'une porte de saut, et distinctes l'une de l'autre. Les deux portes doivent être
rechargées.

## Temps de recharge

```text
prêt_à = instant_du_saut + temps_de_recharge
```

Le temps de recharge est le même pour les deux portes, celle de départ comme
celle d'arrivée : un aller-retour immédiat est impossible. Il est persisté, donc
il survit à un redémarrage du serveur.

## Cas limites et invariants

Un saut ne crée ni ne détruit aucun vaisseau : ce que la lune de départ perd, la
lune d'arrivée le gagne, dans la même transaction. Un saut vers une lune qui
n'est pas la sienne, vers une planète, vers elle-même, ou pendant la recharge est
refusé sans rien déplacer. Deux sauts concurrents depuis la même porte n'en
laissent réussir qu'un.

## Tests de référence

Saut nominal et conservation des vaisseaux ; refus pendant la recharge des deux
côtés ; refus vers une lune étrangère, une planète ou la lune de départ ; deux
sauts concurrents ; persistance de la recharge après relecture.
