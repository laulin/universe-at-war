# Milestone 4 — Moteur de flotte

## Objectif

Rendre les vaisseaux mobiles par un moteur événementiel complet : calcul de
distance, vitesse et carburant, lancement atomique, arrivée, retour et rappel.
Il n'existe aucune conduite en temps réel : une flotte partie est engagée.

## Travaux préparatoires obligatoires

Produire avant le code `docs/rules/distances.md`, `docs/rules/fleet-speed.md` et
`docs/rules/fuel.md`. Chaque fiche précise formules, circularité des galaxies et
systèmes, arrondis, vitesse choisie, durée minimale, consommation, exemples et
tests de référence. Toute approximation doit être explicite et configurable.

## Tranches d'implémentation

### 1. Domaine pur

- distance entre coordonnées selon la topologie active ;
- caractéristiques de propulsion et vitesse effective selon technologies ;
- durée aller/retour et consommation de deutérium ;
- capacité cargo après carburant ;
- composition de flotte et cargo sans quantité négative ;
- missions initiales transport et stationnement ;
- machine d'état explicite : `prepared`, `outbound`, `resolving`, `returning`,
  `completed`, `recalled`, `destroyed` ;
- matrice des transitions et des rappels autorisés.

### 2. Persistance

Créer les tables flottes, compositions, cargos et transitions. Une flotte garde
origine, destination, propriétaire, mission, vitesse, départ, arrivée, retour,
version du ruleset et état. Les événements d'arrivée et de retour possèdent une
clé d'idempotence stable et un payload versionné minimal.

### 3. Cas d'usage atomiques

Le lancement règle les ressources, vérifie les vaisseaux, le carburant et les
slots, retire inventaire/cargo, crée flotte et événement puis journalise avant
un commit unique. L'arrivée de transport transfère le cargo selon les capacités,
puis programme le retour. Le stationnement change la planète porteuse selon ses
règles. Le rappel annule l'arrivée devenue invalide et calcule un retour à partir
du temps réellement parcouru.

### 4. Interface joueur

- flottes stationnées et en vol avec timestamps absolus ;
- assistant d'envoi : composition, destination, mission, vitesse, cargo,
  consommation, arrivée, retour, confirmation ;
- action de rappel uniquement lorsqu'elle est autorisée ;
- countdown JavaScript seulement décoratif, avec page correcte sans JavaScript ;
- PRG, CSRF, idempotence et contrôle strict de propriété côté serveur.

## Invariants et concurrence

- aucun vaisseau ni deutérium créé par un lancement ou un retour ;
- une flotte n'est jamais simultanément stationnée et en vol ;
- événement et action utilisateur sur la même flotte sont arbitrés par la
  transaction SQLite et l'état relu ;
- rappel et arrivée concurrents donnent une seule transition valide ;
- une ancienne flotte conserve ses timings après changement de ruleset ;
- ordre d'événements `(due_at, priority, id)` documenté.

## Tests obligatoires

- tables et fuzz distance/vitesse/carburant ;
- scénario D complet de la spécification ;
- carburant ou composition insuffisants sans mutation partielle ;
- double lancement avec le même inventaire ;
- transport plafonné et retour cohérent ;
- rappel avant, pendant et après l'arrivée ;
- crash avant commit puis reprise sans duplication ;
- test HTTP du workflow complet et absence de fuite entre joueurs ;
- benchmark de calcul et insertion de milliers de missions.

## Séquence de commits recommandée

1. `docs: define fleet travel fidelity rules`
2. `feat: model fleet travel and state transitions`
3. `feat: persist atomic fleet missions`
4. `feat: resolve fleet arrival return and recall events`
5. `feat: expose fleet launch workflow`
6. `test: prove milestone four fleet invariants`

## Critère de sortie

Un joueur peut envoyer une flotte de transport ou stationnement, observer ses
horaires, la rappeler lorsque permis, puis récupérer un inventaire et un cargo
cohérents après arrivée et retour.
