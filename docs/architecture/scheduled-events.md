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

La priorité départage les événements de même échéance. Chaque type possède une
valeur documentée ; une valeur plus faible est traitée en premier.

| Type d'événement | Priorité | Jalon |
| --- | ---: | --- |
| `combat_resolved` (arrivée d'une attaque) | 10 | 5 |
| `espionage_resolved` (arrivée d'un espionnage) | 20 | 5 |
| `fleet_arrived` | 30 | 4 |
| `fleet_returned` | 40 | 4 |
| `building_completed` | 50 | 2 |
| `research_completed` | 60 | 3 |
| `production_completed` | 70 | 3 |
| `holding_ended`, `acs_locked`, `jump_gate_ready` | 80 | 4, 6, 7 |
| `ai_think` | 90 | 8 |
| entretien (`protection_expired`, `ban_expired`) | 100 | 10 |

À échéance identique, un combat est donc résolu avant qu'une flotte défensive ne
reparte et avant qu'une production ne livre ses unités ; une intelligence
artificielle réfléchit après que le monde a changé.

## Traitement

Dans une transaction d'écriture, le worker relit l'événement et son agrégat,
vérifie qu'il est encore applicable, calcule la transition, persiste les effets
et événements suivants, puis marque l'événement terminé. Un crash avant commit
ne laisse aucun effet. Un événement déjà terminal devient un no-op réussi.

Un handler qui échoue annule sa transaction : aucun effet ne subsiste. Le nombre
de tentatives et la dernière erreur technique sont enregistrés dans une
transaction courte séparée. Au-delà de cinq tentatives, l'événement n'est plus
sélectionné et reste `pending` pour inspection administrative : un événement
empoisonné ne bloque jamais la simulation. Un type sans gestionnaire enregistré
suit la même politique.

Le worker dort jusqu'à la prochaine échéance. Une insertion peut le réveiller ;
un rescan périodique borné protège contre un signal perdu. Le domaine ne dort
jamais et les tests avancent une fake clock.

