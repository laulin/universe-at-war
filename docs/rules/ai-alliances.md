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
| `miner` | tous les autres |

Un même membre ne tient qu'un rôle ; les égalités se départagent par
identifiant, si bien que la répartition est reproductible. Un membre endormi ne
reçoit aucun rôle actif : il reste mineur jusqu'à son réveil.

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
  groupée sur la cible ; les fleeters y engagent leur flotte.
- **resolved** : l'opération est arrivée.
- **abandoned** : l'échéance passe sans quorum, la cible disparaît, ou plus
  personne ne peut y aller. Le meneur retire alors sa propre flotte, ce qui
  annule l'opération quand la dernière part.

```text
quorum   = max(2, membres_éveillés / 2)
échéance = ouverture + 6 × intervalle_de_réflexion_du_meneur
```

Le score d'une cible reprend celui d'une IA seule, appliqué aux préférences du
meneur, avec la confiance du souvenir partagé en facteur supplémentaire : une
information de seconde main vaut moins qu'un rapport que l'on a soi-même
rapporté.

## Défense d'un membre

Un membre attaqué publie une menace dans la mémoire commune, tirée de son propre
rapport de combat. Tant que cette menace est fraîche, les défenseurs éveillés
qui ont des vaisseaux vont stationner sur le corps visé, par le cas d'usage de
défense groupée du Milestone 7, jusqu'à la fin de la fenêtre.

Un défenseur sans vaisseau, endormi, ou dont l'emplacement de flotte est déjà
pris, ne part pas : il enregistre la raison. Une alliance qui n'a pas les moyens
de défendre ne défend pas.

## Conflit entre objectif individuel et collectif

Le collectif passe avant, mais ne consomme jamais l'économie d'un membre : la
construction, la recherche et le chantier suivent toujours les règles
individuelles. Seule la couche opérationnelle change de maître. Un membre sans
rôle actif se conduit exactement comme une IA seule.

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
ressources et des vaisseaux.
