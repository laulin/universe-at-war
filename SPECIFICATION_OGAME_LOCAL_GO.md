# Spécification maître — Jeu spatial persistant local inspiré d’OGame

> **Document destiné à un LLM / agent de développement.**
>
> Ce document est la source de vérité fonctionnelle et architecturale du projet.  
> Le but n’est pas de produire un simple prototype, mais un jeu réellement jouable, maintenable, testable et extensible, avec un niveau de qualité production.
>
> **Ne pas commencer par “faire des écrans”.** Commencer par les invariants du domaine, les tests, le moteur temporel et les transactions. Le front-end est une projection ergonomique du moteur, jamais l’inverse.

---

## 0. Mission

Construire un jeu de stratégie spatiale persistant, jouable en local ou sur un petit réseau privé, reproduisant fidèlement les sensations fondamentales d’OGame :

- empire planétaire persistant ;
- production de ressources continue ;
- bâtiments et recherches à durée réelle ;
- flotte composée de vaisseaux ;
- missions avec heure de départ, d’arrivée et de retour ;
- espionnage et information imparfaite ;
- combats automatiques ;
- pertes durables ;
- champs de débris et recyclage ;
- colonisation ;
- lunes ;
- phalange ;
- portes de saut ;
- fleetsave ;
- attaques groupées et défenses groupées ;
- alliances / équipes ;
- IA indépendantes ou regroupées en alliances ;
- rapports comme principal vecteur narratif ;
- univers configurable en profondeur.

Le jeu doit pouvoir créer naturellement des situations de ce type :

> Une alliance humaine surveille depuis plusieurs jours les habitudes d’une alliance IA.  
> Une lune et une phalange permettent de détecter une fenêtre de retour de flotte.  
> Plusieurs joueurs coordonnent une attaque groupée et leurs recycleurs.  
> L’alliance IA peut éventuellement détecter la menace, sonder, rappeler certaines flottes ou tenter une défense groupée.  
> Le résultat dépend des informations réellement disponibles, des timings, de la composition des flottes et du moteur de combat.

Si le gameplay devient principalement une comparaison de “puissance totale” ou un RTS où l’on pilote les vaisseaux, l’objectif est manqué.

---

# 1. Contraintes non négociables

## 1.1 Technologie

Le serveur MUST :

- être écrit en **Go** ;
- cibler Go 1.27.x ou une version stable plus récente compatible ;
- utiliser **SQLite** comme unique base persistante ;
- ne nécessiter aucun serveur de base de données externe ;
- ne nécessiter Redis, PostgreSQL, MySQL, RabbitMQ, Kafka ou autre service externe ;
- être distribuable sous la forme d’un **seul binaire applicatif** ;
- embarquer les templates HTML, CSS, JavaScript, migrations, règles par défaut et autres assets nécessaires ;
- pouvoir fonctionner hors connexion Internet après installation ;
- fonctionner au minimum sous Linux amd64/arm64 ;
- viser également Windows amd64 et macOS amd64/arm64.

Le driver SQLite SHOULD être `modernc.org/sqlite` afin de conserver un build sans CGO.

Le projet MUST utiliser `database/sql`.

Le nom du jeu est Universe At War.

## 1.2 Architecture

Le projet MUST être un **monolithe modulaire**.

Interdictions :

- microservices ;
- RPC interne inutile ;
- bus de messages externe ;
- architecture distribuée ;
- dépendance à un cloud ;
- dépendance obligatoire à un service tiers ;
- SPA React/Vue/Angular/Svelte ;
- logique métier dans les handlers HTTP ;
- logique métier dans du JavaScript client.

Le domaine doit rester exécutable et testable sans serveur HTTP.

## 1.3 Qualité

Le développement MUST être réalisé en **TDD** :

1. test rouge ;
2. implémentation minimale ;
3. test vert ;
4. refactor ;
5. répétition.

Aucune fonctionnalité métier importante ne doit apparaître sans tests associés.

Le projet MUST réussir en CI :

- tests unitaires ;
- tests d’intégration SQLite ;
- tests de concurrence pertinents ;
- `go test -race ./...` lorsque supporté ;
- `go vet ./...` ;
- `staticcheck` ;
- `govulncheck` ;
- formatage Go ;
- tests de migration ;
- tests des invariants du moteur.

---

# 2. Philosophie générale

## 2.1 Fidélité recherchée

La fidélité recherchée concerne avant tout :

- le rythme ;
- l’économie ;
- les décisions ;
- la logistique ;
- le risque ;
- la temporalité ;
- l’information imparfaite ;
- les rapports ;
- les pertes ;
- le fleetsave ;
- la chasse ;
- les interactions d’alliance.

La fidélité ne doit pas dépendre de la copie d’assets graphiques, de textes ou de marques propriétaires.

Les images, icônes, textes d’ambiance, noms et identité visuelle doivent être originaux si le projet doit être diffusé.

## 2.2 Principe cardinal

L’unité fondamentale du serveur est **l’événement temporel**, pas la frame.

Le moteur n’est pas un game loop à 30 ou 60 ticks/s.

Les principales opérations sont :

- une construction se termine à T ;
- une recherche se termine à T ;
- une flotte arrive à T ;
- un espionnage se résout à T ;
- un combat se résout à T ;
- une flotte revient à T ;
- une expédition se résout à T ;
- un recyclage se résout à T.

La simulation MUST être principalement événementielle et paresseuse.

## 2.3 Temps et ressources

Les ressources ne doivent pas être incrémentées toutes les secondes.

Pour une planète, conserver au minimum :

- stock de ressources au dernier règlement ;
- timestamp du dernier règlement ;
- taux de production applicable ;
- capacité maximale de stockage ;
- état énergétique applicable.

À la lecture ou mutation nécessitant un état exact :

1. calculer le temps écoulé ;
2. calculer la production ;
3. appliquer les plafonds ;
4. mettre à jour le snapshot si la transaction le nécessite.

Cette opération MUST être testable de façon déterministe.

---

# 3. Architecture logique

Le système doit être séparé en modules explicites.

## 3.1 Domain

Contient les règles métier pures.

Sous-domaines recommandés :

- Account
- Moderation
- Universe
- Clock
- Rules
- Player
- Planet
- Moon
- Economy
- Building
- Research
- Shipyard
- Fleet
- Mission
- Espionage
- Combat
- Debris
- Colonization
- Alliance
- ACS
- Expedition
- AI
- Reports

Le domaine MUST ignorer :

- HTTP ;
- templates ;
- cookies ;
- SQLite ;
- chemins de fichiers.

## 3.2 Application

Contient les cas d’usage et orchestrations :

- créer un compte ;
- lancer une construction ;
- lancer une recherche ;
- envoyer une flotte ;
- rappeler une flotte ;
- espionner ;
- rejoindre une alliance ;
- lancer une attaque groupée ;
- bannir un utilisateur ;
- modifier un ruleset ;
- traiter un événement planifié.

Les règles d’autorisation s’appliquent à ce niveau ou dans une couche dédiée appelée par l’application.

## 3.3 Infrastructure

Contient :

- repositories SQLite ;
- transactions ;
- migrations ;
- horloge système ;
- génération cryptographique ;
- gestion des sessions ;
- logging ;
- backup ;
- fichiers embarqués.

## 3.4 Simulation

Responsable de :

- sélectionner les événements arrivés à échéance ;
- les traiter dans un ordre déterministe ;
- appliquer les mutations dans une transaction ;
- produire les rapports ;
- produire les nouveaux événements ;
- garantir l’idempotence.

## 3.5 Web

Responsable uniquement de :

- routage ;
- parsing/validation transport ;
- appel des use cases ;
- mapping erreurs HTTP ;
- rendu HTML ;
- petites réponses partielles ;
- SSE éventuel ;
- sécurité web.

## 3.6 AI

L’IA utilise les **mêmes commandes applicatives qu’un humain**.

Interdiction formelle de modifier directement :

- ressources ;
- flottes ;
- planètes ;
- rapports ;
- technologies ;
- événements.

Une IA ne doit jamais bénéficier d’une API interne “cheat”.

---

# 4. Organisation recommandée du dépôt

L’agent peut ajuster les noms, mais la séparation conceptuelle doit rester.

