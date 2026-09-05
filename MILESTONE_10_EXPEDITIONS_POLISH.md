# Milestone 10 — Expéditions, profils et finition

## Objectif

Achever le produit distribuable : expéditions PvE déterministes, profils de
ruleset, inscriptions complètes, administration/modération, sauvegardes,
observabilité, UX, accessibilité et performances. Le résultat doit rester un
binaire autonome sans service externe obligatoire.

## Travaux préparatoires obligatoires

Écrire `docs/rules/expeditions.md` avec table de résultats, poids, limites,
pirates/aliens, retards, pertes et seed. Inventorier toutes les exigences encore
ouvertes de la spécification et établir une matrice traçable vers tests et écrans.
Ne déclarer aucune approximation silencieuse.

## Tranches d'implémentation

### 1. Expéditions

- mission avec durée, slot et conditions explicites ;
- résultats configurables : ressources, vaisseaux, retard, rien, pirates,
  aliens, pertes et événements rares ;
- tirage déterministe à seed persistée et rapport immuable ;
- application atomique du résultat et programmation du retour ;
- limites empêchant l'expédition de remplacer le cœur PvP/PvE stratégique.

### 2. Profils et règles

- profils fournis : proche classique, lent, accéléré, conflit, coopératif et
  PvPvE ;
- charger, sauvegarder, comparer, importer et exporter avec version de format ;
- aperçu des différences, avertissements et impact avant activation ;
- validateur unique partagé par wizard, import et administration ;
- non-rétroactivité vérifiée sur les événements déjà lancés.

### 3. Inscriptions et comptes

- politiques fermée, ouverte et sur invitation ;
- username unique, Argon2id, limitation d'abus et planète de départ atomique ;
- protections initiales sans privilège implicite ;
- parcours joueur indépendant du compte bootstrap administrateur ;
- messages ne révélant pas l'existence d'un compte ou d'une invitation.

### 4. Administration et modération

- dashboard : état, uptime, DB/WAL, prochain événement, backlog, débit,
  joueurs/IA actifs, erreurs, backups et ruleset ;
- configuration catégorisée avec unités, limites, défauts, diff et impact ;
- gestion comptes/rôles/statuts et avertissement permanent si l'admin joue ;
- modérateur limité aux joueurs et sanctions ;
- ban avec durée/justification, débannissement et audit ;
- un ban bloque le jeu mais ne gèle pas automatiquement l'empire.

### 5. Maintenance et sauvegardes

- commande `backup` et action admin utilisant l'API de sauvegarde SQLite ;
- nom horodaté, vérification d'intégrité et rétention optionnelle ;
- `doctor` étendu à écriture, WAL, permissions et cohérence du ruleset ;
- refus propre d'une sauvegarde provenant d'un schéma futur ;
- procédure documentée de restauration et de mise à niveau.

### 6. UX, temps réel et accessibilité

- compléter tous les écrans obligatoires et la navigation globale ;
- timestamps serveur, countdowns non authoritative et SSE seulement pour
  signaler un rafraîchissement ciblé, avec fallback lent ;
- responsive tablette/mobile, focus visible, contraste, labels et tableaux ;
- hostiles/alliés jamais signalés uniquement par couleur ;
- séparation des textes UI et identifiants, UTC stocké et formatage localisé ;
- assets originaux embarqués, sans pipeline Node requis.

### 7. Performance, observabilité et release

- métriques internes et logs `slog` corrélés sans secrets ;
- benchmarks production, événements, combat, galaxie et cycles IA ;
- profils CPU/mémoire sur des milliers d'événements et combats massifs ;
- aucune boucle active au repos, budgets mémoire/latence documentés ;
- matrice builds Linux/Windows/macOS, version, notes de release et procédure de
  démarrage depuis une DB vide.

## Tests obligatoires

- distribution d'expédition contrôlée et reproductibilité des seeds ;
- import corrompu/incompatible et comparaison de profils ;
- inscription pour chaque politique, invitation rejouée et création concurrente ;
- permissions admin/modérateur/joueur et scénario I de bannissement ;
- backup sous écritures, restauration puis `doctor` ;
- scénario J de changement de ruleset non rétroactif ;
- tests navigateur des parcours bootstrap, construction, flotte, rapport et ban ;
- audit anti-fuite d'information et anti-secret dans les logs ;
- accessibilité automatisée complétée par une revue clavier ;
- charge, benchmarks, race detector, fuzzing, vulnérabilités et cross-builds.

## Séquence de commits recommandée

1. `docs: define expeditions and release gap matrix`
2. `feat: resolve deterministic expeditions`
3. `feat: add versioned ruleset profiles`
4. `feat: complete registration and invitation flows`
5. `feat: complete administration and moderation`
6. `feat: add verified sqlite backups`
7. `feat: polish responsive accessible player experience`
8. `perf: validate event combat galaxy and ai workloads`
9. `test: complete end-to-end release acceptance`
10. `docs: publish standalone release operations`

## Critère de sortie

Un nouvel utilisateur télécharge un seul binaire, crée une DB, configure un
univers, inscrit des joueurs, observe des IA équitables, joue tous les systèmes,
administre et sauvegarde la partie. Tous les scénarios A à J, contrôles qualité,
tests de sécurité et builds multiplateformes sont verts, sans TODO bloquante.
