# Phalange — règle de fidélité

## Comportement attendu

La phalange est un instrument de renseignement temporel : elle dit quand des
flottes arrivent quelque part, pas ce qu'elles transportent. Elle se construit
sur une lune et coûte du deutérium à chaque balayage.

Un balayage ne révèle jamais une information qu'un joueur ne pourrait pas obtenir
autrement sur la composition ou le cargo d'une flotte adverse.

## Portée

```text
rayon = niveau_phalange^2 - 1
```

Une phalange de niveau 1 n'observe que son propre système. Le rayon compte des
systèmes de la même galaxie ; l'écart est circulaire lorsque la topologie l'est.
Une phalange n'observe jamais une autre galaxie.

## Coût

Chaque balayage prélève `coût_de_balayage` deutérium sur la lune qui l'exécute,
dans la même transaction que le balayage. Un stock insuffisant refuse le
balayage sans rien révéler.

## Informations révélées

Pour la position balayée, la phalange liste les missions en vol dont l'origine ou
la destination est cette position exacte, avec :

- le type de mission ;
- l'origine et la destination ;
- l'heure d'arrivée et, si elle existe, l'heure de retour ;
- le nombre total de vaisseaux.

Elle ne révèle jamais la composition détaillée, ni le cargo, ni la seed, ni le
propriétaire des flottes qu'elle observe. Les missions d'espionnage restent
invisibles : des sondes ne déclenchent pas un capteur de masse.

## Cas limites et invariants

Une position hors de portée renvoie une erreur, pas une liste vide, afin que le
joueur sache que sa phalange est trop faible. Une lune sans phalange ne balaye
pas. Le balayage ne modifie rien d'autre que le stock de deutérium de la lune.

## Tests de référence

Table des rayons pour les niveaux 1 à 5, avec et sans circularité ; refus hors
de portée ; refus sans deutérium ; missions d'espionnage absentes ; absence de
composition détaillée et de cargo dans la projection comme dans la réponse HTTP.
