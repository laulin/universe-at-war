# ADR 0004 — Peuplement artificiel

Statut : accepté.

## Contexte

L'étape 8 de l'assistant demande combien de joueurs artificiels un univers doit
porter, en combien d'alliances, avec quels horaires et quelle fréquence de
réflexion. La réponse était validée puis écrite dans le ruleset, et personne ne
la relisait jamais. L'activation enregistre le ruleset, fait passer le serveur à
`RUNNING` et ne crée personne : un univers configuré pour cinquante machines
tournait vide, et le seul moyen de le remplir était un administrateur tapant un
nom à la fois dans un formulaire.

Trois contraintes encadrent la correction. L'activation refuse de tourner deux
fois, donc un correctif qui s'y accroche ne répare aucun univers déjà lancé.
Créer un joueur exige un principal administrateur, que le serveur agissant pour
son compte n'a pas. Et le placement d'un empire partait toujours du début de la
carte, ce qui empile une population fondée d'un coup sur les premiers inscrits.

## Décisions

- Le peuplement est une **réconciliation**, jamais un branchement sur
  l'activation : `appai.Populating` compare la commande du ruleset à ce qui
  existe et comble l'écart. Il remplit un univers neuf et répare un univers
  ancien par le même chemin, et ne coûte rien une fois les deux d'accord.
- Il tourne dans la boucle du worker existant, **quelques joueurs par passage**,
  en tête de boucle pour que la première réflexion d'un nouveau joueur soit déjà
  dans l'horaire que ce passage va lire.
- `Create` et `Enlist` sont scindés de leur contrôle de privilège. Le contrôle
  reste soudé à la frontière HTTP et la moitié non exportée sert l'appelant
  interne : **aucun principal n'est forgé**. Le joueur qui en sort a exactement
  les droits d'un humain, c'est-à-dire aucun.
- Un slot historique reste **dépensé** et une IA retirée ne renaît jamais sous
  la même identité. En revanche, `AI.Total` cible désormais les profils actifs :
  un départ libère une place pour un nouvel arrivant, créé avec un nouveau nom,
  un nouvel empire et une nouvelle graine.
- `AI.Total` fait autorité : un univers qui demande cinquante joueurs en obtient
  cinquante. Les alliances sont pourvues d'abord, le reste est indépendant.
- Les empires artificiels visent une case et s'installent à la première libre à
  partir d'elle. **L'inscription d'un humain n'est pas touchée** : elle ne vise
  rien et prend la première position libre de l'univers, comme toujours.
- Une heure de fin à vingt-quatre est ramenée à zéro, qui est la façon dont une
  fenêtre écrit la fin du jour. Durcir le ruleset à la place rendrait indécodable
  le document actif d'un univers en cours, et l'emporterait avec lui.

## Conséquences

Un serveur se peuple seul en quelques minutes après l'activation, et une partie
lancée avant cette version se remplit au redémarrage sans repartir de zéro.

La population reste ensuite vivante : une IA qui perd son dernier monde est
retirée, quitte proprement son alliance et laisse arriver un remplaçant. Un
retrait administratif produit le même remplacement sans réactiver l'ancien
profil. La règle détaillée et ses cas limites sont consignés dans
`docs/rules/ai-population.md`.

Un seul réglage de la section `ai` reste inerte : `initial_development_level`.
Il ne peut pas être honoré naïvement — offrir des bâtiments aux machines est
précisément l'API interne de faveur que la spécification interdit. Un départ
avancé devra être une règle de l'univers appliquée à tous, humains compris.

`independent_count` est descriptif : `total` décidant de la population, ce
compteur est un plancher et non une part. L'assistant le dit désormais, et la
page d'administration affiche la population commandée à côté de celle présente.

## Décisions ultérieures

- **La difficulté** infléchit les poids d'un archétype : marge de sécurité,
  prudence, convoitise, seuil de rentabilité et nombre de sondes. Elle ne touche
  ni le rythme de développement, qui appartient au caractère, ni le fleetsave,
  qui est une règle de bien jouer et non un curseur. Elle est lue dans le ruleset
  à chaque réflexion plutôt que stockée, donc la changer change tout le monde.
- **La coordination** est la part des réflexions qu'un membre consacre à ce que
  son alliance veut. Le tirage vient de la graine et du tick, dans l'idiome des
  réflexions, mêlé d'une constante propre pour ne pas suivre leur gigue. Ne pas
  répondre à un appel n'est pas gêner : le membre laisse la cible de l'alliance
  tranquille et continue de partager ce qu'il voit.
- **La diplomatie** repose sur les déclarations reçues, que le build ne chargeait
  pas : une relation annonce, et une annonce que personne n'entend n'annonce
  rien. Les alliances répondent en nature, jamais deux fois, et seul le chef
  parle, par le cas d'usage ordinaire.
- **Une naissance interrompue** après la fondation de l'empire n'est plus
  abandonnée : le compte n'est plus désactivé, et le réconciliateur donne son
  caractère au joueur avant d'en fonder de nouveaux.
