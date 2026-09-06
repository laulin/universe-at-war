# Stratégie de migrations

- migrations SQL monotones, embarquées dans le binaire et nommées par version ;
- table de metadata créée par le runner, distincte des migrations métier ;
- checksum enregistré pour détecter la modification d'une migration publiée ;
- application automatique au démarrage, sous verrou d'écriture ;
- une migration est transactionnelle sauf impossibilité SQLite documentée ;
- aucun downgrade automatique ;
- refus explicite d'une base provenant d'une version future ;
- sauvegarde cohérente proposée avant toute migration classée majeure.

Les tests ouvrent de vraies bases temporaires avec le même driver et les mêmes
PRAGMA. Ils couvrent une base vide, chaque version encore supportée, une
migration déjà appliquée, un checksum invalide et une version future.


## Versions de document de règles

Les documents de `ruleset_versions` sont immuables. Ils portent un
`schema_version` : un document plus ancien est décodé par-dessus les valeurs par
défaut courantes, si bien qu'une section ajoutée par une version ultérieure
n'invalide aucune partie existante. Un document dont le `schema_version` dépasse
celui de l'application est refusé, comme un schéma SQLite venu du futur. Les
champs inconnus restent interdits : ils ne peuvent venir que d'une application
plus récente. Le contenu du jeu (bâtiments, recherches, unités) est identifié
par `progression.catalogue_version`.

## Réécriture d'une table référencée

Une contrainte de table ne se supprime pas en place dans SQLite : il faut
recréer la table, y recopier les lignes, supprimer l'ancienne et renommer la
nouvelle. Or supprimer une table référencée avec les clés étrangères actives
efface en cascade les lignes filles.

Une migration qui commence par `-- migration: rebuild referenced table` est donc
exécutée sur une connexion épinglée, clés étrangères désactivées le temps de la
réécriture. Elle reste entièrement dans une transaction, et `foreign_key_check`
est exécuté avant le commit : une réécriture qui laisserait une ligne orpheline
est refusée. Les clés sont réactivées ensuite.
