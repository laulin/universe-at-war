# Acceptation — alliances d'intelligences artificielles

## Scénarios couverts

1. Un administrateur affecte un joueur artificiel à une alliance : le premier la
   fonde, le suivant y est invité et accepte, par les cas d'usage ordinaires.
   Un membre n'obtient jamais une seconde alliance.
2. Une croyance partagée porte son auteur, sa date d'observation, son
   expiration, sa confiance et le rapport dont elle découle.
3. Retirer le partage d'un rapport révoque instantanément tout ce qui en
   découlait ; la ligne reste, mais plus personne ne la lit.
4. Un rapport jamais partagé reste invisible aux autres membres, et illisible
   pour eux.
5. Une alliance ne lit rien de la mémoire d'une autre.
6. Une croyance périmée n'est plus lue et ne décide plus rien.
7. Le meneur répartit les rôles à partir des seules capacités déclarées ; il ne
   range que les membres qui se sont déclarés.
8. Un membre endormi ne reçoit aucun rôle actif et ne part jamais.
9. Une alliance ne poursuit qu'un plan à la fois, et le schéma lui-même le
   garantit.
10. Un plan que personne ne vient confirmer est abandonné à l'échéance.
11. Aucune attaque groupée n'est ouverte avant qu'un membre ait réellement
    espionné la cible et partagé ce qu'il a vu.
12. La flotte qui ouvre une opération rampe à 10 % de vitesse, ce qui laisse aux
    alliés le temps de la rejoindre ; deux machines résolvent ensemble une
    attaque groupée, en une seule bataille.
13. Sans quorum à la dernière réflexion avant l'atterrissage, le meneur retire
    sa flotte et l'opération est annulée.
14. Une cible qui se déplace avant l'atterrissage annule l'opération et renvoie
    toutes les flottes, sans un seul rapport de combat.
15. Un membre attaqué appelle à l'aide ; les alliés éveillés qui ont des
    vaisseaux vont stationner sur son corps, et ceux qui n'en ont pas
    enregistrent la raison.
16. Tant que l'alliance tient une cible, aucun membre n'ouvre sa propre attaque
    dessus, et personne n'espionne ni n'attaque un corps de sa propre alliance.
17. Deux univers de mêmes seeds prennent le même plan collectif dans le même
    ordre.
18. Une campagne complète ne crée ni vaisseau, ni ressource, ni croyance sans
    auteur.
19. La vue debug de l'administrateur montre l'alliance, le rôle, le plan et la
    provenance de chaque croyance ; aucun joueur n'y accède.
20. La liste de la section 60 est parcourue de bout en bout : développer,
    chercher, produire, espionner, apprendre, partager, coordonner, résoudre,
    perdre et expliquer.

## Contrôles

- `go test ./...` et `go test -race ./...` ;
- l'audit de fuite couvre les pages d'administration : un joueur ordinaire n'y
  trouve rien, pas même l'existence d'une alliance de machines ;
- le test d'isolation structurelle du Milestone 8 continue de garantir que
  `internal/ai` n'atteint aucun dépôt : la coordination passe par les mêmes cas
  d'usage que le reste.
