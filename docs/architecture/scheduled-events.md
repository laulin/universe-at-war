# Événements planifiés

Le temps du jeu est piloté par des échéances, sans boucle de ticks.

## Enveloppe persistée

Un événement contient :

- identifiant entier monotone ;
- type stable ;
- `due_at` UTC ;
- priorité de type documentée ;
- type et identifiant de l'entité cible ;
- version du ruleset ;
- version du payload et payload strictement validé ;
- clé d'idempotence unique ;
- état `pending`, `processing`, `completed` ou `cancelled` ;
- dates de création et de traitement ;
- nombre de tentatives et dernière erreur technique éventuelle.

Le payload ne duplique que les valeurs nécessaires à la non-rétroactivité. La
vérité courante reste relationnelle.

## Ordre déterministe

Les événements sont sélectionnés par :

1. `due_at` croissant ;
2. `priority` croissante ;
3. `id` croissant.

La priorité n'est utilisée que lorsqu'une règle métier impose un ordre. Une
table documentée accompagnera chaque nouveau type.

## Traitement

Dans une transaction d'écriture, le worker relit l'événement et son agrégat,
vérifie qu'il est encore applicable, calcule la transition, persiste les effets
et événements suivants, puis marque l'événement terminé. Un crash avant commit
ne laisse aucun effet. Un événement déjà terminal devient un no-op réussi.

Le worker dort jusqu'à la prochaine échéance. Une insertion peut le réveiller ;
un rescan périodique borné protège contre un signal perdu. Le domaine ne dort
jamais et les tests avancent une fake clock.

