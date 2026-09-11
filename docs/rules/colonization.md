# Colonisation — règle de fidélité

## Comportement attendu

Une colonisation est une mission de flotte comme les autres : elle part, elle
vole, elle se résout à l'arrivée. Elle exige un vaisseau de colonisation, un
emplacement de colonie encore libre et une position vide dans les limites de
l'univers.

La carte de galaxie propose « Coloniser » sur chaque position inoccupée. Ce
raccourci ouvre l'assistant de flotte depuis le corps sélectionné, avec les
coordonnées et la mission déjà renseignées ; la composition reste choisie par
le joueur et suit les validations ordinaires.

Tout se décide à l'arrivée, jamais au départ : la position est relue dans la
transaction qui crée la planète. Si une autre colonisation est arrivée avant, la
mission échoue proprement et la flotte rentre avec tout ce qu'elle transportait.

## Emplacements de colonie

```text
emplacements = min(limite_du_ruleset, (astrophysique + 1) / 2)
colonies     = planètes possédées - 1
```

La planète mère ne consomme pas d'emplacement. Un joueur sans astrophysique ne
peut donc pas coloniser.

## Génération de la planète

Les caractéristiques sont tirées d'une source dérivée de la seed persistée de la
mission : la même mission recolonisée donnerait la même planète.

```text
centre       = (positions_par_système + 1) / 2
température_max = 40 - (position - centre) * 10
température_min = température_max - 40
cases           = min_cases + tirage(max_cases - min_cases + 1)
```

Une position proche du soleil est chaude, une position lointaine est froide, ce
qui rend le deutérium plus intéressant en périphérie.

## Résolution

Dans une seule transaction : relire la position, vérifier qu'elle est libre,
vérifier que l'emplacement de colonie est toujours disponible, créer la planète,
ses ressources et sa propriété, consommer exactement un vaisseau de colonisation,
déposer le cargo dans la limite du stockage de la nouvelle planète, puis renvoyer
le reste de la flotte.

Une colonisation réussie consomme un seul vaisseau de colonisation, même si la
flotte en emportait plusieurs ; les autres rentrent.

## Cas limites et invariants

Une position déjà occupée, une position hors limites, un emplacement de colonie
épuisé ou une flotte sans vaisseau de colonisation font échouer la mission sans
rien créer. Deux colonisations simultanées sur la même position ne créent qu'une
planète : la contrainte d'unicité de la position arbitre, et la perdante rentre.
Une redélivrance de l'événement d'arrivée ne crée jamais une seconde planète.

## Tests de référence

Colonisation nominale ; deux colonisations concurrentes sur la même position ;
limite d'emplacements atteinte ; absence de vaisseau de colonisation ; génération
déterministe des caractéristiques ; redélivrance idempotente.
