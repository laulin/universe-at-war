# Alliances d'intelligences artificielles — règle de fidélité

## Comportement attendu

Une alliance d'IA coopère avec les moyens d'un groupe humain équivalent : ses
membres se disent ce qu'ils ont vu, se répartissent les rôles, choisissent une
cible ensemble et y vont en même temps. Elle ne devient jamais une source
d'omniscience : rien n'entre dans sa mémoire commune qui n'ait été observé par
l'un de ses membres et partagé volontairement.

Toutes les actions passent par les cas d'usage du Milestone 7 — invitation,
partage de rapport, opération groupée. La fiche [`acs.md`](acs.md) reste
l'autorité sur les opérations groupées ; ce document ne décrit que la manière
dont des IA décident de s'en servir.

## Mémoire partagée

La mémoire commune tient quatre sortes de souvenirs :

| Sorte | Contenu | Origine |
| --- | --- | --- |
| `target` | butin et défense vus sur une position | un rapport d'espionnage partagé |
| `threat` | un membre vient d'être attaqué | le rapport de combat de la victime |
| `debris` | un champ de débris | la carte publique |
| `capability` | ce qu'un membre déclare posséder | le membre lui-même |

Chaque souvenir porte sa **provenance** : l'auteur, la date d'observation, une
**confiance** entre 0 et 1, une **expiration**, et le rapport dont il découle
lorsqu'il en découle d'un.

```text
confiance  = fraîcheur(observation)          (voir ai-individual.md)
expiration = observation + 12 × recent_report_seconds
```

Un souvenir issu d'un rapport n'est lisible que tant que ce rapport est
effectivement partagé avec l'alliance. Retirer le partage, quitter l'alliance ou
en être exclu **révoque** immédiatement tout ce qui en découlait : la lecture
rejoint la table des rapports et ne trouve plus rien. Aucune requête ne joint
jamais l'état réel d'un adversaire.

Une IA qui appartient à une alliance partage d'elle-même ses rapports
d'espionnage frais et complets. C'est un acte volontaire, pris avec le cas
d'usage de partage, et réversible.

## Rôles

Le meneur — le membre le plus ancien qui a le droit de conduire une opération —
répartit les rôles à chaque réflexion, à partir des seules capacités déclarées
et de la disponibilité de chacun :

| Rôle | Attribué à |
| --- | --- |
| `scout` | le membre éveillé qui a le plus de sondes |
| `fleeter` | le membre éveillé dont la flotte de combat est la plus forte |
| `recycler` | le membre éveillé qui a le plus de recycleurs |
| `defender` | le membre éveillé dont le sol est le mieux défendu |
| `logistician` | le membre éveillé dont les soutes sont les plus grandes |
| `miner` | tous les autres |

Un même membre ne tient qu'un rôle ; les égalités se départagent par
identifiant, si bien que la répartition est reproductible. Un membre endormi ne
reçoit aucun rôle actif : il reste mineur jusqu'à son réveil. Le meneur ne
range que ceux qui se sont déclarés : un membre qui n'a pas encore réfléchi
n'existe pas encore pour la répartition.

## Objectif collectif

Une alliance ne poursuit qu'un objectif à la fois. Il naît de sa mémoire
commune, porte une cible, une échéance et un quorum.

```text
scouting → assembling → resolved
scouting → abandoned
assembling → abandoned
```

- **scouting** : la cible la mieux notée de la mémoire commune n'a pas encore de
  rapport partagé frais et complet. Les éclaireurs vont la regarder. Aucune
  attaque n'est préparée tant que personne n'a vu.
- **assembling** : le renseignement est là. Le meneur ouvre une opération
  groupée sur la cible **à 10 % de vitesse** : sa propre flotte rampe, et c'est
  précisément ce qui laisse aux alliés le temps de la rejoindre. Les fleeters
  s'y engagent à pleine vitesse et se synchronisent sur elle.
