# Sauvegardes, restauration et mise à niveau

## Prendre une sauvegarde

```sh
universe-at-war backup --database universe-at-war.db --keep 14
```

Sans `--directory`, la sauvegarde est écrite dans un dossier `backups/` à côté
de la base. Le nom porte l'instant UTC de la prise :
`universe-at-war-20420910T120000Z.db`.

La copie est faite par `VACUUM INTO`, la facilité de SQLite elle-même : elle
s'exécute sous une transaction de lecture, donc **le serveur peut continuer à
écrire pendant la sauvegarde**. La copie est ensuite rouverte et vérifiée :
contrôle d'intégrité, version de schéma lisible par cette version du jeu. Une
copie qui échoue à l'un de ces contrôles fait échouer la commande.

`--keep N` ne conserve que les N plus récentes et supprime les autres. La
rétention ne touche que les fichiers écrits par cette commande : tout ce qui ne
s'appelle pas `universe-at-war-*.db` est laissé où il est.

Un administrateur peut aussi déclencher une sauvegarde depuis la page
d'administration. Chaque prise est inscrite dans la table `backups`, et la
dernière apparaît sur le tableau de bord.

## Restaurer

1. Arrêter le serveur.
2. Mettre la base courante de côté :
   `mv universe-at-war.db universe-at-war.db.avant-restauration`
   (déplacer aussi les fichiers `-wal` et `-shm` s'ils existent).
3. Copier la sauvegarde à sa place : `cp backups/universe-at-war-….db universe-at-war.db`
4. Vérifier avant de démarrer : `universe-at-war doctor --database universe-at-war.db`
5. Redémarrer : `universe-at-war serve`

Les événements planifiés reprennent d'eux-mêmes : le worker relit la file au
démarrage et rattrape tout ce qui était dû pendant l'arrêt.

## Mettre à niveau

1. Prendre une sauvegarde **avant** de remplacer le binaire.
2. Remplacer le binaire.
3. `universe-at-war migrate --database universe-at-war.db` applique les
   migrations manquantes. Elles sont vérifiées par empreinte : une migration
   déjà appliquée qui aurait changé fait échouer la commande plutôt que
   d'abîmer la base.
4. `universe-at-war doctor` confirme le schéma, l'intégrité, le mode WAL,
   l'accès en écriture et la lisibilité du ruleset actif.

Une base ou une sauvegarde dont le schéma est **plus récent** que le binaire est
refusée avec un message clair. C'est voulu : redescendre de version se fait en
restaurant une sauvegarde prise avant la montée, jamais en forçant.

## Ce que `doctor` vérifie

| Contrôle | Ce qu'il attend |
| --- | --- |
| version de schéma | connue de ce binaire, jamais plus récente |
| intégrité | `PRAGMA integrity_check` sans erreur |
| mode journal | `wal` |
| écriture | une écriture réelle, annulée aussitôt |
| clés étrangères | `PRAGMA foreign_key_check` vide |
| ruleset | le ruleset actif se décode et se valide |
