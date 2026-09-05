# Architecture cible

Universe At War est un monolithe modulaire. Le domaine ne dépend ni de HTTP, ni
de SQLite, ni de l'horloge système. Les dépendances vont des couches externes
vers le domaine.

## Arborescence

```text
cmd/universe-at-war/       point d'entrée et sous-commandes
internal/domain/           règles métier pures, types et invariants
internal/app/              cas d'usage et frontières transactionnelles
internal/sim/              ordonnanceur et traitement des événements
internal/ai/               décideurs utilisant les mêmes cas d'usage que le web
internal/storage/sqlite/   migrations, transactions et repositories
internal/auth/             mots de passe, sessions et secrets
internal/web/              HTTP, formulaires et rendu serveur
internal/admin/            orchestration administrative
internal/moderation/       sanctions et permissions de modération
internal/observability/    logs, métriques et identifiants de corrélation
migrations/                schéma SQLite embarqué
rules/                     catalogues et profils de règles embarqués
web/templates/             templates HTML embarqués
web/static/                CSS, JavaScript et images embarqués
docs/                      architecture, ADR et règles documentées
tests/                     scénarios d'acceptation inter-modules
```

Les ressources à embarquer seront exposées par un paquet racine dédié afin de
ne pas faire dépendre le domaine de chemins de fichiers.

## Modules bornés

| Module | Responsabilité | Invariants principaux |
| --- | --- | --- |
| Account | identité, rôles et état d'accès | compte distinct du joueur, rôles explicites |
| Moderation | bans et historique | justification obligatoire, historique immuable |
| Universe | topologie et état global | coordonnées uniques, transitions serveur valides |
| Rules | versions et catalogue | version immuable, validation unique, non-rétroactivité |
| Player | empire et propriété | aucune autorité administrative implicite |
| Planet/Moon | corps célestes | une position ne possède qu'une planète |
| Economy | stocks, production et énergie | aucune ressource négative, calcul UTC déterministe |
| Building | niveaux et file | coût atomique, prérequis vérifiés |
| Research | graphe et progression | graphe acyclique, une progression valide |
| Shipyard | vaisseaux et défenses | quantités entières positives, coût atomique |
| Fleet/Mission | composition et voyage | conservation des unités, transitions validées |
| Espionage | connaissance partielle | aucune vérité cachée envoyée au joueur ou à l'IA |
| Combat | résolution pure | même entrée et même seed donnent le même résultat |
| Debris | création et recyclage | prélèvement atomique, stock jamais négatif |
| Colonization | acquisition de position | attribution unique et transactionnelle |
| Alliance/ACS | équipe et opérations | droits explicites, résolution unique |
| Expedition | PvE probabiliste | seed enregistrée et résultats configurables |
| Reports | récit destiné à un acteur | rapport immuable et filtré à la création |
| AI | décision stratégique | mêmes commandes, coûts et informations qu'un humain |

## Règles de dépendance

- `domain` ne dépend que de la bibliothèque standard.
- `app` dépend du domaine et déclare uniquement les ports réellement utiles.
- `storage/sqlite`, `auth`, `sim` et `web` adaptent ces ports.
- les événements planifiés sont répartis par type : `storage/sqlite` sélectionne
  la prochaine échéance tous types confondus et délègue au gestionnaire
  enregistré, dans la transaction qui l'a sélectionnée.
- `web` ne démarre aucune transaction et ne contient aucune formule métier.
- `ai` appelle les cas d'usage de `app`; il n'accède jamais aux repositories
  adverses.
- une transaction ne contient ni rendu de template, ni attente, ni appel réseau.

## Flux d'une mutation

1. L'adaptateur HTTP ou IA valide la forme de la commande.
2. Le cas d'usage vérifie identité, permission et état serveur.
3. Le gestionnaire transactionnel ouvre l'unique transaction d'écriture.
4. Les snapshots nécessaires sont relus et réglés à l'heure de la commande.
5. Le domaine calcule la transition.
6. État, événement planifié, journal métier et audit sont persistés ensemble.
7. Le commit précède toute réponse ou notification externe.


## Observabilité

Le serveur construit un journal structuré `slog` sur la sortie d'erreur. Un
identifiant de corrélation est attribué à chaque requête, renvoyé dans
`X-Request-Id` et repris dans la ligne d'accès (méthode, chemin, statut, durée).
La chaîne de requête, les cookies et les jetons ne sont jamais journalisés. Le
worker journalise les lots en échec et poursuit : une erreur transitoire de base
ne doit pas arrêter la simulation.
