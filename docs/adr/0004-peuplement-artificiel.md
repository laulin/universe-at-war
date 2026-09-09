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
- Un slot est **dépensé tant qu'un profil ou un empire subsiste**. Une IA retirée
  ne renaît donc jamais : le retrait est une décision d'administrateur. Une
  naissance qui n'a pas atteint son empire ne laisse rien et son slot est
  réessayé, sous le nom suivant si celui qu'elle voulait est déjà porté.
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

Quatre réglages de la section `ai` restent inertes : `difficulty`,
`coordination`, `diplomacy` et `initial_development_level`. Le dernier ne peut
pas être honoré naïvement — offrir des bâtiments aux machines est précisément
l'API interne de faveur que la spécification interdit. Un départ avancé devra
être une règle de l'univers appliquée à tous, humains compris.

`independent_count` devient descriptif : `total` décidant de la population, ce
compteur ne commande plus rien, et l'écart entre ce que l'assistant affiche et ce
que l'univers contient devra être levé côté interface.
