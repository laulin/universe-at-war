# ADR 0003 — Masters et dérivés d'illustration

Statut : accepté.

## Contexte

L'ADR 0002 a défini le mécanisme des slots et prévu son propre remplissage :
déposer un fichier suffit. Les premières illustrations arrivent, et elles font
1254 pixels de côté pour 2,5 Mo pièce. Trois contraintes ne se concilient pas
d'elles-mêmes. La distribution est un binaire autonome, qu'embarquer trente-cinq
mégaoctets rendrait absurde pour servir des images que l'interface recadre en
640 × 480. Le dépôt doit rester la seule source de vérité de ce qui s'affiche,
donc jeter le fichier d'origine après recadrage interdirait de le refaire. Et
l'intégration continue ne dispose d'aucun outil de traitement d'image, ni ne
doit en gagner un : sa promesse est de compiler l'arbre tel qu'il est cloné.

## Décisions

- Le master vit dans `images/{catégorie}/{slug}.png`, hors de la racine
  `//go:embed`, et n'est jamais servi.
- Son nom est le slug et son dossier est la catégorie : l'identité que l'ADR
  0002 donne au slot remonte jusqu'au fichier d'origine, ce qui évite toute
  table de correspondance à tenir à jour avec le catalogue.
- `scripts/build-art.sh`, appelé par `make art`, en tire
  `web/static/art/{catégorie}/{slug}.webp` : recadrage centré au format du slot,
  WebP qualité 80. Une catégorie inconnue ou un nom qui n'est pas un slug
  arrêtent la course.
- Le dérivé est versionné. `make art` est un outil de développement, jamais une
  étape de construction, et ne fait donc pas partie de `check`.
- Un test énumère le catalogue et exige une illustration réelle par vaisseau,
  ainsi que l'inverse : un fichier qu'aucun vaisseau ne réclame est une erreur.

## Conséquences

Le binaire grossit d'environ 950 Kio pour quinze illustrations, contre trente-
cinq mégaoctets si les masters y entraient. Le dépôt les porte à leur place :
c'est un prix payé une fois pour garder la possibilité de re-cadrer ou de
re-dimensionner sans jamais rien redessiner. Regénérer demande ImageMagick et
libwebp, que rien d'autre du projet n'exige, et un ré-encodage sous une autre
version de libwebp produira un diff sans changement visible — bruit accepté,
non défaut. Enfin, ajouter un vaisseau au catalogue oblige désormais à produire
son image : c'est le rappel voulu, puisqu'une illustration manquante ne se voit
pas autrement.
