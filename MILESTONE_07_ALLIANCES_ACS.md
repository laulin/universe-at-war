# Milestone 7 — Alliances et ACS

## Objectif

Permettre la création d'équipes persistantes, le partage volontaire
d'informations et les attaques/défenses groupées sans fusion automatique des
ressources ni privilège implicite.

## Travaux préparatoires obligatoires

Écrire `docs/rules/acs.md` : création d'un groupe, invitations, participants,
fenêtre d'ajout/retrait, synchronisation des arrivées, ralentissement, rappel,
partage du butin/débris et rapports. Relire les modes d'équipe du ruleset pour
distinguer alliances libres, équipes imposées, humains contre IA et PvPvE.

## Tranches d'implémentation

### 1. Domaine alliance

- alliance avec nom/tag uniques, description et historique ;
- adhésion unique, rôles internes et permissions explicites ;
- invitation avec expiration, acceptation/refus idempotents ;
- départ, exclusion et transfert de responsabilité ;
- diplomatie versionnée si activée ;
- aucune mise en commun automatique des ressources.

### 2. Persistance et autorisations

Tables alliances, membres, rôles, invitations, relations, rapports partagés et
historique. Toutes les mutations critiques sont transactionnelles et auditées.
Les projections joueur n'exposent que les données publiques, propres ou
partagées ; le rôle d'alliance ne confère aucun rôle serveur.

### 3. ACS attaque et défense

- groupe d'attaque avec propriétaire, cible et état ;
- invitations réservées aux membres autorisés ;
- ajout/retrait de flottes avec recalcul temporel ;
- une flotte plus lente peut retarder le groupe selon la règle documentée ;
- verrouillage avant résolution ;
- un seul combat multi-acteurs et rapports adaptés à chaque destinataire ;
- stationnement/défense groupée avec limites et heure de fin.

### 4. Interface

- page alliance : membres, rôles, invitations, diplomatie et historique ;
- partage explicite d'un rapport autorisé ;
- préparation ACS affichant participants et impact temporel avant confirmation ;
- états d'erreur compréhensibles si participant, cible ou flotte change.

## Tests obligatoires

- invitations concurrentes et double acceptation ;
- permissions de chaque rôle et isolation entre alliances ;
- aucune lecture d'un rapport non partagé ;
- arrivée simultanée et flotte ajoutée ralentissant le groupe ;
- rappel/retrait à la limite de la fenêtre ;
- participant banni, déconnecté ou sorti de l'alliance ;
- cible détruite ou modifiée avant résolution ;
- combat multi-acteurs, conservation et partage configuré du butin/débris ;
- scénario G complet et tests HTTP anti-IDOR.

## Séquence de commits recommandée

1. `docs: define alliance and acs rules`
2. `feat: model alliances roles and invitations`
3. `feat: persist shared intelligence permissions`
4. `feat: coordinate acs fleet groups`
5. `feat: resolve grouped attack and defense`
6. `feat: expose alliance and operation pages`
7. `test: prove milestone seven team invariants`

## Critère de sortie

Deux joueurs peuvent former une alliance, partager un rapport et résoudre une
attaque groupée avec timings et résultats cohérents. Un non-membre ne reçoit
aucune donnée privée et toutes les permissions sont testées côté serveur.
