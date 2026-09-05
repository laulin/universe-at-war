# Acceptation du Milestone 2

Les scénarios automatisés utilisent le driver SQLite de production, toutes les
migrations et une horloge factice. Ils couvrent :

- parsing, bornes, représentation canonique et fuzzing des coordonnées ;
- coûts, durées, prérequis et cases des bâtiments ;
- deux heures de production hors ligne et conservation des fractions ;
- plafonds de stockage et facteur énergétique ;
- unicité de position et contraintes de soldes non négatifs en base ;
- création d'empire atomique ;
- double soumission avec la même clé d'idempotence ;
- deux constructions concurrentes tentant de dépenser le même stock ;
- achèvement exactement une fois et redélivrance simulée après crash ;
- ordre par identifiant de deux événements au même instant ;
- parcours HTTP création d'empire → vue planète → lancement d'une mine ;
- refus HTTP sans jeton CSRF ;
- wake-up et arrêt propre du worker ;
- benchmark `BenchmarkSettleLazy` sans attente réelle.

Commandes de validation :

```sh
go test ./...
go test -race ./...
go vet ./...
staticcheck ./...
go test -bench BenchmarkSettleLazy -benchmem ./internal/domain/economy
```
