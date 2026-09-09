# Universe At War

Universe At War est un jeu de stratégie spatiale persistant, local et autonome,
inspiré des mécaniques temporelles d'OGame. Le serveur est développé en Go et
utilise SQLite sans service externe.

Le socle et les deux premiers milestones fournissent les migrations SQLite
embarquées, le bootstrap administrateur, l'authentification, les sessions
opaques, l'assistant de configuration versionné et une progression économique
jouable. Après le démarrage de l'univers, un compte peut fonder son empire,
produire des ressources hors ligne et construire les premiers bâtiments depuis
l'interface SSR. La spécification complète se trouve dans
[`SPECIFICATION_OGAME_LOCAL_GO.md`](SPECIFICATION_OGAME_LOCAL_GO.md).

## Prérequis de développement

- Go 1.27.x ou plus récent ;
- aucune installation SQLite ou CGO ;
- ImageMagick et libwebp uniquement pour regénérer les illustrations
  (`make art`) : ni la compilation ni les tests n'en ont besoin.

## Commandes disponibles

```sh
go run ./cmd/universe-at-war serve
go run ./cmd/universe-at-war migrate --database universe-at-war.db
go run ./cmd/universe-at-war doctor --database universe-at-war.db
go run ./cmd/universe-at-war backup --database universe-at-war.db --keep 14
go run ./cmd/universe-at-war admin reset-password --database universe-at-war.db --username admin
go run ./cmd/universe-at-war version
```

Au premier `serve`, le terminal affiche une seule fois l'identifiant et le mot
de passe aléatoire de l'administrateur initial. Après connexion sur
`http://127.0.0.1:8080`, ce mot de passe doit être remplacé avant de parcourir
les dix étapes de création de l'univers.

Si la politique d'inscription de l'univers vaut `open`, la page de connexion
propose de créer un compte joueur ; sinon le parcours reste totalement fermé.

Une fois l'univers lancé, la page principale liste les corps de l'empire et
propose de fonder un empire si le compte n'en a pas encore. Chaque planète a sa
page de bâtiments, une page de recherche, un chantier spatial et une page de
défense. Chacune tient une file d'attente : jusqu'à dix ordres par type, qui
avancent en parallèle d'un type à l'autre. Le coût est débité à la commande, si
bien qu'un ordre entré dans la file ne peut jamais caler faute de ressources ;
seule sa durée est décidée quand vient son tour. L'ordre en cours affiche une
barre de progression animée et un compte à rebours, ceux qui attendent affichent
la prévision de leur tour, et n'importe lequel s'annule avec remboursement
intégral. Achèvement par événement planifié et reprise après redémarrage restent
les mêmes que pour tout le reste du jeu.

Au chantier et à la défense, une carte déplie ses caractéristiques dès qu'on la
survole, ou que le clavier s'y pose : arme, bouclier, coque, fret, vitesse et
consommation, puis les feux rapides dans les deux sens — ce que l'unité
déchiquette et ce qui la déchiquette. La vitesse affichée est celle que donnent
les propulsions déjà recherchées, pas celle du catalogue. Une défense n'inflige
aucun feu rapide : sa carte ne montre donc que ses prédateurs.

La page Flotte liste les vaisseaux stationnés, les emplacements disponibles et
les missions en vol avec leurs horaires absolus. L'assistant d'envoi calcule
distance, carburant, capacité, arrivée et retour avant confirmation. Une flotte
partie est engagée : seul le rappel, tant que la mission le permet, la fait
revenir.

La carte galaxie n'affiche que le public : noms de planètes, joueurs et champs
de débris. Elle propose d'espionner en un clic ou de préparer une flotte. Les
rapports d'espionnage, de combat et de recyclage sont immuables et filtrés à
leur création : une section non révélée n'est pas envoyée au navigateur. Les
rapports hostiles non lus sont signalés par un compteur et un texte, jamais par
la seule couleur.

L'empire s'étend : une mission de colonisation fonde une planète si la position
est encore libre à l'arrivée, un combat assez destructeur peut agréger une lune,
et chaque lune a ses propres cases, bâtiments et vaisseaux. Une phalange de
capteurs donne les horaires des flottes alentour sans jamais révéler leur
composition, et une porte de saut transfère des vaisseaux entre deux lunes avec
un temps de recharge durable. La
planète mère affiche les productions, stockages, énergie et cases, ainsi que le
catalogue de bâtiments. Les constructions et leurs échéances sont durables : le
worker reprend automatiquement les événements après un redémarrage.

L'interface est un centre de commandement en trois colonnes : la navigation à
gauche, la barre des ressources du corps courant en haut, la liste des corps de
l'empire à droite avec leurs stocks et les totaux. Un stock arrivé à sa capacité
passe en alerte, car il ne produit plus.

Les compteurs ne restent pas figés entre deux chargements : le navigateur
prolonge chaque stock au taux que la page affiche déjà, de la barre du haut aux
totaux, et l'alerte de capacité s'allume à la seconde où le plafond est atteint.
C'est une estimation et rien de plus — un débit venu d'ailleurs n'est réglé
qu'au chargement suivant, que la page réclame d'elle-même dès qu'un compte à
rebours expire. Sans JavaScript, les chiffres restent ceux que le serveur a
rendus, donc justes à l'instant du chargement.

