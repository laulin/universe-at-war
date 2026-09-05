# Universe At War

Universe At War est un jeu de stratégie spatiale persistant, local et autonome,
inspiré des mécaniques temporelles d'OGame. Le serveur est développé en Go et
utilise SQLite sans service externe.

Le socle et le premier milestone applicatif fournissent les migrations SQLite
embarquées, le bootstrap administrateur, l'authentification, les sessions
opaques et l'assistant de configuration versionné. La spécification complète se
trouve dans
[`SPECIFICATION_OGAME_LOCAL_GO.md`](SPECIFICATION_OGAME_LOCAL_GO.md).

## Prérequis de développement

- Go 1.27.x ou plus récent ;
- aucune installation SQLite ou CGO.

## Commandes disponibles

```sh
go run ./cmd/universe-at-war serve
go run ./cmd/universe-at-war migrate --database universe-at-war.db
go run ./cmd/universe-at-war doctor --database universe-at-war.db
go run ./cmd/universe-at-war admin reset-password --database universe-at-war.db --username admin
go run ./cmd/universe-at-war version
```

Au premier `serve`, le terminal affiche une seule fois l'identifiant et le mot
de passe aléatoire de l'administrateur initial. Après connexion sur
`http://127.0.0.1:8080`, ce mot de passe doit être remplacé avant de parcourir
les dix étapes de création de l'univers.

La commande `migrate` applique les migrations embarquées. `doctor` vérifie la
version du schéma, l'intégrité SQLite et les clés étrangères sans modifier les
données. `admin reset-password` constitue la récupération locale : elle génère
un nouveau secret, invalide toutes les sessions du compte et exige un nouveau
changement de mot de passe.

## Qualité

```sh
make test
make test-race
make vet
make build
```

La CI ajoute `staticcheck`, `govulncheck` et les compilations Linux, Windows et
macOS. Les fonctionnalités métier sont développées en TDD, par tranches
transactionnelles.
