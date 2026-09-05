# Universe At War

Universe At War est un jeu de stratégie spatiale persistant, local et autonome,
inspiré des mécaniques temporelles d'OGame. Le serveur est développé en Go et
utilise SQLite sans service externe.

Le socle et les deux premiers milestones fournissent les migrations SQLite
embarquées, le bootstrap administrateur, l'authentification, les sessions
opaques, l'assistant de configuration versionné et une progression économique
jouable. Après le démarrage de l'univers, un compte peut fonder son empire,
produire des ressources hors ligne et construire les premiers bâtiments depuis
l'interface SSR. La spécification complète se trouve dans
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

Si la politique d'inscription de l'univers vaut `open`, la page de connexion
propose de créer un compte joueur ; sinon le parcours reste totalement fermé.

Une fois l'univers lancé, la page principale liste les corps de l'empire et
chaque planète a sa propre page. La page principale propose de fonder un empire. La
planète mère affiche les productions, stockages, énergie et cases, ainsi que le
catalogue de bâtiments. Les constructions et leurs échéances sont durables : le
worker reprend automatiquement les événements après un redémarrage.

Le serveur écrit des journaux structurés sur la sortie d'erreur. Chaque requête
porte un identifiant de corrélation renvoyé dans l'en-tête `X-Request-Id` ;
aucun secret, jeton ni cookie n'y figure. `UAW_LOG_LEVEL` choisit le niveau
(`debug`, `info`, `warn`, `error`).

La commande `migrate` applique les migrations embarquées. `doctor` vérifie la
version du schéma, l'intégrité SQLite et les clés étrangères sans modifier les
données. `admin reset-password` constitue la récupération locale : elle génère
un nouveau secret, invalide toutes les sessions du compte et exige un nouveau
changement de mot de passe.

## Plans d'implémentation

Les jalons restants sont découpés en tâches exécutables dans
[`docs/plans/`](docs/plans/README.md) : feuille de route, socle transverse et
plans détaillés des recherches, du moteur de flotte et du combat.

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
