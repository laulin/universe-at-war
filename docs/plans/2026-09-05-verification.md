# Vérification de bout en bout

À chaque tâche : `make check` (tests, vet, gofmt). À chaque fin de partie :

```sh
go test ./... && go test -race ./... && go vet ./... && staticcheck ./... && govulncheck ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/universe-at-war && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/universe-at-war
go test -fuzz=FuzzCombatInvariants -fuzztime=30s ./internal/domain/combat/   # M5
go test -bench=. -run=^$ ./internal/domain/... ./tests/                        # budgets documentés dans docs/rules
```

Vérification manuelle (spec §43.1 niveau 5, réalisée à la fin de M3, M4 et M5) : `rm -f universe-at-war.db && go run ./cmd/universe-at-war serve`, connexion admin, mot de passe, wizard 10 étapes (politique d'inscription `open`), inscription d'un second compte via `/register`, empire pour chacun, puis :
- M3 : construire labo et chantier, lancer une recherche, commander des chasseurs et des lance-missiles, observer la livraison progressive et les échéances après redémarrage du serveur.
- M4 : envoyer un transport vers l'autre joueur, vérifier arrivée/retour et le rappel ; redémarrer le serveur en vol et constater la reprise.
- M5 : espionner, attaquer, lire les rapports des deux comptes, recycler les débris depuis la galaxie ; vérifier dans le HTML du défenseur l'absence de toute donnée non révélée.

Scénarios de la spécification prouvés à l'issue de la Partie 3 : A, B, C (existants), D, E, F, J. Restent G, H, I pour M7, M8, M10.
