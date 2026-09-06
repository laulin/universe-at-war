# Acceptation — joueurs artificiels

## Scénarios couverts

1. Un joueur artificiel est un compte ordinaire de type `ai`, sans identifiant
   de connexion, avec un empire fondé par le même cas d'usage qu'un humain.
2. Seul un administrateur crée, retire ou inspecte un joueur artificiel ; un
   joueur ne peut ni les lister ni les voir, même en forgeant la requête.
3. Retirer un joueur artificiel l'arrête pour de bon, annule sa réflexion
   planifiée et désactive son compte, sans effacer son monde de la carte.
4. La réflexion se planifie elle-même : chaque tick porte sa propre clé, la
   prochaine échéance est persistée, et le worker dort jusque-là.
5. Hors de sa plage d'activité, un joueur artificiel ne prend aucune décision :
   l'événement se contente de le rendormir jusqu'à la prochaine ouverture et
   d'inscrire la raison dans son journal.
6. Parti d'un empire vide, il développe ses mines, ses stockages et son énergie
   en une demi-journée, sans jamais créer une ressource ni raccourcir une file.
7. Une IA et un humain paient exactement le même prix et attendent exactement le
   même temps pour la même construction.
8. À seed égale et état égal, deux univers identiques prennent les mêmes
   décisions dans le même ordre.
9. Deux archétypes ne se ressemblent pas : la tortue pointe son chantier vers le
   sol bien plus souvent que le raider.
10. Aucun raid n'est lancé sans un rapport d'espionnage complet et frais : sans
    rapport, l'IA envoie d'abord ses sondes.
11. Un rapport vieillissant peut coûter la flotte : la cible s'est fortifiée
    depuis, l'IA attaque sur ce qu'elle croit savoir et perd.
12. Le raid emporte ses vaisseaux de combat et juste assez de soutes ; ni
    tourelles, ni sondes.
13. Avant la nuit, un archétype prudent envoie sa flotte sur un autre de ses
    corps ; ce qui ne vole pas reste au sol.
14. Scénario H : attaquée pendant son sommeil, une IA subit le combat, ne réagit
    pas dans la nuit, et reprend son développement au matin.
15. Test structurel : `internal/ai` et `internal/domain/ai` n'importent ni
    `internal/storage`, ni `database/sql`, ni la couche web ; les règles
    n'importent pas non plus les cas d'usage.
16. Cinq cents cycles de réflexion mesurés, worker au repos entre deux
    échéances.
17. L'audit de fuite balaie les pages d'administration : un joueur ordinaire n'y
    trouve rien, pas même l'existence d'un joueur artificiel.

## Contrôles

- `go test ./...` et `go test -race ./...` ;
- `go test -bench=BenchmarkArtificialReflections -benchtime=500x -run=^$ ./tests/`,
  autour de 20 ms par cycle complet sur une machine de développement, pour une
  réflexion toutes les cinq minutes de temps de jeu ;
- le test d'isolation lit les fichiers du paquet, tests compris : une porte
  ouverte pour un test resterait une porte ouverte.
