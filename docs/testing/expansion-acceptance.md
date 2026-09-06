# Acceptation — colonisation, lunes, phalange et porte de saut

## Scénarios couverts

1. Une colonisation fonde une planète, consomme exactement un vaisseau de
   colonisation, dépose son cargo et renvoie le reste de la flotte.
2. Les caractéristiques d'une colonie sont tirées de la seed de la mission : la
   même seed donne la même planète, et une position proche du soleil est chaude.
3. Coloniser sans astrophysique, sans vaisseau de colonisation ou sur une
   position occupée est refusé sans laisser de flotte.
4. Deux colonisations simultanées sur la même position ne créent qu'une planète ;
   la perdante rentre avec son vaisseau intact.
5. Deux lancements concurrents avec un seul vaisseau de colonisation n'en font
   partir qu'un, et aucun inventaire ne devient négatif.
6. Une arrivée de colonisation redélivrée ne crée pas une seconde planète.
7. Un combat assez destructeur agrège une lune, une seule fois par position, et
   la redélivrance de l'événement n'en crée pas une seconde.
8. La création de lune est reproductible : la même seed donne le même résultat.
9. Une lune ne produit rien, n'a pas de plafond de stockage, n'accepte que les
   bâtiments lunaires et gagne des cases avec sa base lunaire.
10. La portée d'une phalange suit le carré de son niveau et la circularité de la
    galaxie ; une autre galaxie n'est jamais observable.
11. Un balayage est payé même s'il ne trouve rien, refusé sans deutérium, refusé
    hors de portée, et ne révèle ni composition ni cargo. Les missions
    d'espionnage restent invisibles.
12. Une porte de saut déplace des vaisseaux entre deux lunes du joueur, met les
    deux portes en recharge, et refuse une planète, elle-même, une lune
    étrangère, une lune sans porte, une défense ou plus de vaisseaux qu'elle n'en
    a. Deux sauts concurrents n'en réussissent qu'un et rien n'est créé.
13. La vue empire distingue les lunes des planètes, et les pages lunaires
    n'apparaissent que sur une lune.
14. L'audit de fuite balaie seize routes joueur, phalange et porte de saut
    comprises, avec des valeurs sentinelles plantées chez le voisin.

## Contrôles

- `go test ./...` et `go test -race ./...`, dont les sauts concurrents joués
  plusieurs fois ;
- la migration de réécriture est testée sur une base déjà peuplée : chaque
  planète et chacune de ses lignes filles survit, et les clés étrangères sont
  vérifiées avant le commit.
