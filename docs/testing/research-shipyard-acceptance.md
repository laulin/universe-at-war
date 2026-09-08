# Acceptation — recherches, chantier et défenses

## Scénarios couverts

1. Une recherche démarre, débite son coût, se termine une seule fois et son
   niveau est acquis par le joueur, pas par la planète.
2. Une seconde recherche rejoint la file derrière la première, paie son coût à la
   commande, et prend la tête dès que celle-ci s'achève. Un ordre au-delà du
   plafond de file est refusé sans rien dépenser.
3. Le réseau de recherche intergalactique ajoute les meilleurs laboratoires
   distants éligibles et réduit la durée en conséquence.
4. Le graviton exige de l'énergie disponible, ne débite aucune ressource et dure
   une seconde.
5. Deux démarrages concurrents sur le même stock n'en laissent réussir qu'un, et
   aucune ressource ne devient négative.
6. Le laboratoire ne s'améliore pas tant qu'une recherche est active ou en
   attente, et réciproquement une recherche est refusée tant que le laboratoire
   est quelque part dans la file de construction ; les autres constructions
   restent possibles.
7. Une commande de production débite le coût total, livre les unités au fur et à
   mesure et se clôt exactement une fois, même après redélivrance de
   l'événement.
8. Les quantités nulles, négatives, au-delà du plafond ou dont le coût déborde
   sont refusées sans aucune mutation.
9. Les boucliers planétaires restent uniques et les missiles respectent la
   capacité du silo, tout ce que les files doivent encore compris.
10. Les vaisseaux et les défenses avancent dans deux files indépendantes ; un
    lot annulé rembourse les unités que le chantier devait encore, celles déjà
    livrées restant acquises. Recommander un lot identique après le premier est
    une nouvelle commande, jamais un rejeu.
11. Une recherche annulée rembourse la planète qui l'a lancée, quelle que soit la
    page depuis laquelle le joueur annule, et emporte les recherches dont elle
    fournissait le prérequis.
12. Les satellites solaires livrés augmentent l'énergie de la planète.
13. Une commande en cours conserve ses coûts et son échéance après changement de
    ruleset ; une nouvelle commande utilise les nouvelles règles.
14. Trois événements dus au même instant sont traités dans l'ordre documenté :
    bâtiment, recherche, production.
15. Un empire qui n'a jamais rien recherché ni produit reste jouable : toutes les
    pages répondent avec un état vide.
16. Une progression complète mène d'un empire nu au laboratoire, à une
    technologie, au chantier spatial, puis à des vaisseaux et des défenses.

## Contrôles

- `go test ./...` et `go test -race ./...` ;
- `go vet ./...`, `staticcheck ./...`, `govulncheck ./...` ;
- fuzz des formules de coût de recherche ;
- `BenchmarkEventProcessorBacklog` : 1 000 événements dus traités en une passe,
  budget observé d'environ 0,4 milliseconde par événement, chacun dans sa propre
  transaction ;
- `BenchmarkDelivered` : la livraison incrémentale est une opération entière en
  temps constant.
