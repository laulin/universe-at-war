# Schéma des joueurs artificiels

La migration `0013` ajoute la nature d'un compte et tout ce qu'un joueur
artificiel possède en propre.

## Tables

- `accounts.kind` vaut `human` ou `ai`. Un compte d'IA n'a aucune ligne dans
  `password_credentials` : la connexion joint cette table, donc aucune session
  ne peut lui être ouverte.
- `ai_profiles` : le caractère et l'horloge d'un joueur artificiel — archétype,
  heures d'activité, intervalle, seed, numéro de tick, prochaine réflexion,
  réflexion due et dernière réflexion, plus une version optimiste.
- `ai_memory` : ce qu'il croit savoir, une ligne par corps observé, remplacée à
  chaque nouvelle observation. Rien n'y entre qui ne vienne de ses propres
  rapports ou de la carte publique.
- `ai_decisions` : son journal, une ligne par décision, avec le niveau, l'issue
  et la raison. C'est la trace qui prouve que rien n'a été obtenu autrement que
  par les règles.

## Événement

`ai_think` porte l'échéance d'une réflexion, à la priorité 90 : une IA réfléchit
une fois que le monde de cet instant a déjà changé. Chaque tick a sa clé
`ai-think:<joueur>:<tick>`, si bien qu'une réflexion est planifiée exactement
une fois et qu'un événement périmé est ignoré.

## Frontières transactionnelles

Le gestionnaire de l'événement ne délibère jamais. Dans une seule transaction,
il avance l'horloge du joueur, replanifie la réflexion suivante et, hors des
heures d'activité, inscrit un `sleep` sans rien décider d'autre. Le sommeil est
donc une règle du schéma, pas une préférence du cerveau.

La délibération elle-même se fait après le lot d'événements, par le `Thinker` du
worker. Elle passe par les cas d'usage joueurs, qui ouvrent chacun leur propre
transaction : une réflexion est une suite de commandes, exactement comme la
session d'un humain, et non une transaction géante.
