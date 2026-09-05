# Schéma initial du bootstrap

Le premier lot de migrations crée des tables `STRICT` lorsque compatible.

## Tables

- `schema_metadata` : version de schéma et versions applicatives ;
- `server_state` : ligne unique, état courant, état précédent et version ;
- `accounts` : identifiant, nom normalisé unique, statut et dates ;
- `account_roles` : rôles `ADMIN`, `MODERATOR`, `PLAYER` avec contraintes ;
- `password_credentials` : encodage Argon2id et obligation de changement ;
- `sessions` : digest du token, échéance, révocation et rotation ;
- `bans` : cible, auteur, période, justification et levée ;
- `audit_log` : journal administratif append-only ;
- `ruleset_versions` : document canonique immuable, auteur et activation ;
- `scheduled_events` : enveloppes temporelles ordonnées et idempotentes ;
- `game_event_log` : journal métier append-only ;
- `idempotency_keys` : résultat stable des commandes HTTP sensibles.

Le bootstrap ne crée aucun empire pour l'administrateur.

## Création initiale

Sur une base sans metadata, une unique transaction :

1. applique toutes les migrations ;
2. insère l'état `BOOTSTRAP_PENDING` ;
3. génère au moins 24 octets par `crypto/rand` ;
4. dérive et stocke uniquement un encodage Argon2id ;
5. crée le compte avec le rôle `ADMIN` et `must_change_password=true` ;
6. inscrit l'événement d'audit sans le secret ;
7. commit.

Le secret n'est rendu au terminal qu'après commit et n'est jamais persisté ni
réaffiché. Un redémarrage détecte les données existantes et ne recrée rien.

