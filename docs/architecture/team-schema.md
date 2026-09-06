# Schéma des alliances et des opérations groupées

Les migrations `0010` à `0012` ajoutent les équipes, les opérations groupées et
le stationnement défensif.

## Tables

- `alliances` : nom et étiquette uniques après normalisation, description et
  date de création.
- `alliance_members` : `player_id` est la clé primaire, ce qui interdit par le
  schéma d'appartenir à deux alliances à la fois. Le rang y est explicite.
- `alliance_invitations` : un index unique partiel n'autorise qu'une invitation
  en attente par joueur et par alliance ; l'expiration est persistée.
- `alliance_relations` : diplomatie déclarative, sans effet sur le combat.
- `alliance_history` : une ligne par décision, jamais modifiée.
- `acs_groups` : propriétaire, alliance, cible, seed, version de ruleset, heure
  d'arrivée, état et version optimiste.
- `acs_participants` : `fleet_id` est la clé primaire, si bien qu'une flotte
  n'appartient qu'à une seule opération.
- `fleets` gagne `holds_until` et l'état `holding` : une flotte en défense
  alliée attend sur place jusqu'à une heure décidée au départ.

Les rapports partagés ne sont jamais copiés : `reports.shared_alliance_id`
désigne l'alliance qui peut les lire, et se vide quand le propriétaire retire le
partage ou quitte l'équipe.

## Types d'événements

Une flotte engagée dans une opération ne planifie aucune arrivée : le groupe
possède `acs_resolved` sous la clé `acs-arrive:<groupe>`, à la priorité du
combat, et fait atterrir toutes ses flottes ensemble. Une flotte en défense
planifie `holding_ended` sous la clé `fleet-hold:<flotte>` ; anéantie au combat,
cet événement est annulé avec elle.

## Frontières transactionnelles

Ouvrir une opération : lancer la première flotte sans arrivée propre, créer le
groupe et planifier son arrivée, le tout dans une transaction.

Rejoindre : relire le groupe, vérifier l'appartenance, la place et la fenêtre,
lancer la flotte, puis resynchroniser l'opération. Une seule instruction déplace
l'arrivée de chaque flotte en préservant l'écart entre son arrivée et son
retour, qui est sa propre durée de trajet.

Se retirer : rappeler sa flotte comme n'importe quel rappel, la sortir du
groupe, et annuler l'opération et son arrivée si elle était la dernière.

Résoudre : verrouiller le groupe, rassembler les flottes encore en vol, livrer
un seul combat contre la planète et ses défenseurs stationnés, appliquer les
pertes de chaque partie, créer les débris, prélever le butin une fois puis le
répartir, écrire un rapport par participant, renvoyer les survivants et marquer
le groupe résolu. Une cible disparue annule l'opération et renvoie tout le monde.
