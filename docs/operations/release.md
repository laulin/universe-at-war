# Mettre Universe At War en service

Un seul fichier, aucun service externe, aucune dépendance à installer. Le
binaire embarque les migrations, les templates, le CSS et le JavaScript.

## Construire

```sh
make build VERSION=0.10.0          # un binaire pour la machine courante
make release VERSION=0.10.0        # la matrice complète dans bin/
```

| Cible | Fichier produit |
| --- | --- |
| linux/amd64 | `universe-at-war-0.10.0-linux-amd64` |
| linux/arm64 | `universe-at-war-0.10.0-linux-arm64` |
| windows/amd64 | `universe-at-war-0.10.0-windows-amd64.exe` |
| darwin/amd64 | `universe-at-war-0.10.0-darwin-amd64` |
| darwin/arm64 | `universe-at-war-0.10.0-darwin-arm64` |

Tous sont construits avec `CGO_ENABLED=0` : le pilote SQLite est en Go pur, il
n'y a rien à lier. `universe-at-war version` dit quelle version tourne.

## Démarrer depuis une base vide

```sh
./universe-at-war serve
```

1. Le premier démarrage crée la base, applique les migrations et affiche **une
   seule fois** l'identifiant et le mot de passe de l'administrateur initial.
   Notez-les : ils ne seront jamais réaffichés.
2. Ouvrez `http://127.0.0.1:8080` et connectez-vous. Le mot de passe doit être
   changé avant d'aller plus loin.
3. Configurez l'univers : dix étapes, ou un profil chargé depuis
   `/setup/profiles` (proche classique, lent, accéléré, conflit, coopératif,
   PvPvE), avec l'aperçu des écarts avant de démarrer.
4. Démarrez l'univers à la dernière étape. Le ruleset devient une version
   immuable ; les événements déjà lancés gardent la leur.
5. Fondez votre empire depuis la page d'accueil, ou laissez les joueurs
   s'inscrire selon la politique choisie.

### Politiques d'inscription

| Politique | Ce qui se passe |
| --- | --- |
| `closed` | `/register` n'existe pas |
| `open` | n'importe qui peut créer un compte |
| `invitation` | il faut un code créé depuis `/admin`, à usage unique |

Un refus d'inscription dit toujours la même chose, quelle qu'en soit la raison :
la page ne révèle ni les identifiants pris ni les codes valides. Le formulaire
pardonne quelques fautes de frappe puis ralentit.

## Options de service

```sh
./universe-at-war serve --database universe-at-war.db --listen 127.0.0.1:8080 --secure-cookie
```

- `--listen` : écoutez sur une boucle locale sauf si vous placez un reverse
  proxy TLS devant. Sur une adresse publique sans `--secure-cookie`, le serveur
  vous avertit.
- `UAW_LOG_LEVEL` : `debug`, `info`, `warn` ou `error`.
- `UAW_DATABASE_PATH`, `UAW_LISTEN_ADDRESS` : les mêmes réglages par
  l'environnement.

## Exploiter

| Besoin | Commande ou page |
| --- | --- |
| état, arriéré, débit, population | `/admin` |
| compteurs bruts du processus | `/admin/metrics` |
| comptes, rôles, statuts, invitations | `/admin` |
| sanctions | `/admin/moderation` |
| joueurs artificiels | `/admin/ai` |
| sauvegarde | `universe-at-war backup --keep 14` ou le bouton de `/admin` |
| diagnostic | `universe-at-war doctor` |
| migrations | `universe-at-war migrate` |
| mot de passe administrateur perdu | `universe-at-war admin reset-password --username admin` |

La restauration et la mise à niveau sont décrites dans
[`backups.md`](backups.md) ; les budgets de performance dans
[`performance.md`](performance.md).

## Qualité d'une version

Avant de publier une version, tout ceci doit être vert :

```sh
make check          # tests, vet, gofmt
make test-race      # détecteur de course
make lint           # staticcheck
make audit          # govulncheck
make bench          # bancs de performance
make release        # les cinq cibles
go test -fuzz=FuzzResolveKeepsItsInvariants -fuzztime=30s -run=^$ ./internal/domain/combat/
```

Les scénarios A à J de la spécification sont couverts par la suite
d'acceptation ; la matrice de traçabilité vers les tests et les écrans est dans
[`docs/plans/2026-09-06-milestone-10-matrix.md`](../plans/2026-09-06-milestone-10-matrix.md).

## Ce que cette version ne fait pas

- **Pas de canal temps réel.** Les pages affichent des horodatages serveur
  autoritatifs et demandent un rechargement ciblé quand une échéance passe.
  Sans JavaScript tout reste correct. Un flux SSE reste possible plus tard.
- **Pas de peuplement automatique en IA au démarrage.** La section `ai` du
  ruleset porte les valeurs recommandées ; un administrateur crée les joueurs
  artificiels depuis `/admin/ai`.
- **Pas de TLS intégré.** Servez derrière un reverse proxy pour exposer la
  partie hors de la machine.
