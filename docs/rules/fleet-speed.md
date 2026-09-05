# Vitesse de flotte — règle de fidélité

## Comportement attendu

Chaque vaisseau possède une vitesse de base et un moteur. Le niveau du moteur
augmente sa vitesse. La flotte se déplace à la vitesse de son vaisseau le plus
lent. Le joueur choisit un pourcentage de vitesse par pas de dix, qui allonge la
durée et réduit la consommation.

Une flotte partie est engagée : sa durée est calculée au lancement, capturée avec
la version du ruleset, et ne change plus.

## Formules

```text
vitesse_unite = floor(vitesse_base * (1 + bonus_moteur * niveau_moteur))
vitesse_flotte = min(vitesse_unite) sur la composition
secondes = floor((10 + 35000 / pourcentage
                  * racine(10 * distance / vitesse_flotte))
                 / vitesse_univers)
durée = max(durée_minimale_de_mission, secondes)
```

Le bonus de moteur vaut 0,1 par niveau de réacteur à combustion, 0,2 par niveau
de réacteur à impulsion et 0,3 par niveau de propulsion hyperespace. Le
pourcentage appartient à `{10, 20, ..., 100}` ; toute autre valeur est refusée.

La vitesse d'univers dépend de la mission : `peaceful_fleet_speed` pour le
transport, le déploiement, l'espionnage, la colonisation et le recyclage,
`hostile_fleet_speed` pour l'attaque, `holding_fleet_speed` pour le
stationnement. Le retour dure exactement aussi longtemps que l'aller.

## Surclassement de moteur

Trois vaisseaux changent de moteur lorsque la technologie progresse. Le
surclassement remplace à la fois la vitesse de base et le moteur pris en compte :

| Unité | Condition | Nouveau moteur | Nouvelle vitesse de base |
| --- | --- | --- | ---: |
| `small_cargo` | réacteur à impulsion 5 | impulsion | 10 000 |
| `bomber` | propulsion hyperespace 8 | hyperespace | 5 000 |
| `recycler` | réacteur à impulsion 17 | impulsion | 4 000 |
| `recycler` | propulsion hyperespace 15 | hyperespace | 6 000 |

Le surclassement le plus avancé l'emporte.

## Exemples de référence

Un chasseur léger sans technologie vole à 12 500. Avec un réacteur à combustion
de niveau 3, il vole à 16 250. Un petit transporteur avec une impulsion de niveau
5 vole à 20 000 au lieu de 5 000.

| Composition | Distance | Vitesse | Durée |
| --- | ---: | ---: | ---: |
| 1 chasseur léger | 1 010 | 100 % | 324 s |
| 1 chasseur léger | 1 010 | 50 % | 639 s |
| 2 petits transporteurs | 3 080 | 100 % | 878 s |
| 1 chasseur léger | 20 000 | 100 % | 1 410 s |
| 1 chasseur léger | 5 | 100 % | 60 s (durée minimale) |

Avec une vitesse d'univers de 2, la première ligne tombe à 162 secondes.

## Cas limites et invariants

Une composition vide, une quantité négative, une unité inconnue ou une unité
immobile — satellite solaire, défense — est refusée. La durée est toujours au
moins la durée minimale de mission. Un débordement de calcul est refusé, jamais
saturé.

## Tests de référence

Table des vitesses avec et sans surclassement ; table des durées ci-dessus ;
refus d'un pourcentage invalide ; fuzz de la durée sur des distances et des
vitesses aléatoires.
