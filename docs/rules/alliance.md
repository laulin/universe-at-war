# Alliances — règle de fidélité

## Comportement attendu

Une alliance est une équipe persistante. Elle coordonne, elle renseigne, elle
frappe ensemble, mais elle ne met jamais les ressources en commun : un membre
qui veut donner du métal envoie un transport, comme tout le monde.

Un joueur appartient à une alliance au plus. Le rôle qu'il y tient ne lui donne
aucun pouvoir sur le serveur : un chef d'alliance n'est ni administrateur, ni
modérateur.

## Identité

Le nom compte de 3 à 32 caractères et le tag de 2 à 8 caractères parmi les
lettres majuscules et les chiffres. Les deux sont uniques dans l'univers, sans
distinction de casse pour le nom. La description est libre et peut être vide.

## Rôles et permissions

| Rôle | Inviter | Exclure | Modifier le profil | Diplomatie | Mener une opération |
| --- | :---: | :---: | :---: | :---: | :---: |
| `founder` | oui | oui | oui | oui | oui |
| `officer` | oui | membres seulement | non | non | oui |
| `member` | non | non | non | non | non |

Une exclusion ne remonte jamais la hiérarchie : un officier n'exclut ni un autre
officier, ni le fondateur. Le fondateur ne peut pas quitter son alliance sans
avoir transmis sa charge à un autre membre, ce qui évite une alliance sans chef.
Une alliance dont le dernier membre part est dissoute.

## Invitations

Une invitation vise un joueur sans alliance, porte une date d'expiration et
n'existe qu'une fois à la fois pour un couple alliance-joueur. L'acceptation et
le refus sont idempotents : rejouer la même réponse ne change rien et ne produit
pas d'erreur. Une invitation expirée ne s'accepte plus. Rejoindre une alliance
annule les autres invitations en attente du joueur.

Deux acceptations simultanées de deux invitations différentes ne font entrer le
joueur que dans une seule alliance : l'unicité de l'adhésion arbitre.

## Diplomatie

Lorsque le ruleset l'autorise, le fondateur déclare une relation vers une autre
alliance : `pact` ou `war`. La relation est unilatérale et déclarative : elle
annonce une intention, elle ne modifie aucune règle de combat. Chaque
déclaration est horodatée et conservée dans l'historique, si bien qu'on peut
toujours dire qui a déclaré quoi et quand.

## Historique

Toute mutation notable est écrite dans l'historique de l'alliance : création,
invitation, adhésion, départ, exclusion, changement de rôle, transmission de la
charge, déclaration diplomatique, partage de rapport. L'historique est
append-only et lisible par les membres.

## Partage de renseignement

Un rapport appartient à son destinataire. Il peut le partager avec son alliance,
explicitement, rapport par rapport. Un membre lit alors le rapport tel qu'il a
été écrit pour son destinataire : le partage ne révèle rien de plus que ce que
le propriétaire possédait. Quitter l'alliance coupe l'accès aux rapports qu'on y
avait partagés comme à ceux qu'on y lisait.

## Cas limites et invariants

Un joueur sans empire ne rejoint pas d'alliance. Une invitation vers un joueur
déjà membre est refusée. Un non-membre ne lit ni les membres, ni l'historique,
ni les rapports partagés d'une alliance. Aucune ressource, aucune flotte et
aucune technologie ne circule automatiquement entre membres.

## Tests de référence

Table des permissions par rôle ; invitations concurrentes et double acceptation ;
expiration ; refus idempotent ; exclusion d'un officier par un officier ; départ
du fondateur sans successeur ; dissolution au départ du dernier membre ;
isolation complète entre deux alliances ; lecture refusée d'un rapport non
partagé.