Les cartes suivent le même mouvement. Une construction à laquelle il ne manque
que des ressources porte son bouton dès le rendu, désactivé, et la page le lève
à la seconde où le compte y est ; au chantier elle recalcule aussi la taille du
lot que le stock couvre. Un prérequis manquant, lui, ne se lève jamais tout
seul, pas plus qu'un manque d'énergie, qui ne se remplit pas avec le temps. Le
serveur revérifie tout à la commande : la page ne fait que cesser de barrer la
route.

Chaque illustration est demandée par un slot stable, `/art/{catégorie}/{slug}`,
où le slug est l'identifiant du domaine. Le serveur dessine un placeholder
déterministe tant que le slot est vide, et sert le fichier dès qu'il existe dans
`web/static/art/{catégorie}/`. Ajouter du vrai artwork ne demande donc aucune
modification de gabarit ni de feuille de style : voir
[`docs/adr/0002-illustration-slots.md`](docs/adr/0002-illustration-slots.md).

Les vaisseaux sont désormais illustrés pour de bon, du chantier spatial aux
écrans de flotte. Les images d'origine vivent dans [`images/`](images/), hors
du binaire, et `make art` en tire les fichiers embarqués : voir
[`docs/adr/0003-masters-et-derives-d-illustration.md`](docs/adr/0003-masters-et-derives-d-illustration.md).
Les autres catégories gardent leur placeholder tant que leurs illustrations
n'existent pas.

Les joueurs font équipe sans jamais mettre leurs ressources en commun. Une
alliance a des rangs, des invitations qui expirent, une diplomatie déclarative
et un historique. Un rapport ne parvient à l'alliance que si son propriétaire le
partage explicitement. Une attaque groupée réunit plusieurs flottes alliées sur
une même cible à la même seconde : la page de préparation montre les
participants et dit si la flotte que l'on ajoute retarde toute l'équipe. Le
combat qui en résulte est unique et multi-acteurs, et le butin se répartit entre
les flottes survivantes selon la place qui leur reste. Une flotte envoyée en
défense alliée attend sur place jusqu'à la fin de sa garde, se bat pour la
planète, puis rentre.

L'univers est peuplé des joueurs contrôlés par le serveur que sa configuration
commande. Le nombre, les alliances, les horaires et la fréquence de réflexion se
règlent à l'étape 8 de l'assistant ; le serveur comble ensuite l'écart entre ce
qui a été demandé et ce qui existe, quelques joueurs à chaque passage, et une
partie déjà lancée se remplit de la même façon. Un administrateur en ajoute à la
main depuis la page Administration, retire ceux dont il ne veut plus — un retrait
est définitif, personne ne renaît à sa place — et suit leur santé, leur prochaine
réflexion et le journal de leurs décisions. Une intelligence artificielle possède un compte sans mot de
passe, fonde son empire, paie ses constructions, attend ses files, espionne
avant d'attaquer, perd ses flottes et dort en dehors de ses heures. Elle passe
par les mêmes cas d'usage que vous : un test structurel garantit qu'elle
n'atteint aucune base de données ni aucune vérité adverse.

Les machines peuvent aussi faire équipe. Un administrateur les affecte à une
alliance ; elles s'y partagent alors leurs rapports, se répartissent des rôles
selon ce que chacune déclare posséder, choisissent une cible commune, l'espionnent
avant d'y aller et résolvent ensemble une attaque groupée. Une alliée attaquée
appelle à l'aide et celles qui ont des vaisseaux viennent stationner chez elle.
Chaque croyance de cette mémoire commune porte son auteur, sa date et son
expiration : retirer le partage d'un rapport la fait disparaître aussitôt.

Le serveur écrit des journaux structurés sur la sortie d'erreur. Chaque requête
porte un identifiant de corrélation renvoyé dans l'en-tête `X-Request-Id` ;
aucun secret, jeton ni cookie n'y figure. `UAW_LOG_LEVEL` choisit le niveau
(`debug`, `info`, `warn`, `error`).

La commande `migrate` applique les migrations embarquées. `doctor` vérifie la
version du schéma, l'intégrité SQLite et les clés étrangères sans modifier les
données. `admin reset-password` constitue la récupération locale : elle génère
un nouveau secret, invalide toutes les sessions du compte et exige un nouveau
changement de mot de passe.

Une flotte peut aussi partir en expédition au-delà de la dernière planète d'un
système : elle y attend, et ce qu'elle trouve — ressources, vaisseaux, retard,
pirates, aliens, pertes ou rien du tout — est tiré une fois depuis une seed
persistée, donc rejouable.

Un administrateur dispose d'un tableau de bord (état, base, arriéré, débit,
population, sanctions, dernière sauvegarde), de la gestion des comptes, des
rôles et des invitations, de la modération, des joueurs artificiels et d'une
sauvegarde vérifiée en un clic. La mise en service complète, la matrice de
builds et les procédures de restauration sont décrites dans
[`docs/operations/`](docs/operations/release.md).

## Plans d'implémentation

Les jalons restants sont découpés en tâches exécutables dans
[`docs/plans/`](docs/plans/README.md) : feuille de route, socle transverse et
plans détaillés des recherches, du moteur de flotte et du combat.

## Qualité

```sh
make test
make test-race
make vet
make build
```

La CI ajoute `staticcheck`, `govulncheck` et les compilations Linux, Windows et
macOS. Les fonctionnalités métier sont développées en TDD, par tranches
transactionnelles.
