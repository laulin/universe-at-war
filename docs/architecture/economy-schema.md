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
- `building_queue` est la file de construction du corps : chaque ligne capture
  son rang, son niveau cible, le coût réellement payé, la version de ruleset et,
  pour la seule ligne en tête, les deux instants de la construction ;
- `scheduled_events` porte l'échéance durable et la clé d'idempotence.

La migration `0019_build_queues.sql` transforme ce dernier emplacement unique en
file d'attente. Une ligne `'active'` est celle en cours de construction : elle
seule porte `started_at`, `completes_at` et un événement planifié. Les lignes
`'queued'` derrière elle n'ont ni horaire ni événement. Deux index partiels
tiennent l'invariant : un seul `'active'` par planète, et un rang unique parmi
les lignes non terminales. Le plafond de la file vient du ruleset
(`progression.queue_length`, dix par défaut).

Les `CHECK` interdisent soldes, niveaux, coûts et cases négatifs, les restes hors
de `[0, 3599]`, et une ligne en attente qui porterait un horaire.

## Frontières transactionnelles

La création d'empire alloue la première position libre (centre du système en
priorité), crée joueur, planète, ressources, rôle et événement de jeu dans une
transaction d'écriture sérialisée.

Une lecture économique est une courte transaction d'écriture car elle règle la
production paresseuse. La mise en file d'un bâtiment règle cette production,
relit les niveaux, y applique ceux que la file atteint déjà, réserve une case par
ligne non terminale, calcule le plan dans le domaine, **débite le coût
immédiatement**, ajoute la ligne, et — si elle arrive en tête — l'événement
planifié, avec la clé d'idempotence et le journal, avant un commit unique. Un
ordre entré dans la file est donc toujours financé : il ne peut jamais caler
faute de ressources.

L'achèvement règle d'abord la production à `due_at` avec les anciens niveaux,
applique le nouveau niveau, consomme une case, ferme file et événement, écrit
`building_completed`, puis **promeut la ligne suivante dans la même
transaction** : sa durée n'est calculée qu'à cet instant, avec les niveaux
d'usine du moment, et elle démarre à `due_at` pour que la file ne gagne aucun
temps mort. Une livraison répétée voit la file terminale et n'applique aucun
effet supplémentaire.

Annuler une ligne la ferme en `'cancelled'`, annule son événement encore
`'pending'`, libère la clé d'idempotence qu'elle occupait, rembourse le coût
capturé — écrêté à la capacité des entrepôts, le surplus étant signalé au
joueur — et promeut la suivante si la tête est partie.

L'annulation emporte aussi tout ce qui ne tient plus debout sans l'ordre
supprimé. La règle est celle que l'achèvement applique déjà : un ordre monte
exactement d'un niveau et ses prérequis sont satisfaits au moment où il démarre.
On rejoue la file sur cette base ; ce qui n'y survit pas est annulé et remboursé.
Annuler une mine de niveau 2 emporte donc son niveau 3, et annuler une usine de
robots en file emporte le chantier spatial qui comptait dessus.

## Boucle d'événements

Le worker sélectionne `(due_at, priority, id)`, traite par lots bornés, puis dort
jusqu'à la prochaine échéance. Un démarrage de bâtiment envoie un wake-up non
bloquant ; un rescan toutes les 30 secondes couvre un signal perdu. Les lectures
SSR règlent également les événements dus afin que la progression reste correcte
même avant le prochain rescan.
