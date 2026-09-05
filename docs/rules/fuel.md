# Carburant — règle de fidélité

## Comportement attendu

Le carburant est du deutérium, prélevé sur la planète de départ au lancement,
pour l'aller et le retour. Il n'est jamais remboursé, même si la flotte est
rappelée : le trajet déjà parcouru a consommé.

Le carburant réduit la capacité de cargo disponible : une flotte ne peut pas
emporter à la fois son plein et sa charge maximale.

## Formules

```text
consommation = somme sur la composition de
               quantité * consommation_unitaire * distance / 35000
               * (pourcentage / 100 + 1)^2
carburant = 1 + floor(deuterium_consumption * consommation)
capacité  = somme(quantité * cargo_unitaire) - carburant
```

Le terme constant garantit qu'aucune mission n'est gratuite. Une vitesse réduite
réduit la consommation : à 50 %, le facteur vaut 2,25 au lieu de 4.

## Exemples de référence

| Composition | Distance | Vitesse | Carburant |
| --- | ---: | ---: | ---: |
| 1 chasseur léger | 1 010 | 100 % | 3 |
| 1 chasseur léger | 1 010 | 50 % | 2 |
| 2 petits transporteurs | 3 080 | 100 % | 8 |
| 1 chasseur léger | 20 000 | 100 % | 46 |
| 1 chasseur léger | 5 | 100 % | 1 |

Deux petits transporteurs offrent 10 000 de cargo ; après 8 de carburant, il
reste 9 992 pour les ressources.

## Cas limites et invariants

Le deutérium disponible doit couvrir le carburant et la part de deutérium du
cargo. Un cargo supérieur à la capacité restante est refusé. Le carburant est
strictement positif, entier, et calculé avec la version du ruleset capturée au
lancement : changer la règle ne modifie aucune flotte déjà partie.

## Tests de référence

Table des cinq cas ci-dessus ; refus d'un cargo trop grand ; refus d'un
deutérium insuffisant sans mutation partielle ; conservation du deutérium entre
les planètes, les cargos et le carburant consommé.
