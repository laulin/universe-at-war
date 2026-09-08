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
| `acs_resolved` (arrivée d'une opération groupée) | 10 | 7 |
| `espionage_resolved` (arrivée d'un espionnage) | 20 | 5 |
| `fleet_arrived` | 30 | 4 |
| `fleet_returned` | 40 | 4 |
| `building_completed` | 50 | 2 |
| `research_completed` | 60 | 3 |
| `production_completed` | 70 | 3 |
| `holding_ended` (fin d'un stationnement), `jump_gate_ready` | 80 | 6, 7 |
| `ai_think` | 90 | 8 |
| entretien (`protection_expired`, `ban_expired`) | 100 | 10 |

À échéance identique, un combat est donc résolu avant qu'une flotte défensive ne
reparte et avant qu'une production ne livre ses unités ; une intelligence
artificielle réfléchit après que le monde a changé.

Une file de construction, de recherche ou de production ne planifie qu'un seul
événement : celui de l'ordre en tête, sous les clés `building-complete:<file>`,
`research-complete:<file>` et `production-complete:<commande>`. Les ordres qui
attendent derrière n'ont ni échéance ni événement. Achever la tête promeut le
suivant **dans la même transaction** : la promotion calcule sa durée avec les
installations du moment, le date à `due_at` pour que la file ne gagne aucun temps
mort, et planifie son événement. Annuler un ordre passe son événement encore
`pending` en `cancelled`, exactement comme un rappel de flotte.

L'arrivée d'une flotte porte le type de sa mission : une attaque planifie
`combat_resolved`, un espionnage `espionage_resolved`, toute autre mission
`fleet_arrived`. Les trois partagent la clé d'idempotence `fleet-arrive:<id>`,
si bien qu'un rappel annule l'arrivée sans connaître le type de la mission.

Une flotte engagée dans une opération groupée ne planifie aucune arrivée : le
groupe possède la sienne, `acs_resolved` sous la clé `acs-arrive:<groupe>`, et
fait atterrir toutes ses flottes ensemble. Une flotte en stationnement planifie
la fin de sa garde sous la clé `fleet-hold:<flotte>` ; une flotte détruite en
défendant voit cet événement annulé.

La réflexion d'un joueur artificiel porte la clé `ai-think:<joueur>:<tick>` :
chaque tick est planifié une fois et un événement périmé est ignoré. Son
gestionnaire ne délibère jamais — il avance l'horloge du joueur et, hors des
heures d'activité, le rendort sans prendre la moindre décision. La délibération
suit le lot d'événements et passe par les cas d'usage joueurs, chacun dans sa
propre transaction.

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

