# Schéma du renseignement et du combat

La migration `0007` ajoute les rapports et les champs de débris.

## Tables

- `reports` : destinataire, type, sujet, position, date, date de lecture,
  version et contenu du payload. Le contenu est filtré à la création : une
  section qu'un joueur n'a pas le droit de voir n'est jamais stockée dans son
  rapport. Un index partiel sert le compteur des rapports hostiles non lus.
- `debris_fields` : une ligne par position, métal et cristal positifs, avec la
  contrainte `metal + crystal > 0` : un champ vidé est supprimé plutôt que
  laissé à zéro.

## Types d'événements

L'arrivée d'une flotte prend le type de sa mission : `combat_resolved` pour une
attaque, `espionage_resolved` pour un espionnage, `fleet_arrived` pour le reste.
Les trois partagent la clé `fleet-arrive:<id>`, ce qui laisse le rappel annuler
l'arrivée sans connaître la mission, et leurs priorités respectives font résoudre
un combat avant tout autre mouvement de la même échéance.

## Frontières transactionnelles

Espionner : régler la cible à l'échéance, révéler ce que le niveau autorise,
tirer la détection avec la seed de la flotte, écrire les deux rapports, puis
renvoyer les sondes ou les détruire en laissant leurs débris.

Attaquer : régler la cible, construire l'entrée du moteur avec les technologies
des deux joueurs, résoudre avec la seed persistée, appliquer les pertes des deux
camps, créer les débris, prélever le butin dans la limite de la soute, écrire les
deux rapports, puis renvoyer les survivants ou détruire la flotte.

Recycler : lire le champ, calculer la prise avec la capacité des recycleurs,
retirer par une mise à jour gardée, charger la soute, écrire le rapport et
repartir.

Chacune de ces résolutions tient dans la transaction de son événement : un
incident avant le commit ne laisse ni rapport, ni débris, ni butin.

## Rejouabilité

`fleets.seed` est tiré au lancement avec une source cryptographique et persisté.
Le journal du combat conserve la seed et l'issue ; les compositions initiales
figurent dans les rapports. Rejouer le moteur avec ces trois éléments redonne
exactement la même bataille.
