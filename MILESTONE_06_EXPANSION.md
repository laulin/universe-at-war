# Milestone 6 — Colonisation, lunes, phalange et porte de saut

## Objectif

Donner une profondeur spatiale à l'univers : plusieurs planètes, création et
usage de lunes, renseignement temporel par phalange et transfert exceptionnel
entre lunes par porte de saut.

## Travaux préparatoires obligatoires

Écrire `docs/rules/moon.md` et `docs/rules/phalanx.md`, puis compléter les fiches
de distance pour la colonisation et documenter la porte de saut : probabilité de
lune, taille, bâtiments lunaires, rayon/coût/visibilité de phalange, exceptions,
cooldown, cargo et compositions autorisées. Toutes les probabilités utilisent
une seed enregistrée.

## Tranches d'implémentation

### 1. Colonisation

- mission nécessitant colonisateur, astrophysique et slot de colonie ;
- validation des limites du ruleset et de la position visée ;
- résolution à l'arrivée dans une transaction qui relit la position ;
- création de planète, ressources, caractéristiques et ownership atomique ;
- échec propre et retour si une colonisation concurrente gagne la position.

### 2. Lunes

- corps céleste distinct d'une planète avec cases, bâtiments et flotte ;
- tentative de création issue du combat/débris, seed et chance capturées ;
- une seule lune par position selon la règle choisie ;
- catalogue des bâtiments lunaires, dont base, phalange et porte de saut ;
- aucune confusion entre stock/inventaire de la planète et de sa lune.

### 3. Phalange

- prélèvement atomique du coût ;
- calcul du rayon selon niveau et topologie ;
- projection dédiée ne contenant que les missions observables ;
- timestamps et informations strictement bornés par la fiche de fidélité ;
- aucune exposition indirecte de cargo, composition ou mission masquée.

### 4. Porte de saut

- validation de deux lunes compatibles appartenant au joueur ;
- transfert atomique des unités autorisées ;
- cooldown durable par porte et ruleset ;
- absence de trajet ou carburant classique seulement si documentée ;
- rejet des courses concurrentes et des destinations invalides.

### 5. SSR

Vue empire multi-planètes/lunes, sélection du corps actif, colonisation depuis
l'envoi de flotte, vue phalange exploitable pour le timing et formulaire de
porte de saut indiquant précisément le cooldown.

## Tests obligatoires

- deux colonisations simultanées sur la même position : une seule planète ;
- limite de colonies et prérequis ;
- génération déterministe des caractéristiques planétaires ;
- chance de lune bornée, seed reproductible et redélivrance idempotente ;
- rayon circulaire/non circulaire de phalange et missions invisibles ;
- aucune donnée cachée dans la réponse HTTP ;
- double saut concurrent, cooldown et conservation des vaisseaux ;
- crash/reprise de chaque événement terminal.

## Séquence de commits recommandée

1. `docs: define expansion fidelity rules`
2. `feat: resolve transactional colonization`
3. `feat: model moons and lunar buildings`
4. `feat: add phalanx intelligence projection`
5. `feat: implement jump gate cooldown transfers`
6. `feat: expose multi-world expansion pages`
7. `test: prove milestone six expansion invariants`

## Critère de sortie

Un joueur peut coloniser, posséder plusieurs corps, obtenir une lune selon une
règle reproductible, observer une flotte autorisée par phalange et transférer
une flotte entre deux lunes sans duplication.
