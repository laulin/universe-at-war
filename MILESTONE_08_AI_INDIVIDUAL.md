# Milestone 8 — IA individuelle

## Objectif

Introduire des joueurs contrôlés par le serveur qui progressent avec les mêmes
coûts, délais, informations et risques qu'un humain. Une IA doit être observable,
imparfaite, inactive hors de ses horaires et incapable de tricher.

## Travaux préparatoires obligatoires

Documenter `docs/rules/ai-individual.md` : architecture stratégique,
opérationnelle et tactique, fréquence de réflexion, plages d'activité, mémoire,
fraîcheur de l'information, scores de décision, budgets et seeds. Définir les
archétypes mineur prudent, raider, fleeter, tortue, opportuniste, éclaireur,
logisticien et défenseur comme préférences, jamais comme scripts rigides.

## Tranches d'implémentation

### 1. Modèle et mémoire

- compte/joueur IA clairement identifié sans bypass d'autorisation métier ;
- profil versionné, objectifs, horizon, tolérance au risque et activité ;
- mémoire propre contenant seulement observations, rapports et faits publics ;
- décisions et raisons synthétiques persistées pour diagnostic ;
- aucune requête de projection permettant de lire la vérité ennemie.

### 2. Planificateur économique

- choisir mines, énergie, stockages, bâtiments, recherches et production ;
- passer exclusivement par les cas d'usage joueurs existants ;
- payer, attendre les files et gérer les ressources insuffisantes ;
- éviter le blocage durable tout en autorisant des décisions sous-optimales.

### 3. Activité militaire

- espionner avant une attaque significative ;
- scorer une cible à partir des seuls rapports connus et de leur fraîcheur ;
- calculer composition, cargo et recycleurs ;
- lancer, rappeler ou renoncer via le moteur de flotte ;
- fleetsaver avant la période inactive selon profil et connaissance ;
- accepter pertes, rapport obsolète et erreur d'estimation.

### 4. Scheduler IA

Événement `ai_think` durable, lots bornés, jitter déterministe et prochaine
réflexion persistée. Hors plage d'activité, aucune réaction instantanée : seule
la production et les missions déjà lancées continuent. Le worker doit rester au
repos entre deux échéances.

### 5. Administration minimale

Créer/retirer une IA, choisir archétype et horaires, afficher santé, prochaine
réflexion et objectifs. La vue debug omnisciente est explicitement réservée à
l'admin et séparée de la projection joueur normale.

## Tests obligatoires

- IA partant d'un empire vide et développant son économie ;
- paiement et délais identiques à un joueur ;
- aucune action nouvelle hors horaires ;
- même état/seed = décision reproductible ;
- archétypes produisant des priorités différentes ;
- espionnage requis avant raid significatif ;
- rapport ancien pouvant provoquer une erreur légitime ;
- cargo/recycleurs cohérents, flotte pouvant être perdue ;
- fleetsave avant sommeil et scénario H ;
- test structurel garantissant l'absence d'accès au repository de vérité adverse ;
- benchmark de plusieurs centaines de cycles sans boucle CPU permanente.

## Séquence de commits recommandée

1. `docs: define individual ai behavior`
2. `feat: model ai profiles memory and decisions`
3. `feat: add event-driven economic planner`
4. `feat: teach ai espionage raiding and fleetsave`
5. `feat: expose ai administration diagnostics`
6. `test: prove milestone eight fair play invariants`

## Critère de sortie

Au moins deux archétypes développent seuls un empire, espionnent, attaquent et
fleetsavent. Les traces prouvent que toutes leurs ressources et informations
proviennent des mêmes règles que celles des humains.
