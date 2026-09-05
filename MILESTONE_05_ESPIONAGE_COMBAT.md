# Milestone 5 — Espionnage, combat et débris

## Objectif

Créer la première boucle d'affrontement complète : obtenir une information
partielle, lancer une attaque, résoudre un combat reproductible, piller, créer
un champ de débris, recycler et recevoir des rapports autorisés.

## Travaux préparatoires obligatoires

Écrire `docs/rules/espionage.md`, `docs/rules/combat.md` et
`docs/rules/debris.md` avant l'implémentation. Les documents figent niveaux de
révélation, obsolescence, rounds, ciblage, bouclier/coque, rapid fire, pillage,
capacité des recycleurs, arrondis, seed et partage d'information.

## Tranches d'implémentation

### 1. Vérité et connaissance

- séparer les agrégats réels des observations accessibles à un joueur ;
- créer des rapports immuables horodatés avec sections révélées/masquées ;
- conserver fraîcheur, cible et source d'une observation ;
- ne jamais charger puis masquer en CSS une information interdite ;
- ajouter lu/non lu, filtres et notifications hostiles.

### 2. Combat autonome

Développer une fonction domaine sans SQLite ni HTTP. Elle reçoit attaquants,
défenseurs, défenses, technologies, ruleset et seed. Elle renvoie vainqueur ou
nul, survivants, pertes, butin, débris, rounds, statistiques et données de
rapport. La RNG est injectée ou dérivée d'une seed persistée ; même entrée et
même seed produisent strictement le même résultat.

### 3. Missions et transactions

- mission espionnage consommant temps et sondes selon les règles ;
- attaque avec résolution unique à l'arrivée puis retour des survivants/butin ;
- champ de débris relationnel par position ;
- recyclage comme mission de flotte, jamais comme bouton instantané ;
- concurrence de recycleurs ordonnée par arrivée puis priorité/ID ;
- journal `espionage_resolved`, `combat_resolved`, `debris_created` et
  `debris_recycled` dans les mêmes transactions que leurs effets.

### 4. Interface

- galaxie/action rapide de sondage ou cible saisie sans donnée cachée ;
- historique d'espionnage distinguant récent, ancien et inconnu ;
- rapports de combat détaillés, lisibles et partageables plus tard ;
- débris visibles uniquement selon les règles publiques ;
- alertes hostiles fortes sans dépendre uniquement de la couleur.

## Tests obligatoires

- golden tests de combat et petits cas calculables ;
- fuzz/gros volumes : aucune unité négative, survivants bornés, débris positifs ;
- même seed = même rapport ; seeds différentes autorisées à diverger ;
- butin borné par stockage exposé, capacité cargo et taux de pillage ;
- aucun champ créé deux fois après redélivrance ;
- deux recycleurs concurrents ne prélèvent pas plus que le champ ;
- un endpoint joueur ne contient jamais flotte/défense non révélée ;
- scénario E complet espionnage → attaque → pertes → butin → débris → retour ;
- benchmarks du combat massif et de la page rapports.

## Séquence de commits recommandée

1. `docs: define espionage combat and debris rules`
2. `feat: model partial intelligence and reports`
3. `feat: implement deterministic combat engine`
4. `feat: resolve attack pillage and debris atomically`
5. `feat: add espionage recycling and report pages`
6. `test: prove milestone five combat invariants`

## Critère de sortie

Deux joueurs peuvent accomplir le scénario d'attaque complet. Le résultat est
rejouable techniquement, incertain pour le joueur, et aucune donnée secrète
n'apparaît dans le HTML ou les endpoints non autorisés.
