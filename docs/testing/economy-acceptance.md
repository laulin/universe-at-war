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
- une clé par carte rendue : deux corps d'un même compte commandant le même
  bâtiment au même niveau cible mettent chacun le leur en file ;
- deux constructions concurrentes tentant de dépenser le même stock ;
- mise en file de plusieurs constructions, chacune débitée à la commande ;
- démarrage sans temps mort de l'ordre suivant, à la vitesse des usines du
  moment ;
- refus d'un ordre au-delà du plafond de file, sans aucune dépense ;
- réservation d'une case par ordre encore en file ;
- annulation avec remboursement intégral, cascade sur les niveaux empilés et sur
  les ordres dont le prérequis disparaît, écrêtage à la capacité des entrepôts,
  et libération de la clé d'idempotence ;
- achèvement exactement une fois et redélivrance simulée après crash ;
- ordre par identifiant de deux événements au même instant ;
- parcours HTTP création d'empire → vue planète → lancement d'une mine ;
- panneau de file HTTP : barre de progression, compte à rebours silencieux pour
  les lecteurs d'écran, ordres en attente et bouton d'annulation, sans un seul
  style en ligne ;
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
