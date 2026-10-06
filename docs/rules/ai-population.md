# Population des joueurs artificiels

## Comportement attendu

`ai.total` est la cible de population **active**. Le worker réconcilie cette
cible par petits lots : il termine d'abord les créations interrompues, puis
fait arriver de nouveaux joueurs tant que le nombre d'IA actives est inférieur
à la cible.

Un profil retiré reste conservé pour l'historique d'administration, mais il ne
consomme plus une place active. Une nouvelle identité, un nouvel empire et une
nouvelle graine prennent sa place. L'ancien profil n'est jamais réactivé.

## Départs

Une IA quitte automatiquement la partie lorsqu'elle ne possède plus aucun
corps. Ce constat ne dépend ni de sa richesse, ni de sa flotte, ni de son score :
un empire momentanément pauvre doit pouvoir se reconstruire.

Le départ :

- passe le profil à `retired` et désactive son compte ;
- annule sa prochaine réflexion ;
- le retire de son alliance ;
- transmet la charge de fondateur au membre restant le plus ancien, en donnant
  priorité à un officier ;
- dissout l'alliance si l'IA en était le dernier membre.

Un retrait demandé par un administrateur suit exactement ce même chemin. Les
mondes d'un retrait administratif ne sont pas supprimés : le retrait ne réécrit
pas rétroactivement la carte.

## Arrivées

À chaque passage, au plus `PopulationBatch` nouveaux profils sont créés. Le
prochain rang historique choisit leur nom et leur zone d'implantation, ce qui
évite de ressusciter un ancien joueur ou d'empiler les remplaçants au même
endroit. Les alliances configurées sont complétées avant les indépendants.

Si la population active dépasse la cible à la suite d'ajouts manuels ou d'une
baisse de configuration, aucun empire viable n'est expulsé arbitrairement. Les
départs normaux ramènent progressivement la population vers la cible et aucun
remplaçant n'arrive tant qu'elle reste atteinte ou dépassée.

## Administration

La page `/admin/ai` est le point d'entrée recommandé : son bloc « Population
automatique » affiche les IA actives, la cible, les arrivées encore attendues et
les profils retirés. L'administrateur ne saisit que le nombre d'IA actives
souhaité.

La validation publie une nouvelle version complète du ruleset avec une
justification d'audit, puis réveille immédiatement le worker. Le premier petit
groupe peut donc arriver sans attendre son balayage périodique, tandis que le
reste conserve le rythme progressif normal. Une version cachée dans le
formulaire empêche un ancien onglet d'écraser une modification plus récente.

Si une cible réduite ne peut plus contenir les alliances et le minimum
d'indépendants configurés, le formulaire réduit d'abord le nombre d'alliances
entières puis le minimum d'indépendants. Les autres paramètres — difficulté,
horaires, fréquence et taille d'alliance — sont conservés. Ils restent
modifiables depuis les réglages avancés.

La création nominative d'une IA reste disponible dans une section avancée.
Elle est volontairement secondaire : elle peut dépasser la cible et sert aux
personnages précis, aux démonstrations et au diagnostic, pas au peuplement
ordinaire.

## Cas limites et tests de référence

- une cible nulle ne crée personne ;
- un univers arrêté ne crée ni ne retire personne ;
- une création interrompue après la fondation de l'empire est terminée avant
  toute nouvelle naissance ;
- un retrait conserve l'ancien profil et crée une identité distincte ;
- la disparition du dernier monde retire le profil et déclenche un remplaçant ;
- le départ d'un fondateur ne laisse ni membre fantôme ni alliance sans chef.

Ces scénarios sont couverts par `tests/ai_population_test.go`.
