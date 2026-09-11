# Schéma de flotte

La migration `0006` ajoute le moteur de flotte.

## Tables

- `fleets` : propriétaire, planète d'origine, coordonnées de départ et de
  destination, mission, vitesse choisie, vitesse effective, distance, carburant,
  seed, version du ruleset, départ, arrivée, retour, rappel, état et version
  optimiste.
- `fleet_ships` : composition, quantités strictement positives.
- `fleet_cargo` : ressources embarquées, jamais négatives.
- `fleet_transitions` : journal des changements d'état avec leur motif, qui rend
  l'arbitrage des courses vérifiable après coup.

## Frontières transactionnelles

Lancer : régler la production, relire ruleset, technologies, inventaire et
emplacements, calculer distance, durée, carburant et capacité, retirer les
vaisseaux par une mise à jour gardée, débiter cargo et carburant, créer la
flotte, sa composition, son cargo, sa transition initiale, l'événement
d'arrivée, la clé d'idempotence et le journal, puis commit.

Arriver : relire la flotte ; un état autre que `outbound` fait de l'événement un
succès sans effet. Un transport livre ce que le stockage de la cible accepte,
garde le reste et programme le retour. Un stationnement déplace vaisseaux et
cargo puis termine la mission. Une cible disparue renvoie la flotte.

Revenir : créditer le cargo dans la limite du stockage, rendre les vaisseaux,
terminer la mission. Une origine disparue détruit la flotte avec le motif
`origin_lost`.

Rappeler : annuler l'événement d'arrivée par sa clé d'idempotence. Zéro ligne
annulée signifie que l'arrivée a gagné la course, et le rappel est refusé. La
transition est gardée par la version de la flotte, si bien qu'une seule sortie
de l'état `outbound` peut réussir.

## Ordre des événements

`fleet_arrived` porte la priorité 30 et `fleet_returned` la priorité 40 : à
échéance identique, une arrivée est traitée avant un retour, et les deux avant
les achèvements de bâtiment, de recherche et de production.

## Projection d'activité de l'empire

Le panneau permanent des corps lit une projection dédiée, sans recopier les
files dans le navigateur. Pour chaque corps, elle compte les bâtiments et les
recherches encore en file, les vaisseaux et défenses restant à livrer, et les
flottes actives qui en sont parties. Les quantités du chantier correspondent
aux unités restantes (`quantity - delivered`), pas au nombre de lots.

La même lecture sélectionne les attaques encore `outbound` dont la cible
appartient actuellement au joueur. Elle alimente à la fois le triangle d'alerte
du corps visé et la liste des approches de la page Flotte, afin que les deux ne
puissent pas diverger. Une attaque n'est visible que par son défenseur : les
missions d'espionnage et les vols dirigés vers un tiers ne sortent pas de cette
frontière.
