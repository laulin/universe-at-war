# Diagnostic et plan de correction des retours de flotte bloqués

Date : 28 octobre 2026. Signalement : compte `KaLiMH3R0`, joueur `KaLiM`.

Implémentation : branche `fix/stuck-fleet-returns`. Le
[compte rendu de validation](../testing/fleet-return-fix-2026-10-28.md) décrit
la récupération réussie sur une copie isolée et les contrôles automatisés.
La vérification et la reprise du serveur réel restent à effectuer au déploiement.

Le retour bloqué retrouvé dans `debug.universe-at-war.db` a la même cause que
la construction restée à 100 %. Le code actuel corrige déjà cette cause et
contient une migration de reprise. La priorité est de vérifier leur présence
sur le serveur utilisé par le joueur, puis de valider la récupération des
vaisseaux par le traitement normal des événements.

## Incident confirmé dans la sauvegarde

La base de diagnostic a été consultée en lecture seule. Elle est au schéma 23,
antérieur à la migration de reprise 24 ; elle ne prouve pas l'état actuel du
serveur.

| Élément | Valeur |
| --- | --- |
| Joueur | ID 16, compte `KaLiMH3R0`, nom affiché `KaLiM` |
| Flotte | ID 27141, mission `recycle`, origine 16, état `returning` |
| Composition | 230 recycleurs encore dans `fleet_ships` |
| Cargaison | 3 220 800 métal, 0 cristal, 0 deutérium |
| Arrivée sur cible | 26 octobre 2026 à 14:22:49 UTC, événement 1218297 terminé |
| Retour attendu | 26 octobre 2026 à 14:22:57 UTC, soit 15:22:57 à Paris |
| Événement de retour | ID 1218313, `fleet_returned`, `pending`, 5 tentatives |
| Dernière erreur | `economy: production time cannot move backwards` |

L'historique s'arrête à `outbound → returning`, avec la raison `recycled`.
Les vaisseaux sont encore attachés à la mission : le retour n'a pas été
appliqué. Cette flotte continue d'occuper un emplacement puisque
`fleetsInFlight` et `activeFleetCount` incluent l'état `returning`.

La construction 27134, sur la planète 175 du même joueur, est également active
avec son événement 450722 arrêté à cinq tentatives sur la même erreur.
Sur l'ensemble de cette copie, une seule flotte en retour ou rappel possède
un événement absent, terminal ou arrivé à la limite des tentatives : la flotte
27141. Aucun retour sans événement n'a été trouvé.

## Cause et correctifs existants

1. Consulter une planète calcule et persiste sa production à l'instant présent.
2. Le gestionnaire `resolveReturn` charge l'origine à `event.DueAt`, l'échéance
   historique du retour. Si la planète a été consultée après cette échéance,
   l'ancien calcul de production refuse de revenir en arrière.
3. La transaction du retour est annulée : ni vaisseaux ni cargaison ne sont
   transférés, et la flotte reste `returning`.
4. `EventProcessor.CompleteDue` retente l'événement, puis l'exclut dès que
   `attempts >= MaxAttempts` ; la limite par défaut est cinq. L'événement reste
   `pending` et un simple rafraîchissement ne le débloque plus.

Le commit `4f03ab5` du 27 octobre modifie le chargement économique partagé dans
`internal/storage/sqlite/economy.go` : la production est calculée à
`max(échéance demandée, produced_at)`. Cela couvre aussi `resolveReturn`, qui
utilise ce chargement. Le commit `f9d7d9b` compare par ailleurs les échéances
chronologiquement ; il évite un retard de sélection lié aux fractions de
seconde, sans réactiver les événements épuisés.

Le commit `0b4b33a` ajoute
`migrations/0024_retry_late_settlement_events.sql`. Il remet à zéro les
tentatives des événements `pending` dont l'erreur correspond exactement à
celle constatée. Il concerne tous les types, dont `fleet_returned`, et ne
modifie pas les événements échoués pour une autre raison.

Le démarrage `serve` appelle déjà `database.Migrate` avant de lancer les
services. Une instance utilisant le code actuel doit donc appliquer cette
reprise au démarrage si sa base est encore au schéma 23.

## Étape 1 Vérifier le serveur et préparer la reprise

- [ ] Identifier le binaire exécuté et le chemin de sa base. Vérifier la
  présence des trois commits ci-dessus et de la migration 24 dans
  `schema_migrations`. Ne pas déduire ces valeurs de la copie locale.
- [ ] Retrouver le joueur par son compte, puis relever ses flottes actives,
  leurs événements, compositions, cargaisons et inventaires d'origine.
  Les IDs locaux servent de repères, sans remplacer cette recherche.
- [ ] Examiner aussi les autres joueurs pour détecter des retours échus
  sans événement, avec événement terminal ou avec tentatives épuisées.
- [ ] Avant la reprise, prendre une sauvegarde cohérente de la base réelle
  avec la commande de sauvegarde existante.