- `/cmd/`
  - exécutable principal et sous-commandes
- `/internal/domain/`
- `/internal/app/`
- `/internal/sim/`
- `/internal/ai/`
- `/internal/storage/sqlite/`
- `/internal/auth/`
- `/internal/web/`
- `/internal/admin/`
- `/internal/moderation/`
- `/internal/observability/`
- `/migrations/`
- `/web/templates/`
- `/web/static/`
- `/rules/`
- `/docs/`
- `/tests/`

Les migrations, templates et assets finaux MUST être embarqués dans le binaire.

---

# 5. SQLite

## 5.1 Principes

SQLite est la base de données de production, pas uniquement une base de test.

Le schéma et les accès doivent être conçus pour ses caractéristiques.

Activer explicitement et tester :

- foreign keys ;
- WAL ;
- busy timeout ;
- niveau de synchronisation choisi ;
- checkpoints ;
- intégrité au démarrage.

Les timestamps MUST être stockés en UTC.

L’affichage utilise le fuseau configuré pour l’univers ou le navigateur.

## 5.2 Concurrence

SQLite n’autorise qu’un nombre limité d’écritures concurrentes utiles.

Le projet SHOULD :

- utiliser un chemin d’écriture contrôlé ;
- sérialiser proprement les mutations importantes ;
- conserver plusieurs lectures concurrentes si nécessaire ;
- éviter les transactions longues ;
- ne jamais effectuer d’appel réseau dans une transaction ;
- ne jamais rendre un template dans une transaction.

Une stratégie recommandée est :

- handle/pool lecture ;
- handle écriture avec concurrence très limitée ;
- WAL pour permettre aux lectures de continuer durant les écritures ;
- événements de simulation traités dans de petites transactions atomiques.

## 5.3 Migrations

Les migrations MUST :

- être versionnées ;
- être embarquées ;
- être appliquées automatiquement au démarrage ;
- être transactionnelles lorsque SQLite le permet ;
- être testées depuis une DB vide ;
- être testées depuis chaque version supportée ;
- ne jamais effectuer de downgrade automatique destructeur.

Avant migration majeure, le serveur SHOULD pouvoir produire un backup.

## 5.4 Tables STRICT

Utiliser les tables SQLite `STRICT` lorsque cela améliore la sécurité du schéma.

## 5.5 Intégrité

Utiliser :

- foreign keys ;
- UNIQUE ;
- CHECK ;
- NOT NULL ;
- index explicites ;
- contraintes métier raisonnables en DB.

Ne pas compter uniquement sur Go pour garantir l’intégrité.

---

# 6. Modèle de données conceptuel

Ce document n’impose pas une normalisation exacte, mais le schéma doit représenter explicitement au minimum :

## Infrastructure / administration

- metadata DB
- migrations
- server state
- accounts
- password credentials
- sessions
- bans
- audit log
- universe
- ruleset versions
- configuration changes
- backups metadata optionnelle

## Jeu

- players
- galaxies/systems/positions ou représentation déterministe équivalente
- planets
- moons
- planet resources
- building levels
- research levels
- ship inventories
- defense inventories
- building queues
- research queues
- shipyard queues
- fleets
- fleet ship composition
- fleet cargo
- missions
- scheduled events
- debris fields
- espionage reports
- combat reports
- expedition reports
- messages / notifications
- alliances
- alliance memberships
- invitations
- ACS groups
- diplomacy

## IA

- AI profiles
- AI activity schedules
- AI strategic state
- AI known intelligence
- AI observations
- AI objectives
- AI alliance shared intelligence

Toutes les entités métier importantes SHOULD avoir :

- identifiant stable ;
- création ;
- mise à jour si pertinente ;
- version ou mécanisme équivalent lorsque nécessaire pour éviter les écritures perdues.

---

# 7. Bootstrap et premier démarrage

## 7.1 DB inexistante

Lorsque la DB n’existe pas :

1. créer le fichier SQLite ;
2. appliquer les migrations ;
3. créer l’état serveur `BOOTSTRAP_PENDING` ;
4. créer automatiquement le compte administrateur initial ;
5. générer son mot de passe via CSPRNG ;
6. stocker uniquement le hash Argon2id ;
7. afficher le mot de passe **une seule fois** dans le terminal ;
8. marquer le compte `must_change_password=true`.

Le mot de passe initial doit avoir au minimum 128 bits d’entropie réelle.

Préférence :

- 24 à 32 octets aléatoires ;
- représentation base64url ou autre représentation facile à copier.

Ne jamais :

- utiliser `admin/admin` ;
- utiliser un mot de passe fixe ;
- dériver le mot de passe du hostname ;
- enregistrer le mot de passe en clair dans la DB ;
- réafficher le mot de passe après redémarrage.

## 7.2 Perte du mot de passe

Le binaire MUST fournir une commande locale de récupération administrative, par exemple conceptuellement :

- réinitialisation du mot de passe d’un administrateur ;
- génération d’un nouveau secret aléatoire ;
- invalidation des sessions existantes ;
- journalisation de l’action dans l’audit local si possible.

Cette opération doit nécessiter un accès local au fichier DB / à la machine.

## 7.3 Serveur non configuré

Tant que le setup initial n’est pas validé :

Le serveur MUST refuser l’accès aux fonctionnalités de jeu.

Seuls doivent être accessibles :

- login ;
- logout ;
- setup ;
- assets nécessaires ;
- health minimal si nécessaire.

Un joueur ne doit pas pouvoir s’inscrire ou créer un empire avant la fin du setup.

---

# 8. Assistant de setup initial

Après connexion du bootstrap admin, lancer automatiquement le wizard.

Étapes minimales :

## Étape 1 — Identité

- nom de l’univers ;
- langue ;
- fuseau horaire ;
- description ;
- visibilité réseau ;
- politique d’inscription.

## Étape 2 — Topologie

- nombre de galaxies ;
- systèmes par galaxie ;
- positions par système ;
- géométrie circulaire ou non ;
- paramètres de colonisation ;
- caractéristiques de génération des planètes ;
- distance inter-systèmes ;
- distance inter-galaxies.

## Étape 3 — Temps

Paramètres séparés, pas un simple multiplicateur global :

- vitesse économique ;
- vitesse bâtiments ;
- vitesse recherches ;
- vitesse chantier spatial ;
- vitesse défenses ;
- vitesse flotte pacifique ;
- vitesse flotte hostile ;
- vitesse flotte stationnement ;
- vitesse expédition ;
- durée minimale de mission ;
- éventuels horaires actifs de l’univers ;
- politique de pause si cette fonctionnalité est activée.

## Étape 4 — Économie

- production de base métal ;
- production de base cristal ;
- production de base deutérium ;
- coefficients de croissance des mines ;
- coûts de base ;
- exposants de coûts ;
- consommation énergétique ;
- stockage ;
- taux de pillage ;
- consommation de deutérium ;
- bonus/malus éventuels ;
- production en cas d’énergie insuffisante.

## Étape 5 — Combat

- nombre maximal de rounds ;
- paramètres de bouclier ;
- coque ;
- armes ;
- rapid fire ;
- part des vaisseaux transformée en débris ;
- part des défenses transformée en débris ;
- reconstruction éventuelle des défenses ;
- chance et paramètres de création de lune ;
- limites de butin ;
- paramètres des recycleurs ;
- paramètres missiles.

## Étape 6 — Progression

- coûts et coefficients bâtiments ;
- coûts et coefficients recherches ;
- coûts vaisseaux ;
- coûts défenses ;
- prérequis ;
- temps de construction ;
- bonus de laboratoire ;
- réseau de recherche ;
- limite de colonies ;
- effets des technologies.

## Étape 7 — Jeu d’équipe

- alliances autorisées ;
- taille maximale ;
- ACS activé ;
- défense groupée activée ;
- partage de rapports ;
- partage d’intelligence ;
- diplomatie ;
- mode de départ : libre / équipes prédéfinies / humain contre IA / PvPvE.

## Étape 8 — IA

- nombre d’IA ;
- nombre d’IA indépendantes ;
- nombre d’alliances IA ;
- taille des alliances ;
- archétypes ;
- difficulté ;
- fréquence de réflexion ;
- horaires d’activité ;
- niveau initial ;
- règles de diplomatie ;
- degré de coordination.

