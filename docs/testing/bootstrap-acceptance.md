# Acceptation du bootstrap

Les premiers tests exécutables couvriront les scénarios suivants.

## Base absente

- le fichier est créé et toutes les migrations sont appliquées ;
- les PRAGMA requis sont actifs ;
- l'état est exactement `BOOTSTRAP_PENDING` ;
- un seul compte administrateur sans joueur est créé ;
- son secret possède au moins 128 bits d'entropie issue de `crypto/rand` ;
- seul le hash Argon2id est stocké ;
- le secret est retourné une fois au processus appelant.

## Redémarrage

- aucune nouvelle credential ni nouveau compte n'est créé ;
- aucun secret bootstrap n'est retourné ;
- les migrations restent idempotentes ;
- l'état et les données existantes sont inchangés.

## Accès avant setup

- login, logout, setup, assets et health minimal sont accessibles ;
- inscription, création d'empire et routes de jeu sont refusées ;
- un joueur et un modérateur ne peuvent pas valider le setup ;
- le mot de passe initial doit être remplacé avant validation finale.

## Reprise et erreur

- un échec avant commit ne laisse ni compte partiel ni état serveur ;
- un redémarrage reprend depuis un état cohérent ;
- une base d'une version future est refusée sans mutation ;
- le reset local invalide les sessions et ne journalise jamais le secret.

