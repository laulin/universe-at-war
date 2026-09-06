# Espionnage — règle de fidélité

## Comportement attendu

Une mission d'espionnage n'emporte que des sondes et vise une planète d'un autre
joueur. À l'arrivée, l'attaquant reçoit un rapport ; la cible reçoit toujours une
notification qui ne contient jamais ce qui a été révélé.

Le serveur connaît la vérité ; le joueur ne connaît que ce qu'il a découvert. Une
section non révélée est absente du rapport : elle n'est ni envoyée, ni masquée à
l'affichage.

## Formules

```text
écart  = espionnage_attaquant - espionnage_défenseur
niveau = écart + racine_entière(sondes)
```

Une section est révélée lorsque le niveau atteint son seuil :

| Section | Seuil par défaut |
| --- | ---: |
| ressources | 1 |
| flotte | 2 |
| défenses | 3 |
| bâtiments | 4 |
| recherches | 5 |

Les seuils sont croissants et configurables. Une section révélée est complète et
exacte à l'instant de l'arrivée : aucun bruit n'est ajouté.

La détection est tirée une fois avec la seed de la mission :

```text
flotte_cible = nombre de vaisseaux stationnés sur la cible (défenses exclues)
p = borne01(base_detection * sondes * flotte_cible
            * 2^(espionnage_défenseur - espionnage_attaquant))
```

La base vaut 0,0025. Une cible sans vaisseau ne détecte jamais, et aucun tirage
n'est consommé lorsque la probabilité est nulle.

Si la mission est détectée, toutes les sondes sont perdues : la flotte est
détruite, un champ de débris reçoit la part habituelle du coût des sondes
détruites, et le rapport de l'attaquant signale la perte. Sinon les sondes
rentrent normalement.

## Fraîcheur

Un rapport est `récent` tant que son âge ne dépasse pas la durée configurée
(3 600 secondes par défaut), `ancien` au-delà. Une section absente est
`inconnue`. La fraîcheur est une règle d'affichage : elle ne modifie jamais le
contenu du rapport.

## Exemples de référence

À technologies égales, une sonde donne un niveau de 1 : seules les ressources
sont révélées. Quatre sondes donnent 2 et ajoutent la flotte ; neuf sondes
donnent 3 et ajoutent les défenses. Avec trois niveaux d'espionnage de retard et
une sonde, le niveau vaut −2 et le rapport ne contient que le nom du joueur, celui
de la planète et ses coordonnées.

Une sonde contre une cible d'un vaisseau, à technologies égales, est détectée
avec une probabilité de 0,0025. Deux niveaux d'espionnage de retard pour
l'attaquant multiplient cette probabilité par quatre.

## Cas limites et invariants

Une composition qui n'est pas uniquement faite de sondes est refusée. Une cible
qui n'appartient à personne, ou au joueur lui-même, est refusée. Un rapport est
immuable : il n'est jamais mis à jour après coup. Le rapport de la cible ne
contient jamais les sections révélées.

## Tests de référence

Table des niveaux pour des écarts de −3 à +5 et 1, 4, 9, 16, 25 sondes ; table
des probabilités de détection ; absence d'alias entre la vérité et le rapport ;
absence de clé JSON pour une section non révélée ; scénario complet où la cible
reçoit sa notification et perd, ou non, les sondes.
