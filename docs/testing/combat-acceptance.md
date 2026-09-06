# Acceptation — espionnage, combat et débris

## Scénarios couverts

1. Scénario E complet : espionnage, attaque, pertes, butin, débris, recyclage et
   retour de toutes les flottes, sans qu'aucun inventaire ne devienne négatif.
2. Un rapport d'espionnage ne contient que les sections que son niveau autorise ;
   la cible reçoit toujours une notification, et cette notification ne contient
   jamais ce qui a été vu.
3. Un combat est reproductible : rejouer les compositions du rapport avec la
   seed persistée redonne la même issue.
4. Une redélivrance d'un événement de combat ne crée ni second rapport, ni
   second champ de débris, ni second butin.
5. Deux recycleurs visant le même champ n'en retirent jamais plus qu'il ne
   contient, et un champ vidé disparaît.
6. Espionner ou attaquer sa propre planète est refusé, comme recycler une
   position sans débris ; aucun de ces refus ne laisse de flotte derrière lui.
7. La carte galaxie ne montre que les noms de planètes, les joueurs et les
   champs de débris.
8. Un rapport appartenant à un autre joueur est introuvable, jamais interdit.
9. Le marquage « lu » est idempotent et protégé contre la falsification de
   requête.
10. Les rapports hostiles non lus lèvent une alerte dans la navigation, avec un
    texte et un compteur, jamais par la seule couleur.
11. Audit de fuite : quatorze routes joueur sont balayées avec des valeurs
    sentinelles plantées chez le voisin ; aucune n'apparaît dans une réponse.

## Contrôles

- `go test ./...` et `go test -race ./...` ;
- combats témoins enregistrés dans `internal/domain/combat/testdata` : toute
  divergence signale un changement de règle ;
- fuzz des invariants de combat : quantités positives, survivants bornés,
  conservation des effectifs, débris positifs, chance de lune bornée ;
- `BenchmarkResolveLargeBattle` : cent mille unités par camp en environ
  120 millisecondes et une centaine d'allocations ;
- `BenchmarkReportsPage` : une page de rapports parmi cinq mille en moins d'une
  milliseconde.