## Étape 9 — Protection et règles joueurs

- protection débutant ;
- protection contre écart de points ;
- nombre de comptes autorisés ;
- vacances ;
- inactivité ;
- suppression automatique ;
- inscription ouverte/fermée ;
- limite de joueurs.

## Étape 10 — Validation

Afficher :

- résumé complet ;
- paramètres modifiés par rapport au profil de base ;
- paramètres qui seront verrouillés ;
- avertissements ;
- estimation qualitative de vitesse de partie.

L’admin confirme explicitement.

Le serveur passe ensuite à `RUNNING`.

---

# 9. Profils de règles

## 9.1 Pas de “x100” unique

Le panneau admin MUST exposer les paramètres individuellement.

Un “profil” est seulement un ensemble de valeurs préremplies.

Exemples de profils fournis :

- Classic
- Classic rapide
- Coopération locale
- Guerre accélérée
- Longue campagne

L’admin peut :

- charger un profil ;
- modifier chaque paramètre ;
- sauvegarder un nouveau profil ;
- exporter/importer un profil ;
- comparer deux profils ;
- voir le diff.

## 9.2 Versionnement

Les règles MUST être versionnées.

Toute modification importante crée une nouvelle version immuable du ruleset.

Conserver :

- auteur ;
- date ;
- ancienne valeur ;
- nouvelle valeur ;
- justification optionnelle ou obligatoire selon criticité ;
- date d’entrée en vigueur.

## 9.3 Classes de réglages

Chaque paramètre doit appartenir à une classe :

### A — Verrouillé après démarrage

Exemples :

- topologie fondamentale ;
- structure des coordonnées ;
- certains choix de génération.

Modification interdite après le lancement sauf procédure de migration administrative explicite.

### B — Modification à risque

Exemples :

- vitesse flotte ;
- combat ;
- coûts ;
- débris ;
- production.

Nécessite :

- confirmation ;
- résumé d’impact ;
- audit ;
- éventuellement univers en pause ;
- nouvelle version de règles.

### C — Modification live-safe

Exemples :

- texte ;
- paramètres UI ;
- inscription ;
- certains paramètres de modération.

Peut être appliquée immédiatement.

## 9.4 Non-rétroactivité

Une modification de règles ne doit pas produire d’effets absurdes sur les actions déjà engagées.

Exemples :

- une flotte déjà partie conserve son heure d’arrivée ;
- un coût déjà payé n’est pas recalculé ;
- une construction en cours conserve sa durée sauf règle explicitement documentée ;
- une mission conserve les paramètres nécessaires à sa résolution lorsque changer la règle en vol serait injuste.

Les événements planifiés doivent embarquer ou référencer la version nécessaire à leur résolution.

---

# 10. Comptes et rôles

Trois rôles :

- `ADMIN`
- `MODERATOR`
- `PLAYER`

## 10.1 Séparation compte / joueur

Un compte et un empire joueur sont deux concepts distincts.

Un administrateur peut être un compte purement administratif.

Le bootstrap admin SHOULD ne pas posséder d’empire par défaut.

Un modérateur est un joueur doté de capacités supplémentaires.

## 10.2 ADMIN

Peut :

- accéder à toute la configuration ;
- gérer les comptes ;
- promouvoir/rétrograder ;
- bannir/débannir ;
- changer les règles ;
- gérer l’IA ;
- mettre en pause/reprendre l’univers ;
- consulter l’audit ;
- lancer des sauvegardes ;
- exécuter les outils de maintenance ;
- gérer les inscriptions ;
- voir l’état de simulation ;
- consulter les erreurs techniques.

Attention :

L’accès admin à des informations cachées de gameplay doit être clairement signalé.

Si un admin joue également, l’UI d’administration doit afficher un avertissement permanent afin d’éviter toute confusion entre information administrative omnisciente et information acquise en jeu.

## 10.3 MODERATOR

Le modérateur est un joueur.

Il peut :

- consulter les outils de modération nécessaires ;
- bannir temporairement ou définitivement selon politique ;
- débannir si autorisé ;
- renseigner une justification obligatoire ;
- consulter l’historique de sanctions.

Il ne peut pas :

- modifier l’économie ;
- modifier l’univers ;
- modifier l’IA ;
- modifier les technologies ;
- voir les secrets techniques ;
- promouvoir un admin ;
- modifier directement ressources/flottes ;
- accéder aux credentials.

## 10.4 PLAYER

Accès uniquement aux fonctionnalités normales du jeu.

---

# 11. Bannissement

Un ban MUST contenir :

- compte ciblé ;
- modérateur/admin auteur ;
- date début ;
- date fin ou permanent ;
- justification ;
- statut ;
- date de levée ;
- auteur de la levée si applicable.

La justification est obligatoire.

Une sanction ne doit jamais être supprimée de l’historique.

Un bannissement empêche l’authentification/jeu mais ne gèle pas automatiquement l’univers.

Les flottes déjà parties, productions et événements continuent normalement sauf décision administrative distincte.

Cette règle évite que le ban devienne un outil de protection d’empire.

---

# 12. Sécurité

## 12.1 Password hashing

Utiliser Argon2id.

Le format stocké doit inclure :

- algorithme ;
- version ;
- paramètres ;
- salt ;
- hash.

La politique doit permettre un rehash automatique lors d’un login si les paramètres évoluent.

Ne jamais utiliser SHA-256 seul pour un mot de passe.

## 12.2 Sessions

Sessions serveur opaques.

Cookie :

- HttpOnly ;
- SameSite ;
- Secure lorsque HTTPS ;
- durée limitée ;
- rotation à l’authentification ;
- invalidation au changement de mot de passe ;
- invalidation possible depuis l’admin.

Ne pas stocker d’autorisation complète dans un JWT client.

## 12.3 CSRF

Toutes les mutations HTTP doivent être protégées contre CSRF.

## 12.4 Login

Prévoir :

- rate limiting ;
- temporisation progressive ;
- journalisation des échecs ;
- messages ne révélant pas l’existence précise d’un compte ;
- invalidation des tokens sensibles après usage.

## 12.5 Réseau

Par défaut, écouter uniquement sur loopback si aucun paramètre explicite n’est fourni.

Si binding sur une adresse non-loopback sans TLS, afficher un avertissement explicite.

Le serveur doit pouvoir :

- servir HTTP local ;
- servir HTTPS si certificats configurés ;
- fonctionner derrière un reverse proxy sans le rendre obligatoire.

## 12.6 Headers

Utiliser des headers de sécurité cohérents :

- CSP restrictive ;
- nosniff ;
- frame restrictions ;
- referrer policy.

Aucun JavaScript inline non maîtrisé si la CSP l’interdit.

## 12.7 Audit

Toute action sensible doit produire une ligne d’audit append-only :

- login admin ;
- modification règles ;
- création rôle ;
- promotion ;
- bannissement ;
- débannissement ;
- pause univers ;
- manipulation maintenance ;
- reset password ;
- import de profil.

---

# 13. Univers

## 13.1 Coordonnées

Modèle conceptuel :

`galaxie:système:position`

Une position peut contenir :

- planète ;
- lune ;
- champ de débris.

La distance influence :

- durée de trajet ;
- consommation ;
- zones de chasse ;
- couverture phalange.

## 13.2 Génération planétaire

Une planète possède notamment :

- taille / cases ;
- température ;
- position ;
- bonus/malus dérivés ;
- ressources ;
- bâtiments ;
- défense ;
- flotte stationnée.

La position doit avoir un intérêt stratégique.

---

# 14. Économie

Ressources de base :

- métal ;
- cristal ;
- deutérium ;
- énergie comme capacité, pas nécessairement stock principal.

La profondeur doit venir des arbitrages, pas de dizaines de ressources.

Règles :

- production continue ;
- capacité de stockage ;
- production influencée par mines ;
- énergie disponible ;
- technologies/bonus ;
- règles d’univers.

Invariant :

**ressource stockée = ressource exposée au risque.**

Les coûts doivent croître exponentiellement ou selon le modèle classique configuré.

---

# 15. Bâtiments

Prévoir au minimum les familles :

