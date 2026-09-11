# Simulation de combat — estimation avant lancement

## Parcours

Un rapport d'espionnage ou un rapport d'attaque propose de préparer une
nouvelle attaque depuis le corps actuellement sélectionné. Le raccourci ouvre
l'assistant de flotte avec la cible et la mission déjà renseignées. Dès que la
composition est validée, la confirmation affiche l'estimation avant le bouton
qui lance réellement la flotte.

La simulation ne lance rien et ne persiste rien. Modifier la flotte puis
recalculer la mission recalcule aussi l'estimation.

## Frontière d'information

Le simulateur ne lit jamais la planète cible. Il utilise exclusivement :

- la flotte, les défenses, les recherches et les ressources effectivement
  révélées dans le rapport choisi ;
- les survivants et les niveaux technologiques consignés dans un rapport
  d'attaque ;
- la composition, le cargo déjà chargé et les technologies actuelles du joueur
  qui prépare la mission ;
- les règles et le catalogue actifs de l'univers.

Cette frontière est volontaire. Une section absente d'un rapport d'espionnage
reste inconnue au lieu d'être complétée depuis la base. Un rapport partagé avec
une alliance est utilisable, mais seulement à travers le même contrôle d'accès
que sa page de détail. Un identifiant de rapport étranger répond comme un
rapport inexistant.

Un ancien document qui porte un niveau de révélation suffisant conserve la
signification de ce niveau : une carte absente peut alors signifier une flotte
ou une défense vide, conformément à la compatibilité des rapports historiques.

## Calcul

Le moteur pur `combat.Resolve` est exécuté avec plusieurs seeds dérivées de
manière stable du rapport, de la cible et de la composition. Recalculer la même
mission produit donc la même distribution. Le nombre de tirages s'adapte à la
taille de la bataille, avec au plus 64 résolutions et un budget total de 500 000
unités simulées.

Au-delà de 500 000 unités dans un seul combat, la prévisualisation de flotte
reste utilisable mais l'estimation est désactivée afin qu'une requête HTTP ne
puisse pas monopoliser le serveur.

L'écran rapporte :

- les fréquences de victoire, de nul et de défaite ;
- la valeur moyenne et la fourchette observée des pertes de chaque camp ;
- le nombre minimum, moyen et maximum de vaisseaux et de défenses perdus pour
  chaque type d'unité ;
- les débris issus des vaisseaux, ceux issus des défenses, leur total et la
  chance de lune ;
- le butin lorsque le rapport a révélé les ressources ;
- le bilan après pertes et carburant, puis le bilan hypothétique si tous les
  débris produits étaient recyclés.

Les coûts tiennent compte des multiplicateurs de vaisseaux et de défenses. Le
butin emploie la capacité encore libre des survivants après le cargo déjà
chargé, et les limites de pillage du même jeu de règles que le combat réel.

Une absence de perte dans un tirage compte comme zéro dans la moyenne d'une
unité. Les pertes de défenses sont celles qui restent après leur reconstruction
aléatoire : une défense reconstruite n'est ni une perte définitive, ni une
source de débris.

## Incertitudes affichées

Lorsque les technologies ennemies n'ont pas été révélées, le scénario emploie
les niveaux zéro et le dit explicitement. Sans flotte ou défenses révélées,
aucun résultat numérique n'est présenté. Sans ressources révélées, les pertes
et les débris restent calculables mais pas le butin ni le bilan net.

Un rapport d'attaque repart de ses survivants connus. Dans tous les cas, le
résultat reste une estimation historique : productions, constructions,
mouvements et renforts postérieurs au rapport peuvent modifier la bataille
réelle avant l'arrivée.
