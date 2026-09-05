# Milestone 3 — Recherches, chantier spatial et défenses

## Objectif

Étendre la progression économique du Milestone 2 avec les recherches, la
fabrication de vaisseaux et les défenses. À la fin du jalon, un joueur doit
pouvoir débloquer une technologie, construire les prérequis nécessaires puis
produire des unités utilisables par le futur moteur de flotte.

## État d'entrée

- univers actif et ruleset versionné ;
- planètes, ressources et production paresseuse ;
- bâtiments et événements planifiés durables ;
- transactions SQLite sérialisées et clés d'idempotence.

## Travaux préparatoires obligatoires

1. Relire les sections 16, 17, 43 à 51, 59 et 64 de la spécification.
2. Écrire `docs/rules/research.md` avant le code : coûts, durées, laboratoire,
   réseau intergalactique, graviton, cas limites et tests de référence.
3. Versionner un catalogue cohérent de recherches, vaisseaux et défenses. Les
   identifiants persistés ne doivent jamais être des libellés traduits.
4. Valider le graphe de prérequis et refuser les identifiants, coûts, effets ou
   cycles invalides lors de l'activation d'un ruleset.

## Tranches d'implémentation

### 1. Domaine recherche

- type d'identifiant et niveau de recherche ;
- graphe générique de prérequis bâtiments/recherches ;
- calcul du coût du prochain niveau ;
- calcul de durée selon laboratoire, vitesse et réseau multi-planètes ;
- effets explicitement exposés au domaine sans logique dans les handlers ;
- une seule recherche active par joueur, même avec plusieurs planètes.

### 2. Domaine chantier et défense

- catalogue générique d'unités avec famille, coût, durée, prérequis et effets ;
- commandes par quantité avec calcul sans débordement ;
- progression et achèvement déterministes d'un lot ;
- inventaires stationnés non négatifs ;
- séparation vaisseaux/défenses malgré une mécanique de production commune.

### 3. Persistance et transactions

Ajouter une migration pour les niveaux de recherche, la file de recherche, les
commandes de chantier, les inventaires de vaisseaux et les défenses. Les index
partiels garantissent les files actives autorisées.

Dans une transaction unique, un démarrage doit régler la production, relire le
ruleset et les prérequis, débiter tout le coût, créer la commande, l'événement,
la clé d'idempotence et le journal de jeu. Aucun débit ne survit sans commande.

Les événements `research_completed` et `production_completed` appliquent leurs
effets exactement une fois. Les coûts, durées et version de ruleset sont capturés
au démarrage pour préserver la non-rétroactivité.

### 4. Application et SSR

- page Recherche : niveaux, prérequis, coût, durée et file ;
- page Chantier : quantité, coût total, durée et inventaire ;
- page Défense : mêmes informations adaptées aux unités défensives ;
- PRG, CSRF, idempotence et messages métier distincts ;
- navigation clavier, labels et rendu mobile raisonnable ;
- aucun calcul authoritative dans le navigateur.

## Tests obligatoires

- tables de coûts et durées aux niveaux bas et élevés ;
- rejet d'un cycle et d'un prérequis manquant ;
- impossibilité de lancer deux recherches concurrentes ;
- double dépense et double soumission d'une commande ;
- coût `unité × quantité` atomique et sans débordement ;
- achèvement exactement une fois après redélivrance simulée ;
- inventaires et ressources jamais négatifs ;
- ancienne commande inchangée après changement de ruleset ;
- tests SQLite réels, HTTP, fuzz des formules et benchmark des grosses files.

## Séquence de commits recommandée

1. `docs: define research and unit production rules`
2. `feat: model research and production catalogues`
3. `feat: persist research and shipyard queues`
4. `feat: expose research shipyard and defense pages`
5. `test: prove milestone three progression invariants`

## Critère de sortie

Une partie existante reste jouable et un joueur peut construire ses prérequis,
terminer une recherche puis produire des vaisseaux et défenses sans intervention
administrative. Tests, race detector, `vet`, `staticcheck`, audit de vulnérabilités
et builds multiplateformes sont verts ; l'arbre Git est propre.
