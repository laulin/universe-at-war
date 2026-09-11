# Combat — règle de fidélité

## Comportement attendu

Le moteur de combat est une fonction pure : il reçoit les camps, les
technologies, les règles et une source aléatoire, et renvoie l'issue, les
survivants, les pertes, les débris et le détail des rounds. Il ne connaît ni
HTTP, ni SQLite. À entrée et seed égales, il rend strictement le même résultat.

La prévisualisation lancée depuis un rapport réutilise cette fonction sans
consulter la cible actuelle. Sa distribution et ses limites sont décrites dans
[`combat-simulation.md`](combat-simulation.md).

Les missiles ne participent jamais à un combat de flotte.

## Statistiques effectives

Elles sont calculées une fois par type, avec un arrondi au plus proche :

```text
arme(t)        = floor(arme_base(t)     * (1 + 0,1 * niveau_armes)     + 0,5)
bouclier(t)    = floor(bouclier_base(t) * (1 + 0,1 * niveau_bouclier)  + 0,5)
coque(t)       = floor(coque_base(t)    * (1 + 0,1 * niveau_protection)+ 0,5)
coque_base(t)  = (métal + cristal) du coût de base, divisé par 10
```

## Déroulement d'un round

1. Les boucliers de toutes les unités vivantes sont restaurés à leur maximum.
2. Les attaquants tirent dans l'ordre de disposition, puis les défenseurs. Les
   dégâts s'appliquent immédiatement, mais les destructions ne sont évaluées
   qu'en fin de round : les deux camps tirent donc sur le même état.
3. Chaque unité vivante tire une fois, sur une cible tirée uniformément parmi
   les unités ennemies vivantes au début du round.
4. Un tir dont les dégâts valent moins d'un centième du bouclier maximal de la
   cible rebondit sans effet. Sinon le bouclier absorbe, puis la coque encaisse
   le reste.
5. Après un tir sur une cible de type `T`, si le feu rapide vaut `n > 1`,
   l'unité tire à nouveau avec une probabilité de `(n − 1) / n`.
6. En fin de round, une unité dont la coque est nulle ou négative est détruite
   sans tirage. Une unité dont la coque est tombée sous 70 % de son maximum
   survit avec une probabilité égale à `coque / coque_max`.

Le combat s'arrête dès qu'un camp n'a plus d'unité, ou au bout du nombre maximal
de rounds.

## Ordre déterministe

La disposition est fixe : les parties dans l'ordre reçu, les types par
identifiant croissant, les instances par index. Une seule source aléatoire est
consommée, dans cet ordre : ciblage, feu rapide, explosions, puis reconstruction
des défenses. Changer cet ordre change les résultats : c'est une modification de
règle, et les tests de référence la détectent.

## Issue

Le camp qui n'a plus aucune unité a perdu. Si les deux camps ont encore des
unités après le nombre maximal de rounds, ou si les deux sont anéantis, le
combat est nul. Une cible sans aucune unité donne la victoire à l'attaquant sans
qu'aucun round n'ait lieu.

## Après la bataille

Reconstruction des défenses : pour chaque défense détruite, un tirage la
reconstruit avec la probabilité configurée. Les vaisseaux ne se reconstruisent
jamais.

Débris, une seule troncature par ressource et par famille :

```text
débris_métal = floor(part_vaisseaux * somme(pertes_vaisseaux * métal_payé))
             + floor(part_défenses  * somme(défenses_perdues_non_reconstruites * métal_payé))
```

Le résultat conserve séparément les deux termes, puis expose leur somme comme
débris total. Cette distinction est informative et ne modifie ni le champ de
débris recyclable ni le calcul de la chance de lune.

Le coût payé est le coût de base multiplié par le multiplicateur de la famille,
exactement comme au chantier. Le deutérium ne produit jamais de débris.

Chance de lune, enregistrée mais appliquée par le jalon des lunes :

```text
chance = min(chance_maximale, floor((débris_métal + débris_cristal) / 100000) / 100)
```

Pillage, lorsque l'attaquant gagne et qu'il lui reste au moins un survivant :

```text
taux        = min(taux_pillage_économie, pillage_maximal_combat)
disponible_i = floor(stock_i * taux)
capacité     = somme(survivants * cargo) - cargo déjà embarqué
passe 1 : butin_i = min(disponible_i, capacité / 3)
passe 2 : reste = capacité - somme(butin) ; métal et cristal prennent chacun
          jusqu'à reste / 2 de ce qui leur manque
passe 3 : le deutérium prend le reste
passe 4 : métal puis cristal prennent ce qui reste encore
```

La somme du butin vaut donc exactement le minimum entre la capacité et le total
disponible. Les technologies de l'attaquant sont celles qu'il possède à
l'arrivée, pas au lancement.

## Exemple calculable

Un chasseur léger (coque 400, bouclier 10, arme 50) contre un lance-missiles
(coque 200, bouclier 20, arme 80), sans technologie.

| Round | Coque du lance-missiles | Coque du chasseur | Tirages |
| ---: | ---: | ---: | --- |
| 1 | 170 (85 %) | 330 | aucun |
| 2 | 140 (70 %) | 260 (65 %) | survie du chasseur |
| 3 | 110 (55 %) | 190 (47,5 %) | survie des deux |

Les tirages de fin de round concernent d'abord les attaquants, puis les
défenseurs. Avec 0,10 puis 0,40 puis 0,60, le chasseur survit au round 2, survit
encore au round 3 et le lance-missiles explose : l'attaquant gagne en trois
rounds. Un quatrième tirage décide alors de la reconstruction du lance-missiles.
Avec la part de débris des défenses à zéro, cette victoire ne laisse aucun
débris.

Avec 0,10 puis 0,60, le chasseur explose au round 3 avant que le lance-missiles
ne tire son propre tirage : le défenseur gagne, et le chasseur détruit laisse
`floor(0,3 × 3000)` de métal et `floor(0,3 × 1000)` de cristal, soit 900 et
300.

## Cas limites et invariants

Aucun camp vide côté attaquant. Aucune quantité négative. Les survivants ne
dépassent jamais les effectifs initiaux. Les pertes valent exactement les
effectifs initiaux moins les survivants, reconstructions comprises. Les débris
sont positifs ou nuls. Le butin ne dépasse ni la capacité restante, ni la part
pillable du stock. Deux exécutions avec la même seed donnent le même résultat.

## Tests de référence

L'exemple calculable ci-dessus avec une source scriptée ; des combats témoins
enregistrés pour cinq situations ; le déterminisme à seed égale ; le fuzz des
invariants ; les tables de pillage et de reconstruction ; un combat de cent
mille unités par camp, qui se résout en environ 120 millisecondes pour une
centaine d'allocations.
