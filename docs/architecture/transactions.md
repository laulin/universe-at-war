# Modèle transactionnel

SQLite est l'autorité persistante. Une commande métier correspond à une courte
transaction atomique.

## Connexions

- un handle de lecture autorise plusieurs connexions en WAL ;
- un handle d'écriture est limité à une connexion active ;
- `foreign_keys=ON`, `journal_mode=WAL`, `busy_timeout` et le niveau de
  synchronisation sont appliqués et vérifiés sur les connexions ;
- les timestamps persistés sont des instants UTC avec une représentation
  canonique.

La sérialisation du chemin d'écriture réduit les conflits mais ne remplace pas
les contraintes `UNIQUE`, `CHECK`, les clés étrangères et les gardes de version.

## Frontière applicative

Le port transactionnel reçoit une fonction et fournit les repositories liés à
la transaction. Le cas d'usage :

1. relit les agrégats concernés ;
2. règle les ressources à l'heure fournie par `Clock` ;
3. vérifie permissions, versions et préconditions ;
4. appelle le domaine pur ;
5. persiste toutes les mutations ;
6. écrit événements, rapports et audit associés ;
7. commit ou rollback de l'ensemble.

Les erreurs de conflit sont distinctes des erreurs de validation et des erreurs
temporaires `busy`.

## Concurrence et idempotence

- les agrégats mutables portent une version incrémentée par mise à jour ;
- une mise à jour utilise `WHERE id = ? AND version = ?` et doit affecter une
  ligne exactement ;
- les commandes sensibles portent une clé d'idempotence unique par acteur et
  type d'opération ;
- les événements utilisent une clé d'idempotence et un état terminal ;
- une contrainte d'unicité arbitre les colonisations concurrentes ;
- les prélèvements de débris et de ressources vérifient le stock dans la même
  instruction ou transaction.

L'ordre observable des écritures concurrentes est l'ordre de commit. Pour les
événements de même échéance, l'ordre défini dans `scheduled-events.md` prévaut.

