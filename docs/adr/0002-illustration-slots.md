# ADR 0002 — Slots d'illustration

Statut : accepté.

## Contexte

L'interface visée est celle d'un centre de commandement spatial : bannière par
écran, vignette par bâtiment, par recherche, par vaisseau et par défense, icône
par ressource. Le dépôt ne contient aucune image et la distribution est un
binaire autonome : embarquer une cinquantaine d'illustrations avant de les avoir
dessinées bloquerait la refonte, et laisser les emplacements vides afficherait
des images cassées.

## Décisions

- Les illustrations sont adressées par un slot stable, jamais par un nom de
  fichier : `GET /art/{catégorie}/{slug}`.
- Le slug est l'identifiant que le domaine utilise déjà (`metal_mine`,
  `astrophysics`, `light_fighter`), donc aucun champ n'est ajouté aux vues.
- Le handler sert `web/static/art/{catégorie}/{slug}.{webp,avif,png,jpg,svg}`
  quand le fichier existe, et sinon dessine un SVG déterministe dérivé d'un
  hachage du slot.
- Les catégories sont une liste close, ce qui empêche le slug d'atteindre le
  système de fichiers comme un chemin arbitraire.
- La planète d'un corps est dessinée depuis sa position orbitale : les orbites
  intérieures brûlent, les extérieures gèlent.

## Conséquences

Ajouter une vraie illustration se réduit à déposer un fichier et recompiler :
ni template ni feuille de style ne bouge. Un écran n'a jamais d'image manquante.
Le binaire ne grossit qu'au rythme des illustrations réellement produites. En
contrepartie, tant qu'un slot n'est pas rempli, son placeholder reste
géométrique et ne remplace pas un travail d'illustration.