- mines ;
- centrales ;
- stockage ;
- usine robotique ;
- nanites ;
- chantier spatial ;
- laboratoire ;
- dépôt/alliance si retenu ;
- silo à missiles ;
- terraformation ;
- bâtiments lunaires.

Le système MUST être générique :

- coût ;
- multiplicateur ;
- prérequis ;
- durée ;
- effet ;
- niveau maximal optionnel.

Ne pas disperser les formules dans les handlers.

---

# 16. Recherches

Les recherches forment un graphe de dépendances.

Prévoir les familles :

- énergie ;
- laser/ions/plasma ;
- espionnage ;
- ordinateur ;
- astrophysique/colonisation ;
- réseau de recherche ;
- armes ;
- bouclier ;
- protection/coque ;
- moteurs ;
- gravitation ou équivalent avancé.

Le moteur doit gérer :

- prérequis ;
- niveau ;
- durée ;
- effet ;
- laboratoire ;
- éventuel réseau multi-planètes.

---

# 17. Chantier spatial et défenses

Le chantier spatial produit des quantités d’unités.

Une commande de production :

- réserve/dépense les ressources atomiquement ;
- calcule sa durée ;
- possède une progression ;
- termine via événement.

Les défenses suivent une logique similaire.

Prévoir des unités configurables mais fournir un catalogue par défaut cohérent avec le gameplay classique :

- cargos ;
- chasseurs ;
- croiseurs ;
- vaisseaux lourds ;
- bombardement ;
- destructeurs ;
- unités spécialisées ;
- recycleurs ;
- sondes ;
- colonisateurs ;
- satellites.

---

# 18. Flottes

Une flotte est :

- un propriétaire ;
- une origine ;
- une destination ;
- une composition ;
- un cargo ;
- une mission ;
- une vitesse ;
- un départ ;
- une arrivée ;
- éventuellement un retour ;
- une version de règles ;
- un état.

Aucune conduite temps réel.

Une flotte partie est engagée.

Les actions possibles doivent être strictement contrôlées :

- rappeler lorsque la mission l’autorise ;
- rejoindre une opération ACS lorsque permis ;
- aucune téléportation arbitraire.

---

# 19. Missions

Prévoir au minimum :

- attaque ;
- transport ;
- stationnement ;
- espionnage ;
- colonisation ;
- recyclage ;
- expédition ;
- ACS attaque ;
- ACS défense / stationnement ;
- destruction de lune si activée ;
- missile interplanétaire si activé.

Chaque mission est un use case distinct ou une stratégie explicitement testée.

---

# 20. Fleetsave

Le fleetsave est un invariant de design.

Un joueur expérimenté doit pouvoir rendre sa flotte difficile à intercepter en choisissant :

- mission ;
- vitesse ;
- origine ;
- destination ;
- heure de retour ;
- lune ;
- modes de mission adaptés.

Les IA doivent savoir utiliser le fleetsave.

Une IA qui laisse systématiquement sa flotte stationnée hors de ses heures d’activité est considérée comme incorrecte.

---

# 21. Espionnage et information imparfaite

## 21.1 Vérité vs connaissance

Séparer explicitement :

### État réel

Ce que le serveur sait.

### Information joueur

Ce que le joueur a découvert.

Une UI joueur ne doit jamais obtenir des informations réelles non autorisées “puis les masquer en CSS”.

Le serveur ne doit pas envoyer l’information cachée.

## 21.2 Rapport

Un rapport d’espionnage :

- possède un timestamp ;
- référence une cible ;
- révèle un sous-ensemble d’informations ;
- devient progressivement obsolète.

Selon les technologies et paramètres :

- ressources ;
- flotte ;
- défense ;
- bâtiments ;
- recherches.

Le joueur doit distinguer :

- information récente ;
- information ancienne ;
- information inconnue.

## 21.3 IA

L’IA conserve sa propre base de connaissances.

Une IA ne peut prendre une décision qu’à partir :

- de son état ;
- de ses observations ;
- de ses rapports ;
- des informations partagées par son alliance ;
- d’informations publiques.

Jamais depuis une lecture omnisciente de la DB ennemie.

---

# 22. Combat

## 22.1 Moteur autonome

Le moteur de combat MUST être une fonction domaine indépendante du web et de SQLite.

Entrées :

- attaquants ;
- défenseurs ;
- défenses ;
- technologies ;
- règles ;
- seed déterministe.

Sorties :

- vainqueur / nul ;
- survivants ;
- pertes ;
- butin ;
- débris ;
- rounds ;
- statistiques détaillées ;
- données de rapport.

## 22.2 Principes

Conserver :

- rounds ;
- sélection de cibles ;
- armes ;
- bouclier ;
- coque ;
- rapid fire ;
- stochasticité contrôlée.

Ne jamais réduire un combat à :

`power(A) > power(B)`.

## 22.3 Déterminisme

Un combat doit être reproductible avec :

- même état ;
- même ruleset ;
- même seed.

Stocker la seed ou les informations permettant de rejouer le combat.

## 22.4 Tests

Créer :

- golden tests ;
- tests de petites flottes calculables ;
- tests de gros volumes ;
- fuzz tests ;
- invariants de conservation.

Exemples d’invariants :

- aucun nombre d’unité négatif ;
- survivants <= unités initiales ;
- débris >= 0 ;
- butin <= capacité disponible et limite de pillage ;
- résultat identique pour seed identique.

---

# 23. Champs de débris

Un combat peut créer un champ de débris.

Le champ contient :

- métal ;
- cristal ;
- position ;
- création ;
- quantité restante.

Plusieurs flottes peuvent tenter de recycler le même champ.

La résolution doit être atomique et ordonnée par heure d’arrivée.

Le recyclage est une mission, pas un bouton instantané.

---

# 24. Lunes

La lune est un objet distinct de la planète.

Elle peut :

- posséder des bâtiments ;
- accueillir une flotte ;
- servir au fleetsave ;
- accueillir phalange ;
- accueillir porte de saut.

La création dépend d’une règle configurable liée au combat/débris.

---

# 25. Phalange

La phalange est une mécanique de renseignement temporel.

Elle ne doit pas révéler arbitrairement toutes les flottes.

Définir précisément :

- cibles observables ;
- rayon ;
- coût ;
- missions visibles ;
- informations révélées ;
- exceptions liées aux lunes/missions.

L’UI doit permettre de transformer l’information obtenue en décision de timing.

---

# 26. Porte de saut

Permet un déplacement exceptionnel entre lunes compatibles.

Doit respecter :

- cooldown ;
- composition autorisée ;
- absence ou présence de cargo selon ruleset ;
- conditions de niveau ;
- destination autorisée.

Pas de carburant/trajet classique si les règles choisies reproduisent ce comportement.

---

# 27. Colonisation

Une colonisation :

- nécessite le prérequis ;
- nécessite un colonisateur ;
- respecte la limite de colonies ;
- cible une position vide ;
- se résout à l’arrivée ;
- échoue proprement si la position n’est plus valide selon les règles.

La création de planète doit être transactionnelle.

---

# 28. Alliances / équipes

Une alliance est la structure d’équipe principale.

Elle possède :

- nom ;
- tag ;
- description ;
- membres ;
- rôles internes ;
- invitations ;
- diplomatie ;
- historique.

Par défaut, une alliance ne fusionne pas automatiquement toutes les ressources.

Elle facilite :

- coordination ;
- renseignement ;
- attaques groupées ;
- défenses ;
- communication ;
- objectifs communs.

Le setup peut définir :

- alliances libres ;
- équipes imposées ;
- humains contre IA ;
- PvPvE ;
- free-for-all.

---

# 29. ACS / combat groupé

L’ACS doit permettre :

- création d’un groupe d’attaque ;
- invitation de membres autorisés ;
- ajout de flottes ;
- recalcul des contraintes temporelles ;
- retrait tant que permis ;
- résolution unique du combat ;
- rapport partagé selon règles.

Prévoir également la logique de défense groupée.

Tests indispensables :

- arrivée simultanée ;
- flotte ajoutée ralentissant le groupe ;
- rappel ;
- participant banni/déconnecté ;
- cible détruite ou modifiée ;
- combat multi-acteurs ;
- partage du butin/débris selon règles.

