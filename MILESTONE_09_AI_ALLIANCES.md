# Milestone 9 — Alliances IA

## Objectif

Faire émerger une coopération légitime entre IA : mémoire partagée, rôles,
priorités communes, défense d'un membre et au moins une attaque ACS coordonnée.
L'alliance ne devient jamais une source d'omniscience.

## Travaux préparatoires obligatoires

Documenter `docs/rules/ai-alliances.md` : données partageables, provenance et
fraîcheur, attribution des rôles, élection d'une opération, quorum, délais de
réponse, abandon et résolution des conflits entre objectif individuel et
collectif. Réutiliser sans exception la fiche ACS du Milestone 7.

## Tranches d'implémentation

### 1. Mémoire partagée

- rapports explicitement partagés et observations publiques ;
- menaces, opportunités, débris, ennemis surveillés et horaires supposés ;
- provenance, auteur, timestamp, confiance et expiration ;
- révocation lorsque le partage ou l'appartenance cesse ;
- aucune jointure directe vers l'état réel d'un ennemi.

### 2. Rôles et stratégie collective

- scout, fleeter, mineur, recycleur, logisticien et défenseur ;
- allocation selon capacités connues, profil et disponibilité ;
- objectifs stratégiques bornés dans le temps ;
- changement de priorité après perte, menace ou information nouvelle ;
- décisions imparfaites et reproductibles à seed égale.

### 3. Opérations coordonnées

- campagne d'espionnage produisant réellement les rapports nécessaires ;
- plan ACS avec cible, fenêtre, participants et rôles ;
- invitations et flottes passées par les cas d'usage du Milestone 7 ;
- adaptation légitime si un membre refuse, dort ou perd sa flotte ;
- défense/stationnement d'un membre menacé lorsque les moyens le permettent ;
- recycleurs et partage des résultats selon le ruleset.

### 4. Observabilité et admin

Vue normale limitée aux informations accessibles à l'alliance. Vue debug admin
marquée montrant objectifs, rôles, prochaines actions, blocages et provenance de
chaque connaissance. Les logs corrèlent alliance, opération, agents et événements
sans révéler de secrets aux joueurs.

## Tests obligatoires

- un rapport non partagé reste inconnu des autres IA ;
- expiration et provenance de la mémoire ;
- mêmes seeds donnant le même plan collectif ;
- campagne de sondage avant attaque significative ;
- ACS réellement résolu avec au moins deux IA ;
- membre indisponible, rappel, flotte détruite ou cible modifiée ;
- défense d'un membre et refus lorsque les moyens manquent ;
- partage autorisé des rapports et interdiction inter-alliance ;
- aucune ressource, flotte ou connaissance créée ;
- test d'acceptation IA complet de la section 60.

## Séquence de commits recommandée

1. `docs: define ai alliance coordination rules`
2. `feat: add provenance-aware shared intelligence`
3. `feat: assign ai alliance roles and objectives`
4. `feat: coordinate ai scouting defense and acs`
5. `feat: expose alliance ai diagnostics`
6. `test: prove milestone nine coordination invariants`

## Critère de sortie

Une alliance IA collecte des renseignements, planifie et résout un ACS, peut
défendre un membre et réviser sa stratégie après un échec, sans aucun accès à
une information qu'un groupe humain équivalent ne posséderait pas.
