# Schéma des alliances de joueurs artificiels

Les migrations `0014` et `0015` ajoutent la mémoire commune d'une alliance de
machines, les rôles qu'elle distribue et le plan qu'elle poursuit.

## Tables

- `ai_alliance_memory` : une croyance par auteur et par position. Chaque ligne
  porte l'auteur, la date d'observation, l'expiration, la confiance et, quand
  elle en découle, le rapport d'origine. La lecture rejoint `alliance_members`
  et `reports` : une croyance dont l'auteur a quitté l'alliance, ou dont le
  rapport n'est plus partagé, n'est tout simplement plus là. La révocation ne
  demande donc aucun nettoyage.
- `ai_alliance_roles` : qui fait quoi, une ligne par membre. Un rôle n'est
  donné qu'à un membre de l'alliance, et un départ efface le rôle.
- `ai_alliance_objectives` : le plan. Un index unique partiel n'autorise qu'un
  plan ouvert par alliance ; les autres sont l'histoire. Le plan référence
  l'opération groupée qu'il a ouverte.

## Journal

L'ouverture et chaque changement d'état d'un plan sont inscrits dans
`game_event_log` sous l'alliance concernée, avec la cible, le quorum et la
raison. Rien de ce qu'un membre sait n'y figure : le journal corrèle, il ne
révèle pas.

## Frontières transactionnelles

Publier une croyance, distribuer les rôles, ouvrir un plan et le faire avancer
sont chacun une transaction. L'avancement est gardé par l'état d'origine, si
bien que deux meneurs ne peuvent pas faire progresser le même plan deux fois.

La délibération collective, elle, n'est pas une transaction : elle lit la
mémoire commune puis agit par les cas d'usage joueurs — invitation, partage,
opération groupée — qui ouvrent chacun la leur.
