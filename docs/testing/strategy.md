# Stratégie TDD

Chaque tranche commence par un test observable rouge, puis l'implémentation
minimale, le refactor et la validation globale.

## Niveaux

1. Domaine : majorité des tests, table-driven, sans DB ni HTTP.
2. Application : cas d'usage avec doubles minimaux et fake clock.
3. SQLite : vraie base temporaire, migrations et PRAGMA de production.
4. HTTP : `httptest` pour auth, permissions, CSRF, formulaires et PRG.
5. Navigateur : quelques parcours critiques seulement.

## Contrôles continus

- `go test ./...` à chaque tranche ;
- `go test -race ./...`, `go vet ./...`, `staticcheck ./...` et
  `govulncheck ./...` en CI ;
- `gofmt` et vérification du diff ;
- fuzzing des parseurs et moteurs probabilistes ;
- tests de concurrence avec barrières contrôlées, jamais avec de longs sleeps ;
- benchmarks des chemins identifiés dans la spécification.

Une fonctionnalité n'est terminée que si sa transaction, ses permissions, ses
erreurs, son audit éventuel et l'absence de fuite d'information sont testés.