- **resolved** : l'opération est arrivée.
- **abandoned** : l'échéance passe sans quorum, la cible disparaît, ou plus
  personne ne peut y aller. Le meneur retire alors sa propre flotte, ce qui
  annule l'opération quand la dernière part.

```text
quorum   = max(2, membres_éveillés / 2)
échéance = ouverture + 6 × intervalle_de_réflexion_du_meneur
```

L'échéance ne borne que l'attente d'un renseignement : dès que les flottes sont
en route, l'opération suit sa propre heure d'arrivée. Le meneur ne renonce alors
que si le quorum n'est toujours pas atteint à la dernière réflexion avant
l'atterrissage — il retire alors sa flotte, ce qui annule l'opération.

Le score d'une cible reprend celui d'une IA seule, appliqué aux préférences du
meneur, avec la confiance du souvenir partagé en facteur supplémentaire : une
information de seconde main vaut moins qu'un rapport que l'on a soi-même
rapporté.

## Défense d'un membre

Un membre attaqué publie une menace dans la mémoire commune, tirée de son propre
rapport de combat. Tant que cette menace est fraîche, **tous les membres éveillés qui ont des
vaisseaux** vont stationner individuellement sur le corps visé, par une mission
de maintien, pour trois heures. La défense n'invente pas de groupe ACS : le
plan reste en `assembling` pendant sa fenêtre de surveillance puis passe en
`resolved`. Une mission déjà en route vers ce corps empêche un second départ.

Un membre sans vaisseau, endormi, ou dont l'emplacement de flotte est déjà pris,
ne part pas : il enregistre la raison. La victime elle-même ne se porte pas
secours : son corps est déjà le sien. Une alliance qui n'a pas les moyens de
défendre ne défend pas.

## Ce qu'une alliance ne vise jamais

Aucun membre n'espionne ni n'attaque un corps de sa propre alliance. Les noms de
ses membres et les positions qu'ils ont déclarées sont écartés des cibles avant
tout calcul, y compris lorsqu'un vieux rapport en parle encore.

## Conflit entre objectif individuel et collectif

Tant que l'alliance tient une cible, aucun membre n'ouvre sa propre attaque
dessus : deux flottes de la même équipe ne se courent pas après. Le reste de la
carte lui reste ouvert.

Le collectif passe avant, mais ne consomme jamais l'économie d'un membre : la
construction, la recherche et le chantier suivent toujours les règles
individuelles. Seule la couche opérationnelle change de maître. Un membre sans
rôle actif se conduit exactement comme une IA seule.

## Observabilité

La page d'administration d'un joueur artificiel montre, sous une bannière qui
dit ce qu'elle est, l'alliance à laquelle il appartient, le rôle qu'il tient, le
plan en cours et **chaque croyance de la mémoire commune avec sa provenance** :
auteur, date d'observation, expiration, confiance et rapport d'origine. Aucun
joueur n'y a accès.

Le journal du serveur enregistre l'ouverture et chaque changement d'état d'un
plan sous l'alliance concernée, avec la cible et le quorum, et rien de ce qu'un
membre sait.

## Cas limites et invariants

Un rapport non partagé reste invisible aux autres membres. Un souvenir périmé ne
déclenche rien. Deux univers de mêmes seeds produisent le même plan collectif.
Une alliance n'attaque pas sans qu'un de ses membres ait réellement espionné.
Une opération sans quorum est abandonnée plutôt que menée à trois contre une
flotte. Rien n'est créé : ni ressource, ni vaisseau, ni connaissance.

## Tests de référence

Rapport non partagé invisible ; provenance et expiration ; plan reproductible à
seeds égales ; campagne de sondage avant l'attaque ; opération groupée résolue
avec deux IA ; membre endormi, flotte perdue ou cible déplacée ; défense d'un
membre et refus faute de moyens ; interdiction inter-alliance ; conservation des
ressources et des vaisseaux ; clôture d'une surveillance défensive sans groupe
ACS et absence de mission de maintien dupliquée.
