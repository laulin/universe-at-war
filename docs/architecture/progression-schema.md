# Schéma de progression

La migration `0005` ajoute la progression technologique et la production
d'unités au schéma économique.

## Tables

- `player_research` : niveau par joueur et par recherche. Les niveaux
  appartiennent au joueur, jamais à une planète.
- `research_queue` : file de recherche du joueur, avec rang, coût, énergie,
  laboratoire effectif et version du ruleset. Un index partiel unique sur
  `player_id` garantit une seule recherche `'active'` à la fois ; les lignes
  `'queued'` derrière elle attendent sans horaire ni événement.
- `planet_units` : inventaire des vaisseaux et des défenses d'une planète. La
  contrainte `quantity >= 0` interdit tout inventaire négatif, y compris sous
  concurrence.
- `production_orders` : files de production de la planète, une par famille.
  Chaque ligne est un lot d'un modèle, avec rang, quantité, quantité livrée,
  coût unitaire, durée unitaire et version du ruleset. Depuis la migration
  `0019`, l'index partiel unique porte sur `(planet_id, family)` : vaisseaux et
  défenses avancent en parallèle au lieu de se disputer un emplacement.
  `CHECK (state <> 'completed' OR delivered = quantity)` interdit de clore une
  commande non livrée.

## Frontières transactionnelles

Mettre une recherche en file : régler la production, relire ruleset et
prérequis, y ajouter les niveaux que la file atteint déjà, vérifier l'énergie
disponible, **débiter le coût immédiatement**, ajouter la ligne et, si elle
arrive en tête, l'événement `research_completed`, avec la clé d'idempotence et
le journal, puis commit.

Commander des unités : régler la production, livrer ce que les lots en cours ont
terminé, compter ce que les files doivent encore contre les limites du modèle,
calculer coût et durées, **débiter**, ajouter le lot et, s'il arrive en tête,
l'événement `production_completed`, la clé d'idempotence et le journal, puis
commit.

L'achèvement d'une recherche ou d'un lot promeut la ligne suivante de sa file
dans la même transaction. La durée d'une ligne promue n'est calculée qu'à cet
instant : un laboratoire ou un chantier terminé entre-temps accélère réellement
ce qui attendait derrière. Le coût, lui, reste celui figé à la commande.

Annuler ferme la ligne en `'cancelled'`, annule son événement encore `'pending'`,
rembourse — écrêté à la capacité des entrepôts — et promeut la suivante. Une
recherche annulée entraîne les niveaux de la même technologie empilés au-dessus.
Un lot ne rembourse que les unités que le chantier devait encore : celles déjà
livrées restent acquises.

Régler une planète livre au passage les unités terminées. L'ordre est celui de
la fiche : les ressources de l'intervalle écoulé sont produites avec l'ancien
nombre de satellites, puis les unités sont livrées, puis les taux affichés sont
recalculés.

## Exclusivité des installations

Le laboratoire ne s'améliore pas tant qu'une recherche est active **ou en
attente** ; une recherche ne démarre pas tant que le laboratoire est quelque part
dans la file de construction de la planète. Le chantier spatial et l'usine de
nanites suivent la même règle vis-à-vis des files de production.

Regarder les files entières plutôt que la seule ligne en cours garde l'exclusion
vraie à chaque instant, sans qu'une file ait jamais à caler : le refus tombe à la
commande, pas au moment de la promotion. Les refus sont symétriques et portent
l'erreur métier correspondante.
