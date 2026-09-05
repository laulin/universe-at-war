# Schéma univers et économie

La migration `0004_universe_economy.sql` sépare l'identité de jeu du compte de
connexion et conserve les invariants critiques dans SQLite.

## Agrégats persistés

- `players` lie au plus un empire à un compte et lui accorde le rôle `PLAYER` ;
- `planets` possède une position `(galaxy, system, position)` globalement unique,
  des températures et un budget de cases ;
- `planet_resources` contient les trois soldes, les restes en unités-secondes,
  l'instant du dernier règlement et une version ;
- `planet_buildings` ne contient que les niveaux terminés ;
- `building_queue` capture le niveau cible, le coût réellement payé, la version
  de ruleset et les deux instants de la construction ;
- `scheduled_events` porte l'échéance durable et la clé d'idempotence.

Une seule ligne `building_queue` active est autorisée par planète grâce à un
index unique partiel. Les `CHECK` interdisent soldes, niveaux, coûts et cases
négatifs, ainsi que les restes hors de `[0, 3599]`.

## Frontières transactionnelles

La création d'empire alloue la première position libre (centre du système en
priorité), crée joueur, planète, ressources, rôle et événement de jeu dans une
transaction d'écriture sérialisée.

Une lecture économique est une courte transaction d'écriture car elle règle la
production paresseuse. Le démarrage d'un bâtiment règle cette production, relit
les niveaux, calcule le plan dans le domaine, débite, crée la file, l'événement
planifié, la clé d'idempotence et le journal avant un commit unique.

L'achèvement règle d'abord la production à `due_at` avec les anciens niveaux,
applique le nouveau niveau, consomme une case, ferme file et événement, puis
écrit `building_completed`. Une livraison répétée voit la file terminale et
n'applique aucun effet supplémentaire.

## Boucle d'événements

Le worker sélectionne `(due_at, priority, id)`, traite par lots bornés, puis dort
jusqu'à la prochaine échéance. Un démarrage de bâtiment envoie un wake-up non
bloquant ; un rescan toutes les 30 secondes couvre un signal perdu. Les lectures
SSR règlent également les événements dus afin que la progression reste correcte
même avant le prochain rescan.