Si l'erreur réelle est celle constatée et la migration manque, déployer le
code corrigé et laisser appliquer la migration 24. Si elle a déjà été
appliquée et que la flotte reste bloquée, relever la nouvelle erreur et
vérifier qu'un ancien processus n'écrit pas encore dans la base. Traiter cette
cause avant toute nouvelle relance.

Critère de sortie : version, base et événements réellement concernés sont
identifiés ; la reprise choisie correspond à leur erreur actuelle.

## Étape 2 Renforcer la validation de la récupération

Fichiers : `tests/late_settlement_test.go` et
`internal/storage/sqlite/database_test.go`.

Les tests couvrent maintenant le parcours complet de reprise, en plus du
retour après une consultation tardive et de la remise à zéro par migration.
Le test de migration inclut les événements `fleet_returned` et les cas qui
doivent rester inchangés.

- [x] Ajouter au test de migration un `fleet_returned` épuisé portant
  l'erreur connue. Vérifier la remise à zéro et l'effacement de l'erreur,
  ainsi que la préservation des événements terminés, annulés ou échoués
  pour une autre cause.
- [x] Ajouter un scénario d'intégration de reprise : une flotte de recyclage
  en retour avec cargaison, une origine déjà calculée après son échéance,
  un événement à cinq tentatives, puis reprise par la migration et passage
  du processeur. Vérifier que l'événement est ignoré avant la reprise.
- [x] Après traitement, vérifier `completed` sur la flotte et l'événement,
  vaisseaux ajoutés exactement une fois à l'origine, cargaison créditée selon
  les capacités de stockage existantes, soute vidée et emplacement libéré.
  Rejouer le retour pour vérifier l'absence de double crédit.
- [x] Étendre le cas de consultation tardive aux flottes rappelées et aux
  retours vers une lune, qui utilisent le même gestionnaire.

Critère de sortie : le parcours complet d'une flotte déjà bloquée est couvert,
en plus de la prévention de nouveaux blocages. Réutiliser la migration 24 ;
aucune nouvelle migration n'est justifiée par le diagnostic actuel.

## Étape 3 Valider le résultat après déploiement

- [ ] Laisser le worker traiter les événements réactivés et vérifier leurs
  nouvelles erreurs éventuelles. Il peut aussi achever les constructions
  bloquées et promouvoir les ordres suivants.
- [ ] Contrôler la restitution des 230 recycleurs si le cas réel correspond
  toujours à la sauvegarde. Comparer les mouvements et le journal aux
  inventaires relevés juste avant la reprise, en tenant compte des autres
  actions du joueur.
- [ ] Vérifier le crédit de cargaison selon les règles de capacité du retour,
  la disparition de la mission de la page flotte et la libération du slot.
- [ ] Vérifier que les compteurs d'événements échoués du tableau de bord
  diminuent et qu'aucun nouveau retour n'épuise ses tentatives.

Faire appliquer le retour par son gestionnaire transactionnel. Passer
directement la flotte à `completed` laisserait les vaisseaux et la cargaison
hors de l'inventaire ; ajouter manuellement les unités exposerait à un double
crédit lors d'une reprise ultérieure.

## Amélioration distincte du rafraîchissement de la page flotte

Avant correction, `web/templates/fleet.html` conserve `data-refresh="page"` sur le compteur
d'arrivée même pour une flotte déjà en retour, tandis que le compteur de
retour ne le porte pas. `web/static/app.js` demande un rechargement deux
secondes après toute échéance marquée comme dépassée. La page peut donc
recharger continuellement pendant le retour, sur l'ancienne arrivée.
Cela n'explique pas l'événement épuisé retrouvé en base.

- [x] Dans `internal/web/fleet.go` et le template, choisir l'échéance à
  rafraîchir selon l'état : arrivée pour `outbound`, fin de garde pour
  `holding`, retour pour `returning` et `recalled`.
- [x] Garder les anciennes heures visibles sans leur attribuer un
  rafraîchissement automatique. Vérifier dans les tests HTTP que les quatre
  états ne rafraîchissent que leur prochaine étape. Vérifier aussi dans Chrome
  l'absence de rechargement avant l'échéance, puis le rechargement à échéance.

## Vérification effectuée pendant cette analyse

La commande suivante passe sur le code actuel dans les deux packages. Elle
couvre les régressions de calcul tardif, la migration de reprise, le traitement
des événements et plusieurs scénarios de transport et de rappel.

```sh
go test ./tests ./internal/storage/sqlite -run 'Test(AFleetReturningToAPlanetSettledMeanwhileStillLands|ABuildingDueBeforeAPlayerSettlementStillCompletes|LateSettlementMigrationRetriesOnlyTheEventsItPoisoned|Recall.*|.*Transport.*|.*Event.*)' -count=1
```

Le scénario complet de récupération de la sauvegarde a ensuite été exécuté
avec succès sur une copie isolée ; voir le compte rendu de validation.
La base de diagnostic originale est conservée sans modification.