---

# 30. Expéditions

Les expéditions constituent un PvE secondaire.

Événements possibles, configurables :

- ressources ;
- vaisseaux ;
- retard ;
- rien ;
- pirates ;
- aliens ;
- pertes ;
- événements rares.

La table de résultats doit être configurable et probabiliste.

La RNG doit être déterministe à partir d’une seed enregistrable.

Les expéditions ne doivent pas remplacer l’interaction entre joueurs/IA.

---

# 31. Architecture de l’IA

## 31.1 Trois niveaux

### Stratégique

Horizon : heures/jours.

Décide :

- économie vs militaire ;
- colonisation ;
- technologie ;
- doctrine ;
- expansion ;
- diplomatie ;
- menace prioritaire.

### Opérationnel

Horizon : minutes/heures.

Décide :

- qui espionner ;
- quelles cibles surveiller ;
- quand fleetsaver ;
- comment préparer une opération ;
- quand demander du soutien.

### Tactique

Décide :

- composition d’une flotte ;
- vitesse ;
- cargo ;
- recycleurs ;
- rappel ;
- participation ACS.

## 31.2 Archétypes

Fournir plusieurs profils :

- mineur prudent ;
- raider ;
- fleeter ;
- tortue ;
- opportuniste ;
- éclaireur ;
- logisticien ;
- défenseur d’alliance.

Un archétype est un ensemble de préférences, pas un script rigide.

## 31.3 Pas de triche

L’IA MUST :

- payer ses ressources ;
- attendre ses constructions ;
- attendre ses voyages ;
- sonder ;
- consommer son deutérium ;
- perdre ses flottes ;
- respecter les files ;
- respecter les technologies ;
- respecter la phalange ;
- respecter les cooldowns.

## 31.4 Erreur et incertitude

Une IA peut :

- se tromper ;
- agir sur un rapport ancien ;
- surestimer une cible ;
- rater une fenêtre ;
- rappeler par prudence ;
- refuser une opération rentable si son profil est prudent.

Une IA parfaite est indésirable.

## 31.5 Horaires d’activité

Chaque IA possède des plages d’activité et des variations.

En dehors :

- production continue ;
- événements continuent ;
- décisions nouvelles fortement réduites ;
- réaction instantanée interdite sauf mécanisme explicitement prévu.

Cela rend les comportements observables et exploitables.

---

# 32. IA d’alliance

Une alliance IA doit avoir une mémoire partagée légitime :

- rapports partagés ;
- menaces ;
- opportunités ;
- champs de débris ;
- ennemis surveillés ;
- horaires supposés ;
- opérations planifiées.

Les membres peuvent avoir des rôles :

- scout ;
- fleeter ;
- mineur ;
- recycleur ;
- défenseur.

Une alliance IA peut :

- coordonner un ACS ;
- défendre un membre ;
- lancer une campagne d’espionnage ;
- réagir à une menace ;
- changer de priorité stratégique.

Elle ne reçoit aucune omniscience supplémentaire.

---

# 33. Boucle de simulation

## 33.1 Scheduled events

Chaque événement planifié possède au minimum :

- id ;
- type ;
- due_at ;
- entity/reference ;
- ruleset version ou snapshot nécessaire ;
- payload versionné si nécessaire ;
- état ;
- idempotency key ou garantie équivalente.

## 33.2 Traitement

Le worker :

1. trouve le prochain événement dû ;
2. ouvre une transaction d’écriture ;
3. relit l’état nécessaire ;
4. vérifie que l’événement est encore valide ;
5. applique le domaine ;
6. persiste les mutations ;
7. ajoute les rapports/notifications ;
8. planifie les événements suivants ;
9. marque l’événement terminé ;
10. commit.

Si le processus crash avant commit, le traitement doit pouvoir reprendre sans duplication.

## 33.3 Ordre

Pour deux événements au même timestamp, définir un ordre stable.

Exemple :

- `due_at`
- puis priorité de type si nécessaire
- puis ID monotone.

Documenter cette règle.

## 33.4 Wake-up

Le worker ne doit pas boucler activement.

Il doit :

- dormir jusqu’au prochain événement ;
- pouvoir être réveillé lorsqu’un nouvel événement plus proche est inséré ;
- avoir un mécanisme de rescan périodique de sécurité.

---

# 34. Horloge de l’univers

Prévoir une abstraction de temps.

Le domaine ne doit jamais appeler directement `time.Now()`.

Utiliser une interface d’horloge afin de :

- tester ;
- avancer le temps ;
- simuler ;
- figer ;
- reproduire des scénarios.

Modes optionnels configurables :

- temps continu ;
- univers en pause administrative ;
- plages d’activité globales ;
- pause lorsque personne n’est connecté, si explicitement activée.

Tout mode de pause doit être clairement visible aux joueurs.

---

# 35. Rapports et notifications

Les rapports sont le journal narratif du jeu.

Types :

- espionnage ;
- attaque ;
- défense ;
- recyclage ;
- expédition ;
- colonisation ;
- phalange ;
- système ;
- alliance ;
- modération.

Un rapport doit :

- être immuable ;
- être horodaté ;
- contenir uniquement ce que le destinataire a le droit de voir ;
- rester consultable ;
- pouvoir être partagé avec l’alliance selon règles.

L’UI doit rendre les événements hostiles immédiatement visibles.

---

# 36. Front-end

## 36.1 Philosophie

Le front doit être :

- beau ;
- sombre/spatial ;
- très lisible ;
- rapide ;
- responsive ;
- utilisable clavier/souris ;
- agréable sur tablette ;
- raisonnablement utilisable sur mobile ;
- très léger.

Aucune SPA.

Le HTML est rendu serveur.

## 36.2 JavaScript

Principe :

**Le jeu doit rester fonctionnel avec très peu de JavaScript.**

JavaScript autorisé pour :

- countdowns ;
- notifications ;
- mise à jour légère d’éléments ;
- confirmations ergonomiques ;
- auto-refresh ciblé ;
- SSE ;
- menus/accessibilité nécessitant interaction.

JavaScript interdit pour :

- logique de combat ;
- validation d’autorisation ;
- calcul authoritative des ressources ;
- règles métier ;
- état authoritative des flottes.

Préférer du JavaScript vanilla.

Aucune dépendance front importante sans justification.

## 36.3 Mise à jour temps réel

Préférence :

- timestamps absolus rendus par serveur ;
- countdown client dérivé ;
- Server-Sent Events pour signaler qu’une zone doit être rafraîchie ;
- fetch ciblé ou fragment HTML ;
- fallback polling lent si SSE indisponible.

Ne pas envoyer un tick serveur chaque seconde.

## 36.4 CSS

Préférer :

- CSS natif ;
- custom properties ;
- Grid ;
- Flexbox ;
- container queries si utiles ;
- design tokens ;
- composants simples.

Pas de framework CSS lourd obligatoire.

Aucun pipeline Node requis pour exécuter ou compiler la version distribuée.

## 36.5 Assets

Embarquer :

- CSS ;
- JS ;
- SVG ;
- fonts uniquement si licence adaptée et nécessité réelle ;
- images optimisées.

Favoriser SVG et formats modernes.

---

# 37. Direction visuelle

Ambiance :

- espace sombre ;
- interface de centre de commandement ;
- panneaux structurés ;
- accent lumineux modéré ;
- chiffres et timings très lisibles ;
- alertes hostiles fortes ;
- illustrations planétaires originales ;
- vaisseaux industriels ;
- densité d’information maîtrisée.

Éviter :

- glassmorphism excessif ;
- animations permanentes ;
- effets 3D lourds ;
- vidéo de fond ;
- canvas WebGL décoratif ;
- surcharges sonores.

Objectif :

**une interface que l’on peut laisser ouverte pendant des heures.**

---

# 38. Écrans joueur obligatoires

## Vue globale

- empire ;
- ressources ;
- production ;
- files ;
- flottes ;
- événements proches.

## Planète

- ressources ;
- bâtiments ;
- énergie ;
- file.

## Recherche

- arbre / liste ;
- prérequis ;
- temps ;
- coûts.

## Chantier

- vaisseaux ;
- quantité ;
- coût ;
- temps.

