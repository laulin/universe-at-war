# Schéma de progression

La migration `0005` ajoute la progression technologique et la production
d'unités au schéma économique.

## Tables

- `player_research` : niveau par joueur et par recherche. Les niveaux
  appartiennent au joueur, jamais à une planète.
- `research_queue` : file de recherche avec coût, énergie, laboratoire effectif
  et version du ruleset capturés au démarrage. Un index partiel unique sur
  `player_id` garantit une seule recherche active par joueur.
- `planet_units` : inventaire des vaisseaux et des défenses d'une planète. La
  contrainte `quantity >= 0` interdit tout inventaire négatif, y compris sous
  concurrence.
- `production_orders` : commande de production avec quantité, quantité livrée,
  coût unitaire, durée unitaire et version du ruleset. Un index partiel unique
  sur `planet_id` garantit une seule commande active par planète, vaisseaux et
  défenses partageant le chantier. `CHECK (state <> 'completed' OR delivered =
  quantity)` interdit de clore une commande non livrée.

## Frontières transactionnelles

Démarrer une recherche : régler la production, relire ruleset et prérequis,
vérifier l'énergie disponible, débiter, créer la file, l'événement
`research_completed`, la clé d'idempotence et le journal, puis commit.

Commander des unités : régler la production, livrer ce que la commande courante
a terminé, refuser une commande concurrente, calculer coût et durées, débiter,
créer la commande, l'événement `production_completed`, la clé d'idempotence et
le journal, puis commit.

Régler une planète livre au passage les unités terminées. L'ordre est celui de
la fiche : les ressources de l'intervalle écoulé sont produites avec l'ancien
nombre de satellites, puis les unités sont livrées, puis les taux affichés sont
recalculés.

## Exclusivité des installations

Le laboratoire ne s'améliore pas pendant une recherche du joueur ; le chantier
spatial et l'usine de nanites ne s'améliorent pas pendant une commande de la
planète. Les refus sont symétriques et portent l'erreur métier correspondante.
