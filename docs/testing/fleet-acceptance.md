# Acceptation — moteur de flotte

## Scénarios couverts

1. Scénario D complet : composition, calcul du carburant, départ, retrait des
   vaisseaux et des ressources, arrivée, livraison, retour, inventaire cohérent.
2. Une mission refusée ne laisse aucune trace : ni flotte, ni vaisseau manquant,
   ni deutérium débité, que le refus vienne du carburant, de l'inventaire, du
   cargo ou de la destination.
3. Une livraison de transport est plafonnée par le stockage de la cible ; le
   surplus repart avec la flotte et revient à l'origine.
4. Un stationnement déplace définitivement vaisseaux et cargo, sans retour.
5. Scénario J : une flotte déjà partie conserve ses horaires après changement de
   ruleset ; une nouvelle flotte utilise les nouvelles règles.
6. Un rappel avant l'arrivée annule l'événement d'arrivée, renvoie la flotte
   pour le temps déjà parcouru et restitue tout à l'origine.
7. Un rappel après l'arrivée est refusé sans rien changer.
8. Un rappel concurrent d'une arrivée ne laisse qu'une seule transition sortante
   et un seul événement de retour, quel que soit le gagnant de la course.
9. Le rappel d'une flotte d'un autre compte est introuvable, jamais interdit.
10. Six aller-retours consécutifs conservent exactement le nombre de vaisseaux,
    répartis entre les planètes et les flottes, et le deutérium dépensé
    correspond au carburant des missions.
11. Le parcours web complet : page flotte, assistant, aperçu chiffré,
    confirmation, lancement, rappel, avec CSRF et clé d'idempotence.

## Contrôles

- `go test ./...` et `go test -race ./...`, dont la course rappel/arrivée jouée
  plusieurs fois ;
- fuzz de la distance : symétrie, positivité, refus des coordonnées invalides ;
- `BenchmarkFleetLaunch` : un lancement complet, transaction comprise, coûte
  environ 3 millisecondes.
