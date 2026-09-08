# Bâtiments et coûts — règle de fidélité

## Comportement attendu

Une planète possède un niveau entier positif ou nul par bâtiment. Elle tient une
file de construction d'au plus `progression.queue_length` ordres, dix par défaut,
dont un seul se construit à la fois.

Mettre une construction en file règle d'abord la production, vérifie les
prérequis, les cases libres et le coût, **débite les ressources immédiatement**,
puis ajoute l'ordre à la fin de la file. Un ordre entré dans la file est donc
toujours financé et ne peut jamais caler faute de ressources. Le niveau ne change
qu'à l'achèvement.

Le niveau visé et les cases réservées tiennent compte de ce que la file atteint
déjà : trois mines enfilées visent les niveaux N+1, N+2 et N+3, et réservent
trois cases. La durée d'un ordre, en revanche, n'est calculée qu'au moment où il
prend la tête de la file : une usine de robots terminée entre-temps accélère
réellement tout ce qui attendait derrière elle.

Un ordre s'annule à tout moment, en cours comme en attente, et rembourse
intégralement. Les niveaux d'un même bâtiment formant une chaîne, annuler un
niveau annule aussi ceux qui étaient empilés au-dessus. Un remboursement ne
dépasse jamais la capacité des entrepôts : ce qui ne rentre pas est perdu, et le
joueur en est averti avant comme après.

Le catalogue initial contient les mines de métal/cristal/deutérium, la centrale
solaire, les trois stockages, l'usine de robots, l'usine de nanites, le chantier
spatial, le laboratoire, le silo à missiles et le terraformeur. Les bâtiments
lunaires seront ajoutés avec les lunes.

## Formules

Pour construire le niveau cible `L >= 1`, chaque composante du coût vaut :

```text
floor(base_cost * growth^(L - 1) * building_cost_multiplier)
```

Le facteur de croissance est propre au bâtiment. La durée est :

```text
work    = metal_cost + crystal_cost
seconds = floor(3600 * work
                / (2500 * (1 + robotics_level) * 2^nanite_level
                   * building_speed))
duration = max(1 seconde, seconds)
```

Le chantier, le laboratoire et les autres bâtiments n'accélèrent pas les
bâtiments. La construction consomme une case ; le terraformeur augmente le total
de 5 cases à son achèvement.

## Catalogue initial

| Identifiant | Coût M/C/D | Croissance | Prérequis |
| --- | ---: | ---: | --- |
| `metal_mine` | 60/15/0 | 1,5 | — |
| `crystal_mine` | 48/24/0 | 1,6 | — |
| `deuterium_synthesizer` | 225/75/0 | 1,5 | — |
| `solar_plant` | 75/30/0 | 1,5 | — |
| `metal_storage` | 1 000/0/0 | 2 | — |
| `crystal_storage` | 1 000/500/0 | 2 | — |
| `deuterium_tank` | 1 000/1 000/0 | 2 | — |
| `robotics_factory` | 400/120/200 | 2 | — |
| `nanite_factory` | 1 000 000/500 000/100 000 | 2 | usine robots 10 |
| `shipyard` | 400/200/100 | 2 | usine robots 2 |
| `research_lab` | 200/400/200 | 2 | — |
| `missile_silo` | 20 000/20 000/1 000 | 2 | chantier 1 |
| `terraformer` | 0/50 000/100 000 | 2 | nanites 1 |

Les identifiants sont persistés et ne dépendent pas de la langue d'affichage.

## Ordre et idempotence

Les événements sont sélectionnés par `(due_at, priority, id)`. Une fin de
construction utilise la clé `building-complete:<queue-id>` et vérifie encore que
la file est active. L'incrément de niveau, la suppression logique de la file,
l'écriture `building_completed` et le passage de l'événement à `completed` sont
atomiques. Une reprise après crash ne peut donc incrémenter qu'une fois.

## Exemples de référence

- Mine de métal niveau 1, multiplicateur 1 : 60 métal et 15 cristal.
- Mine de métal niveau 2 : 90 métal et 22 cristal (22,5 arrondi vers le bas).
- Mine de métal niveau 1 sans robot, vitesse 1 : `floor(3600 * 75 / 2500)`, soit
  108 secondes.
- Avec usine de robots niveau 1, la même construction dure 54 secondes.

## Cas limites et invariants

- niveau cible exactement égal au niveau courant + 1 au moment de l'achèvement ;
- coût figé à la commande, durée décidée à la prise de tête ;
- refus si prérequis, ressources, case libre ou place dans la file manquent ;
- jamais deux constructions en cours sur une planète ;
- un seul ordre en tête, un rang unique parmi les ordres non terminés ;
- aucun débit sans création de ligne de file ;
- un ordre en attente n'a ni horaire ni événement planifié ;
- une annulation rembourse exactement ce qui a été débité, écrêté aux entrepôts,
  et n'est jamais appliquée deux fois ;
- aucune seconde consommation de case lors d'une reprise d'événement ;
- coût ou durée non représentable rejeté, jamais saturé silencieusement.

## Tests de référence

- table de coûts des niveaux 1, 2 et élevés ;
- influence robots, nanites, vitesse et durée minimale ;
- prérequis et cases ;
- démarrage atomique et refus pour ressources insuffisantes ;
- mise en file de plusieurs niveaux, débit de chacun à la commande ;
- refus d'un ordre au-delà du plafond, sans rien dépenser ;
- promotion sans temps mort, à la vitesse des usines du moment ;
- annulation avec remboursement, cascade et écrêtage aux entrepôts ;
- double soumission et double dépense concurrente ;
- achèvement exactement une fois, y compris après reprise simulée ;
- ordre stable de deux événements au même instant.