## Défense

- unités ;
- quantité ;
- coût.

## Flottes

- stationnées ;
- en vol ;
- heure arrivée ;
- heure retour ;
- mission ;
- rappel si possible.

## Envoi de flotte

Workflow clair :

1. composition ;
2. destination ;
3. mission ;
4. vitesse ;
5. cargo ;
6. estimation consommation ;
7. heure arrivée ;
8. heure retour ;
9. confirmation.

## Galaxie

Écran stratégique central :

- coordonnées ;
- planètes ;
- lunes ;
- propriétaires ;
- alliances ;
- activité ;
- débris ;
- actions rapides.

## Espionnage

- historique ;
- fraîcheur ;
- comparaison possible ;
- informations inconnues clairement distinguées.

## Rapports

- filtres ;
- catégories ;
- lu/non lu ;
- partage.

## Alliance

- membres ;
- présence/statut public ;
- invitations ;
- rapports partagés ;
- opérations.

---

# 39. Administration

Le panneau admin est un produit à part entière.

## Dashboard

Afficher :

- état serveur ;
- version ;
- uptime ;
- DB ;
- taille DB/WAL ;
- prochain événement ;
- backlog ;
- événements/min ;
- joueurs actifs ;
- IA actives ;
- erreurs récentes ;
- état des backups ;
- ruleset actif.

## Univers

Sections fines :

- topologie ;
- économie ;
- construction ;
- recherche ;
- flottes ;
- combat ;
- espionnage ;
- débris ;
- lunes ;
- phalange ;
- portes ;
- expéditions ;
- alliances ;
- IA ;
- joueurs ;
- protection ;
- horaires.

## Configuration UX

Pour chaque valeur :

- nom ;
- description ;
- unité ;
- valeur actuelle ;
- valeur par défaut ;
- min/max ;
- classe de modification ;
- impact ;
- validation.

Prévoir :

- recherche ;
- catégories ;
- diff ;
- reset valeur ;
- reset section ;
- import/export ;
- prévisualisation.

## Joueurs

Admin peut :

- rechercher ;
- voir rôle ;
- statut ;
- date création ;
- dernière connexion ;
- sanctions ;
- activer/désactiver ;
- réinitialiser password ;
- promouvoir/rétrograder ;
- supprimer selon politique.

Toute intervention directe sur l’état de jeu doit être exceptionnelle, auditée et séparée des outils ordinaires.

---

# 40. Modération

Interface dédiée plus simple.

Le modérateur voit :

- joueurs ;
- recherche ;
- sanctions ;
- bannissement ;
- débannissement ;
- historique.

Le formulaire de ban exige :

- durée ;
- justification.

Le modérateur ne voit pas les panneaux de configuration.

---

# 41. Maintenance

Le binaire SHOULD proposer des sous-commandes :

- serve
- migrate
- doctor
- backup
- admin reset-password
- version

## Doctor

Vérifie :

- ouverture DB ;
- foreign keys ;
- intégrité ;
- migrations ;
- écriture ;
- WAL ;
- répertoire ;
- permissions ;
- cohérence minimale du ruleset.

## Backup

Le backup doit être cohérent avec SQLite.

Le serveur ne doit jamais faire une simple copie naïve du fichier principal pendant des écritures sans tenir compte du WAL.

Prévoir :

- backup depuis CLI ;
- backup depuis admin ;
- nom horodaté ;
- vérification ;
- politique de rétention optionnelle.

---

# 42. Logging et observabilité

Utiliser le logging structuré.

Chaque log SHOULD inclure lorsque pertinent :

- request id ;
- account id ;
- player id ;
- event id ;
- mission id ;
- universe id ;
- durée ;
- erreur.

Ne jamais logger :

- mot de passe ;
- session token ;
- CSRF secret ;
- hash complet inutilement ;
- données sensibles non nécessaires.

Prévoir des métriques internes accessibles à l’admin sans nécessiter Prometheus.

---

# 43. TDD — stratégie détaillée

## 43.1 Pyramide

### Niveau 1 — domaine

Majoritaire.

Tests rapides sans DB.

### Niveau 2 — application

Use cases avec fakes minimalistes ou repositories de test.

### Niveau 3 — SQLite

Tests contre vraie DB SQLite temporaire utilisant :

- mêmes migrations ;
- même driver ;
- mêmes PRAGMA ;
- mêmes transactions.

### Niveau 4 — HTTP

`httptest` :

- auth ;
- permissions ;
- CSRF ;
- formulaires ;
- status ;
- redirects ;
- fragments.

### Niveau 5 — navigateur

Petit nombre de tests E2E critiques :

- bootstrap ;
- setup ;
- login ;
- construction ;
- envoi de flotte ;
- réception rapport ;
- ban.

## 43.2 Tests table-driven

Utiliser largement les tests table-driven Go pour :

- coûts ;
- durées ;
- distances ;
- production ;
- espionnage ;
- combat ;
- autorisations.

## 43.3 Fuzzing

Fuzz tests sur :

- parsing coordonnées ;
- formules ;
- combat ;
- sérialisation événements ;
- import de profils ;
- inputs HTTP sensibles.

## 43.4 Property tests / invariants

Tester :

- ressources jamais négatives après commit ;
- quantité de vaisseaux cohérente ;
- même seed = même résultat ;
- événement ne se résout qu’une fois ;
- un joueur ne peut envoyer une flotte inexistante ;
- aucune position ne contient deux planètes ;
- une technologie ne démarre pas sans prérequis ;
- une construction ne démarre pas sans ressources ;
- un ban bloque l’accès ;
- un modérateur ne peut modifier un ruleset ;
- un joueur ne peut obtenir une information cachée via endpoint.

## 43.5 Fake clock

Tous les tests temporels utilisent une fake clock.

Aucun `sleep` long dans les tests.

---

# 44. Tests de concurrence

Cas obligatoires :

- deux requêtes tentent de dépenser les mêmes ressources ;
- deux colonisations arrivent sur la même position ;
- deux recycleurs arrivent au même moment ;
- événement et action utilisateur ciblent la même flotte ;
- rappel pendant arrivée ;
- double submit formulaire ;
- double traitement d’événement après crash simulé ;
- deux invitations/alliance concurrentes.

Le résultat doit être déterministe ou respecter une règle d’arbitrage documentée.

---

# 45. Idempotence HTTP

Les opérations importantes SHOULD utiliser un mécanisme empêchant le double submit :

- launch fleet ;
- construction coûteuse ;
- création alliance ;
- changement de ruleset ;
- ban.

Un refresh navigateur ne doit pas dupliquer une action.

Utiliser PRG (Post/Redirect/Get) et, lorsque nécessaire, une clé d’idempotence.

---

# 46. Performance

Le jeu vise une machine locale normale.

Objectifs :

- navigation ordinaire perçue instantanée ;
- aucune boucle CPU permanente ;
- faible mémoire au repos ;
- gestion de milliers d’événements planifiés ;
- combats massifs sans bloquer durablement le serveur.

Créer des benchmarks pour :

- production lazy ;
- sélection prochain événement ;
- insertion événement ;
- combat ;
- page galaxie ;
- IA think cycle.

L’optimisation ne doit jamais compromettre la correction.

---

# 47. Transaction boundaries

Toute action métier doit être atomique.

Exemple lancement flotte conceptuel :

Dans UNE transaction :

- vérifier joueur ;
- vérifier planète ;
- régler ressources ;
- vérifier vaisseaux ;
- vérifier deutérium ;
- calculer durée ;
- retirer vaisseaux ;
- retirer cargo/carburant ;
- créer fleet mission ;
- créer event arrivée ;
- écrire audit/game event si nécessaire ;
- commit.

Jamais :

1. retirer ressources ;
2. commit ;
3. créer flotte.

---

# 48. Event log

Conserver un journal append-only d’événements significatifs différent des tables d’état.

Exemples :

- building_started ;
- building_completed ;
- fleet_launched ;
- fleet_recalled ;
- combat_resolved ;
- moon_created ;
- ban_created ;
- ruleset_changed.

Ce n’est pas nécessairement un event sourcing complet.

L’état relationnel reste authoritative.

Le journal sert :

