# Universe At War

Universe At War est un jeu de stratégie spatiale persistant, local et autonome,
inspiré des mécaniques temporelles d'OGame. Le serveur est développé en Go et
utilise SQLite sans service externe.

Le projet est au début de son développement. Le socle actuel fournit les
migrations SQLite embarquées, les contrôles d'intégrité, une horloge testable et
la machine d'état du serveur. La spécification complète se trouve dans
[`SPECIFICATION_OGAME_LOCAL_GO.md`](SPECIFICATION_OGAME_LOCAL_GO.md).

## Prérequis de développement

- Go 1.27.x ou plus récent ;
- aucune installation SQLite ou CGO.

## Commandes disponibles

```sh
go run ./cmd/universe-at-war migrate --database universe-at-war.db
go run ./cmd/universe-at-war doctor --database universe-at-war.db
go run ./cmd/universe-at-war version
```

La commande `migrate` applique les migrations embarquées. `doctor` vérifie la
version du schéma, l'intégrité SQLite et les clés étrangères sans modifier les
données.

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

