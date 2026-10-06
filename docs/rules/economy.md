# Économie — règle de fidélité

## Comportement attendu

Une planète produit du métal, du cristal et du deutérium sans tick global. La
production est réglée paresseusement avant toute lecture ou mutation économique,
à partir de `produced_at` jusqu'à l'instant UTC demandé. Les ressources sont des
entiers positifs ou nuls. L'énergie est une capacité instantanée : elle n'est pas
stockée.

Chaque planète commence avec 500 métal, 500 cristal et 0 deutérium. Chaque
stockage vaut au moins `base_storage` unités. Une ressource arrivée à sa capacité
ne progresse plus, mais son reste fractionnaire est conservé afin qu'un relèvement
de capacité ne fasse pas dépendre le résultat de la fréquence des lectures.

## Formules

Toutes les productions horaires sont d'abord arrondies à l'entier inférieur :

```text
mine(level, coefficient) = floor(coefficient * level * growth^level * economy_speed)
metal/h     = floor(base_metal/h * economy_speed) + mine(metal_level, 30)
crystal/h   = floor(base_crystal/h * economy_speed) + mine(crystal_level, 20)
deuterium/h = floor(base_deuterium/h * economy_speed)
              + floor(10 * level * growth^level * economy_speed
                      * max(0, 1.44 - 0.004 * max_temperature))
```

L'énergie requise par une mine est :

```text
floor(10 * level * energy_consumption_growth^level)
```

La centrale solaire produit :

```text
floor(20 * level * 1.1^level)
```

Une centrale électrique de fusion alimentée en deutérium produit, avec `E`
le niveau de technologie énergétique :

```text
floor(30 * level * (1.05 + E / 100)^level)
```

Elle consomme par heure, à la vitesse économique de l'univers :

```text
floor(10 * level * 1.1^level * economy_speed)
```

Cette consommation est soustraite de la production horaire de deutérium et
peut donc rendre son taux net négatif. Le stock ne descend jamais sous zéro ;
une centrale sans deutérium disponible s'arrête jusqu'au prochain règlement qui
rend du carburant disponible.

Les fonctions d'énergie s'appellent par niveau autant qu'elles se somment sur
la planète, de sorte qu'un écran peut dire ce qu'un niveau donné consomme ou
produit sans réécrire la formule. C'est ce que la carte d'un bâtiment affiche,
en différence entre le niveau atteint et le niveau visé.

Le facteur énergétique vaut 1 si la production couvre la consommation. Sinon :

```text
max(low_energy_production, energy_produced / energy_consumed)
```

Il ne réduit que la production des mines, jamais la production de base.

Le règlement d'une ressource conserve un reste en `unités-secondes` :

```text
numerator = net_hourly_rate * elapsed_whole_seconds + previous_remainder
whole     = numerator / 3600
remainder = numerator % 3600
stored    = clamp(stored + whole, 0, capacity)
```

Les instants persistés ont une précision d'une seconde. Une durée partielle est
donc ignorée de façon déterministe. Le calcul intermédiaire vérifie les
débordements et refuse un temps antérieur à `produced_at`.

La capacité d'un stockage de niveau `L` est :

```text
floor(base_storage * 2^L)
```

Un stock supérieur à la capacité est ramené à la capacité au règlement suivant :
le plafond s'applique au stock, pas seulement à sa croissance. Toute livraison
extérieure — transport, butin, production — doit donc plafonner explicitement ce
qu'elle dépose, sous peine de perte silencieuse.

## Constantes et incertitudes

Les coefficients 30/20/10, le facteur de température du deutérium et les
coefficients énergétiques suivent le modèle classique OGame. Ils sont regroupés
dans le catalogue économique du domaine ; les vitesses, productions de base,
croissances, capacité de base et plancher énergétique viennent du ruleset actif.
Une future version de ruleset pourra exposer les coefficients sans modifier
l'algorithme.

## Exemples de référence

- Sans mine, à vitesse 1, deux heures ajoutent 60 métal et 30 cristal.
- Une mine de métal niveau 1 avec croissance 1,1 ajoute 33 métal/h, en plus des
  30 métal/h de base.
- À 50 % d'énergie disponible et avec un plancher à 50 %, seule la production
  des mines est divisée par deux.
- 1 unité/h réglée après 1 800 secondes ne produit rien et conserve un reste de
  1 800 ; un second règlement identique produit exactement 1 unité.

## Cas limites et invariants

- aucun solde, débit ou plafond négatif ; seul le taux net de deutérium peut
  l'être lorsqu'une centrale à fusion consomme plus que le synthétiseur ;
- aucun dépassement de capacité après règlement ;
- aucune perte due à des lectures rapprochées grâce au reste ;
- un débit est atomique avec l'action qui le motive ;
- deux dépenses concurrentes du même solde ne peuvent réussir ensemble ;
- un règlement répété au même instant est neutre ;
- une horloge qui recule est rejetée.

## Tests de référence

- production hors ligne après deux heures avec horloge factice ;
- équivalence d'un règlement unique et de plusieurs règlements intermédiaires ;
- plafonnement de chaque ressource ;
- réduction par manque d'énergie ;
- conservation des restes ;
- refus d'un débit insuffisant et absence de ressource négative ;
- concurrence de deux débits en SQLite.