- debug ;
- audit ;
- replay partiel ;
- diagnostics ;
- narration éventuelle.

---

# 49. RNG

Centraliser le hasard.

Interdiction d’appeler arbitrairement une RNG globale dans le domaine.

Chaque résolution probabiliste reçoit une source contrôlée ou seed :

- combat ;
- lune ;
- expédition ;
- certaines décisions IA.

But :

- reproductibilité ;
- tests ;
- audit.

---

# 50. Validation des règles

Un ruleset impossible ne doit pas pouvoir être activé.

Exemples :

- vitesse <= 0 ;
- stockage négatif ;
- chance > 100 % ;
- coût négatif ;
- boucle impossible de prérequis ;
- technologie sans identifiant ;
- rapid fire invalide ;
- taille d’alliance négative ;
- planète de départ impossible.

Créer un validateur complet de ruleset.

Le wizard et l’admin utilisent exactement le même validateur.

---

# 51. Catalogue de contenu

Séparer le contenu des algorithmes.

Le catalogue décrit :

- bâtiments ;
- recherches ;
- vaisseaux ;
- défenses ;
- missions ;
- prérequis ;
- coûts ;
- effets ;
- rapid fire ;
- expéditions.

Les valeurs classiques par défaut doivent être regroupées dans un endroit versionné.

Éviter les magic numbers disséminés.

Toute formule non triviale doit être :

- nommée ;
- documentée ;
- testée.

---

# 52. Compatibilité des sauvegardes

Chaque DB conserve :

- schema version ;
- application version de création ;
- application version dernière ouverture ;
- ruleset version.

Le serveur doit refuser proprement d’ouvrir une DB provenant d’une version future incompatible.

Ne jamais corrompre silencieusement une sauvegarde.

---

# 53. Gestion d’erreur

Distinguer :

- erreur utilisateur ;
- conflit métier ;
- unauthorized ;
- forbidden ;
- not found ;
- validation ;
- DB temporairement occupée ;
- erreur interne.

L’UI doit afficher un message utile sans exposer stack trace ni SQL.

Les erreurs internes obtiennent un identifiant corrélable aux logs.

---

# 54. Accessibilité

Le front MUST :

- conserver un contraste suffisant ;
- ne pas utiliser uniquement la couleur pour signaler hostile/allié ;
- avoir labels de formulaires ;
- focus visible ;
- navigation clavier ;
- boutons sémantiques ;
- tableaux utilisables ;
- `aria` uniquement lorsque nécessaire.

Les countdowns ne doivent pas provoquer des annonces screen-reader chaque seconde.

---

# 55. Internationalisation

Même si la V1 peut être en français :

- ne pas disperser les textes dans le domaine ;
- séparer messages UI et données ;
- prévoir formatage dates/nombres ;
- stocker UTC ;
- ne pas stocker des libellés traduits comme identifiants.

---

# 56. Politique de registration

Configurable :

- fermée ;
- ouverte ;
- sur invitation.

Création joueur :

- username unique ;
- password robuste ;
- planète de départ ;
- position attribuée selon stratégie ;
- protections initiales ;
- aucun privilège implicite.

Limiter les créations abusives même en local.

---

# 57. États du serveur

Machine d’état explicite :

- `BOOTSTRAP_PENDING`
- `SETUP_IN_PROGRESS`
- `RUNNING`
- `PAUSED`
- `MAINTENANCE`

Transitions contrôlées et auditables.

Un handler HTTP ne doit pas improviser le comportement selon quelques booléens indépendants.

---

# 58. États de flotte

Machine d’état explicite.

Exemple conceptuel :

- prepared
- outbound
- holding
- resolving
- returning
- completed
- recalled
- destroyed

Toutes les transitions sont validées.

Une flotte ne peut être simultanément “returning” et stationnée.

---

# 59. Définitions de fidélité

Avant d’implémenter chaque grand système, l’agent MUST produire dans `/docs/rules/` une courte fiche contenant :

- comportement attendu ;
- formule ;
- constantes ;
- exemples ;
- cas limites ;
- tests de référence.

Documents minimum :

- economy.md
- distances.md
- building-costs.md
- research.md
- fleet-speed.md
- fuel.md
- espionage.md
- combat.md
- debris.md
- moon.md
- phalanx.md
- acs.md
- expeditions.md

Ne pas implémenter une approximation silencieuse.

Si une règle exacte est incertaine :

1. documenter l’incertitude ;
2. choisir une règle explicite ;
3. la rendre configurable ;
4. écrire les tests correspondant au choix.

---

# 60. IA — critères d’acceptation

Une IA est acceptable si elle peut :

- développer son économie ;
- atteindre des technologies avancées ;
- coloniser ;
- construire une flotte ;
- espionner avant une attaque significative ;
- calculer approximativement une rentabilité ;
- adapter cargo/recycleurs ;
- fleetsaver ;
- perdre une bataille ;
- apprendre d’un rapport récent ;
- agir différemment selon son archétype ;
- dormir ;
- coordonner au moins une attaque groupée ;
- partager des informations légitimes ;
- défendre un allié lorsqu’elle en a les moyens.

Une IA est refusée si elle :

- connaît sans espionnage la flotte adverse ;
- crée des ressources ;
- instant-build ;
- rappelle après l’instant où un humain ne pourrait plus le faire ;
- réagit 24/7 à chaque action ;
- reçoit des bonus secrets non configurés.

Les bonus de difficulté, s’ils existent, doivent être :

- visibles dans le profil d’univers ;
- optionnels ;
- explicites.

Préférer une meilleure décision à un bonus artificiel.

---

# 61. Administration IA

L’admin peut :

- ajouter IA ;
- retirer IA selon règles ;
- affecter archétype ;
- affecter alliance ;
- changer agressivité ;
- changer horizon stratégique ;
- changer plages d’activité ;
- voir l’état de santé IA ;
- voir sa prochaine réflexion ;
- voir ses objectifs techniques.

Deux modes de vue :

### Vue normale

Ne montre que ce qu’un joueur aurait le droit de voir.

### Debug admin

Peut montrer état interne/intentions IA, clairement marqué comme information omnisciente et inaccessible aux joueurs.

---

# 62. Outils de debug développement

En mode développement uniquement :

- avancer fake clock ;
- traiter prochain événement ;
- créer scénario ;
- seed RNG connue ;
- inspecter queue ;
- rejouer combat ;
- générer empire de test.

Ces outils MUST être absents ou strictement protégés en build normal.

---

# 63. Scénarios de test fonctionnels majeurs

## Scénario A — Premier boot

- DB absente ;
- serveur crée DB ;
- admin généré ;
- secret affiché une fois ;
- joueur impossible ;
- admin login ;
- changement password ;
- wizard ;
- validation ;
- serveur running.

## Scénario B — Économie

- planète produit ;
- serveur arrêté 2h ;
- redémarrage ;
- production correcte sans ticks manqués.

## Scénario C — Construction

- ressources disponibles ;
- lancement ;
- coût atomic ;
- temps avancé ;
- event ;
- niveau augmenté une seule fois.

## Scénario D — Flotte

- création composition ;
- calcul carburant ;
- départ ;
- ressources/vaisseaux retirés ;
- arrivée ;
- retour ;
- inventaire cohérent.

## Scénario E — Attaque

- espionnage ;
- attaque ;
- combat ;
- pertes ;
- butin ;
- débris ;
- rapport ;
- retour.

## Scénario F — Crash

- event dû ;
- crash simulé avant commit ;
- restart ;
- event traité une fois.

## Scénario G — ACS

- deux joueurs ;
- attaque commune ;
- timing ;
- résolution ;
- rapports cohérents.

## Scénario H — IA

- IA offline ;
- flotte vulnérable selon stratégie ;
- retour activité ;
- analyse ;
- réaction légitime.

## Scénario I — Modération

- modérateur ban ;
- justification obligatoire ;
- joueur bloqué ;
- empire continue ;
- admin voit audit.

## Scénario J — Changement ruleset

- flotte déjà en vol ;
- admin change vitesse ;
- nouvelle flotte utilise nouvelle règle ;
- ancienne flotte conserve timing.

---

# 64. Livraison incrémentale

Ne pas tenter de construire tout en parallèle.

