# Acceptation — alliances, partage de rapports et opérations groupées

## Scénarios couverts

1. Une alliance se fonde avec un nom et une étiquette uniques, invite, accueille
   et exclut, et l'historique garde trace de chaque décision.
2. Chaque rang ne peut que ce que la table des permissions lui accorde : un
   membre n'invite pas, un officier n'exclut pas ses pairs, un fondateur
   transmet sa charge avant de partir.
3. Deux acceptations concurrentes de la même invitation ne font entrer le joueur
   que dans une alliance, et une invitation expirée n'est plus acceptable.
4. Un non-membre ne lit rien d'une alliance : ni ses membres, ni ses
   invitations, ni son historique, ni ses opérations.
5. Un rapport ne quitte son propriétaire que si celui-ci le partage
   explicitement ; quitter l'alliance coupe le partage, et personne ne partage
   le rapport d'un autre.
6. Une opération groupée synchronise toutes ses flottes sur une seule arrivée :
   une flotte lente retarde le groupe, une flotte rapide l'attend, et chacune
   rentre pour la durée de son propre trajet.
7. Aucune flotte engagée ne garde d'arrivée à elle : le groupe possède la
   sienne, et la redélivrance de cet événement ne livre pas un second combat.
8. La fenêtre d'ajout et de retrait se ferme exactement à la seconde de
   l'arrivée : une seconde avant, le retrait passe ; à l'arrivée, il est refusé.
9. Retirer la dernière flotte annule l'opération et son arrivée ; les autres
   flottes rentrent comme après n'importe quel rappel.
10. Un joueur sorti de l'alliance garde la flotte déjà engagée, qui combat avec
    le groupe, mais ne peut plus en ajouter ni consulter l'opération.
11. Une cible disparue ou déplacée avant l'arrivée annule l'opération, renvoie
    toutes les flottes et n'écrit aucun rapport.
12. Le combat groupé est unique et multi-acteurs : une partie par flotte
    attaquante, la planète et ses défenseurs alliés en face, un rapport par
    participant et un seul journal de bataille.
13. Le butin est prélevé une fois et réparti entre les flottes survivantes selon
    la place qui leur reste ; la somme distribuée égale exactement le butin.
14. Un combat groupé ne crée ni vaisseau ni ressource : les pertes annoncées par
    les rapports égalent les vaisseaux disparus du recensement, et rien ne
    devient négatif.
15. Une flotte en défense alliée attend sur place jusqu'à la fin de sa garde,
    combat pour la planète, puis rentre ; anéantie, elle ne rentre jamais et sa
    fin de garde est annulée avec elle.
16. Seul un allié, ou le joueur lui-même, peut stationner une flotte sur un
    corps ; la fin de garde doit tomber dans la fenêtre autorisée.
17. Les pages HTTP refusent tout accès indirect : l'opération d'une autre
    alliance est introuvable, le retrait de la flotte d'autrui est introuvable,
    et une action refusée par le rang est expliquée sans être exécutée.
18. L'audit de fuite balaie vingt routes joueur, alliance et opérations
    comprises, avec le nom d'alliance et l'opération du voisin comme sentinelles.

## Contrôles

- `go test ./...` et `go test -race ./...`, dont les acceptations concurrentes
  d'invitation jouées plusieurs fois ;
- la migration de réécriture des flottes est appliquée sur une base peuplée : les
  flottes en vol, leurs vaisseaux et leurs cargos survivent, et les clés
  étrangères sont vérifiées avant le commit ;
- le scénario G de la spécification est joué de bout en bout, de la fondation de
  l'alliance à la répartition du butin.