## Milestone 0 — Foundation

- repo ;
- CI ;
- configuration ;
- SQLite ;
- migrations ;
- logging ;
- clock ;
- TDD harness.

Critère : DB vide -> boot reproductible.

## Milestone 1 — Auth/bootstrap/admin setup

- accounts ;
- roles ;
- Argon2id ;
- sessions ;
- bootstrap admin ;
- setup wizard ;
- server states ;
- audit.

Critère : serveur inutilisable avant setup.

## Milestone 2 — Universe/economy

- coordonnées ;
- planète ;
- ressources ;
- production lazy ;
- bâtiments ;
- queue.

Critère : progression économique jouable.

## Milestone 3 — Research/shipyard

- recherches ;
- prérequis ;
- vaisseaux ;
- défenses.

## Milestone 4 — Fleet engine

- distance ;
- vitesse ;
- carburant ;
- mission ;
- event queue ;
- retour ;
- rappel.

## Milestone 5 — Espionage/combat

- rapports ;
- combat ;
- pillage ;
- débris ;
- recycleurs.

## Milestone 6 — Expansion

- colonisation ;
- lunes ;
- phalange ;
- jump gate.

## Milestone 7 — Alliances/ACS

- équipes ;
- invitations ;
- group attacks ;
- shared reports.

## Milestone 8 — AI individual

- activity ;
- economic planner ;
- fleetsave ;
- espionage ;
- raiding.

## Milestone 9 — AI alliances

- shared intelligence ;
- coordination ;
- defense ;
- ACS.

## Milestone 10 — Expeditions/polish

- expéditions ;
- profils ;
- UX ;
- optimisation ;
- accessibilité ;
- backup.

Chaque milestone MUST se terminer avec une version jouable.

---

# 65. Definition of Done d’une fonctionnalité

Une feature n’est terminée que si :

- comportement documenté ;
- tests rouges écrits ;
- tests verts ;
- erreurs gérées ;
- permissions testées ;
- transaction définie ;
- audit si nécessaire ;
- UI utilisable ;
- mobile raisonnable ;
- pas de régression ;
- docs mises à jour ;
- aucune TODO bloquante ;
- aucune donnée cachée envoyée au client ;
- `go test ./...` vert.

---

# 66. Règles pour l’agent LLM

L’agent MUST suivre ces règles pendant toute l’implémentation.

## 66.1 Avant de coder

Pour chaque milestone :

1. relire cette spécification ;
2. identifier les invariants ;
3. proposer les interfaces domaine ;
4. écrire les tests ;
5. seulement ensuite implémenter.

## 66.2 Pas de raccourci

Interdiction de remplacer une feature par :

- TODO ;
- mock permanent ;
- endpoint factice ;
- timer JavaScript authoritative ;
- cron externe ;
- DB mémoire en production ;
- JSON global à la place du schéma relationnel ;
- admin sans permission réelle ;
- IA omnisciente.

## 66.3 Dépendances

Avant d’ajouter une dépendance :

- vérifier si stdlib suffit ;
- justifier ;
- choisir une bibliothèque maintenue ;
- minimiser la surface ;
- vérifier licence ;
- verrouiller version.

Le runtime final ne nécessite aucun package manager.

## 66.4 Commits

Préférer des changements petits et cohérents.

Chaque tranche TDD doit laisser le projet compilable et les tests verts.

## 66.5 Simplicité

Ne pas créer une interface Go pour chaque struct “par principe”.

Introduire une abstraction lorsqu’elle :

- protège le domaine ;
- permet un test utile ;
- sépare une infrastructure ;
- représente réellement plusieurs implémentations.

Éviter l’architecture cérémonielle.

---

# 67. Décisions techniques préférées

Sauf raison documentée :

- `net/http` pour HTTP ;
- `html/template` ou moteur Go compilé léger ;
- `database/sql` ;
- `modernc.org/sqlite` ;
- `embed` pour assets ;
- `slog` pour logs structurés ;
- Argon2id pour mots de passe ;
- crypto/rand pour secrets ;
- HTML server-side ;
- CSS natif ;
- JS vanilla ;
- SSE uniquement si bénéfique ;
- UTC en stockage.

Le choix d’un petit router HTTP est acceptable s’il réduit clairement le code sans imposer un framework lourd.

---

# 68. Ce qui ne doit PAS être construit

Ne pas ajouter sans demande explicite :

- marketplace ;
- paiement ;
- microtransactions ;
- dark matter payante ;
- publicité ;
- analytics cloud ;
- OAuth obligatoire ;
- chat externe ;
- Discord obligatoire ;
- Kubernetes ;
- Docker obligatoire ;
- Redis ;
- queue externe ;
- websocket pour chaque timer ;
- rendu 3D ;
- combat temps réel ;
- application mobile native.

Docker peut être fourni comme commodité, jamais comme prérequis.

---

# 69. Critères de réussite globaux

Le produit final est réussi si :

1. un utilisateur télécharge un binaire ;
2. il lance le serveur ;
3. une DB SQLite est créée ;
4. un mot de passe admin fort est généré ;
5. il se connecte ;
6. il configure son univers avec finesse ;
7. il démarre l’univers ;
8. des joueurs créent leurs empires ;
9. des IA progressent selon les mêmes règles ;
10. joueurs et IA peuvent former des alliances ;
11. l’espionnage repose sur de vraies informations partielles ;
12. les flottes ont de vrais timings ;
13. le fleetsave fonctionne ;
14. les combats sont reproductibles techniquement mais incertains pour le joueur ;
15. les débris sont disputables ;
16. les lunes changent le jeu ;
17. la phalange permet de créer des interceptions ;
18. l’ACS permet des opérations coordonnées ;
19. l’administration peut modifier finement le ruleset ;
20. les modifications sont auditables et versionnées ;
21. le serveur survit correctement à un crash/restart ;
22. aucune infrastructure externe n’est nécessaire ;
23. le front reste rapide et léger ;
24. la suite de tests protège réellement les invariants.

---

# 70. Test ultime de design

Après plusieurs jours de jeu, les joueurs doivent pouvoir raconter des histoires comme :

> “L’alliance adverse espionnait toujours nos lunes vers 22h. On a volontairement laissé une petite flotte visible. Leur fleeter a mordu, puis une deuxième IA a rejoint l’attaque. On avait préparé une défense groupée depuis une autre lune. Ils ont rappelé une partie de l’opération, mais trop tard pour l’un des groupes. Le champ de débris était assez gros pour déclencher une course aux recycleurs.”

Si de telles histoires émergent spontanément des règles, du renseignement et des timings, le projet est sur la bonne voie.

---

# 71. Priorité absolue

En cas de conflit entre plusieurs objectifs, appliquer cet ordre :

1. **correction du domaine** ;
2. **intégrité des données** ;
3. **sécurité** ;
4. **fidélité gameplay** ;
5. **déterminisme/testabilité** ;
6. **ergonomie** ;
7. **performance** ;
8. **esthétique** ;
9. **confort développeur**.

Ne jamais sacrifier les six premiers pour une animation ou une abstraction élégante.

---

# 72. Première tâche de l’agent

Avant toute implémentation fonctionnelle, l’agent doit produire :

1. l’arborescence cible ;
2. les bounded modules ;
3. la machine d’état serveur ;
4. le modèle transactionnel ;
5. le modèle scheduled events ;
6. le schéma initial comptes/rôles/bootstrap ;
7. la stratégie de migrations ;
8. la stratégie d’horloge testable ;
9. la stratégie TDD ;
10. les premiers tests d’acceptation du bootstrap.

Ensuite seulement commencer Milestone 0 puis Milestone 1.

**Ne pas démarrer par le CSS, la galaxie ou le moteur de combat.**

---

# Fin de la spécification

Ce document doit rester vivant.

Toute décision qui modifie un invariant majeur doit :

- être documentée ;
- avoir une justification ;
- mettre à jour les tests ;
- mettre à jour cette spécification ou un ADR associé.

La règle générale est simple :

> **Le serveur connaît la vérité.  
> Le joueur ne connaît que ce qu’il a appris.  
> L’IA joue selon les mêmes lois.  
> Le temps et les transactions donnent leur poids aux décisions.**
