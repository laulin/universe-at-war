# Milestone 10 — matrice des exigences restantes

Chaque ligne cite une exigence encore ouverte de la spécification ou du brief,
dit où elle est tenue et ce qui le prouve. Une ligne sans preuve est une
approximation silencieuse : il n'y en a aucune ici. Les lignes marquées
**reporté** disent explicitement ce qui n'est pas livré et pourquoi.

## 1. Expéditions (spec §9, brief 1)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Mission, durée, emplacement et conditions explicites | `internal/domain/fleet`, `docs/rules/expeditions.md` | `TestExpeditionsAreBoundedBySlotsAndHold` |
| Table de résultats configurable et pondérée | `internal/domain/expedition`, `rules.ExpeditionSettings` | `TestOutcomeTableFollowsItsWeights` |
| Tirage déterministe à seed persistée | `fleets.seed`, `internal/domain/expedition` | `TestTheSameSeedGivesTheSameExpeditionOutcome` |
| Application atomique et retour programmé | `internal/storage/sqlite/expedition.go` | `TestExpeditionFliesWaitsAndComesBack` |
| Rapport immuable | table `reports`, kind `expedition` | `TestExpeditionFliesWaitsAndComesBack` |
| Limites empêchant de remplacer le PvP | emplacements d'astrophysique, plafond de soute | `TestExpeditionsAreBoundedBySlotsAndHold` |

## 2. Profils de règles (spec §10-11, brief 2)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Profils fournis (classique, lent, accéléré, conflit, coopératif, PvPvE) | `internal/domain/rules/profiles.go` | `TestEveryProfileIsValidAndDistinct` |
| Charger, sauvegarder, comparer, importer, exporter avec version | `internal/app/setup`, `/setup/profiles` | `TestProfilesLoadCompareImportAndExport` |
| Aperçu des différences avant activation | `/setup/profiles` | `TestWebProfilePageShowsTheDifferencesBeforeActivation` |
| Validateur unique partagé | `catalogue.ValidateRuleset` | `TestACorruptOrFutureDocumentChangesNothing` |
| Non-rétroactivité | `ruleset_version` capturée par événement | `TestScenarioJRulesetChangeKeepsRunningFleetsUnchanged` |

## 3. Inscriptions (spec §36, brief 3)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Politiques fermée, ouverte, sur invitation | `internal/app/registration` | `TestRegistrationFollowsTheRulesetPolicy`, `TestInvitationLetsExactlyOnePlayerIn` |
| Invitation à usage unique, rejeu refusé | table `invitations`, `appadmin.InvitationService` | `TestInvitationLetsExactlyOnePlayerIn` |
| Limitation d'abus | limiteur dédié à l'inscription, quelques fautes pardonnées | `TestWebRegistrationIsRateLimited` |
| Messages sans énumération | message unique pour tout refus | `TestWebRegistrationRefusalsAreIndistinguishable` |
| Création concurrente sûre | contrainte UNIQUE | `TestConcurrentRegistrationsOfTheSameNameCreateOneAccount` (existant) |

## 4. Administration et modération (spec §37-42, §61, brief 4)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Tableau de bord (état, DB, backlog, débit, joueurs, erreurs, ruleset) | `/admin` | `TestDashboardShowsTheHealthOfTheUniverse` |
| Gestion comptes, rôles et statuts | `/admin` | `TestDashboardShowsTheHealthOfTheUniverse` |
| Avertissement permanent si l'admin joue | bandeau de la page d'administration | `TestWebAdministrationIsReservedAndWarnsAPlayingAdministrator` |
| Modérateur limité aux joueurs et aux sanctions | `internal/app/moderation` | `TestModeratorCannotReachAdministration` |
| Ban avec durée, justification, levée et audit | `internal/app/moderation`, table `bans` | `TestScenarioIBanBlocksLoginNotEmpire` |
| Un ban n'arrête pas l'empire | aucune action sur les files | `TestScenarioIBanBlocksLoginNotEmpire` |

## 5. Sauvegardes et maintenance (spec §46, brief 5)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Commande `backup` et action admin | `internal/cli`, `/admin` | `TestBackupProducesAVerifiedRestorableCopy` |
| Nom horodaté, intégrité vérifiée | `internal/storage/sqlite/backup.go` | `TestBackupProducesAVerifiedRestorableCopy` |
| Rétention optionnelle | option `--keep` | `TestBackupRetentionKeepsTheNewest` |
| `doctor` étendu (écriture, WAL, clés étrangères, ruleset) | `internal/cli`, `Database.Diagnose` | `TestBackupProducesAVerifiedRestorableCopy` |
| Refus d'une sauvegarde d'un schéma futur | `VerifyBackup`, `doctor` | `TestABackupFromAFutureSchemaIsRefused` |
| Procédure de restauration documentée | `docs/operations/backups.md` | revue documentaire |

## 6. UX et accessibilité (spec §52-54, brief 6)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Écrans obligatoires et navigation globale | `web/templates` | `TestEveryPlayerScreenHoldsItsAccessibilityBasics` |
| Horodatage serveur, comptes à rebours non autoritatifs | `web/static/app.js` (existant) | `TestGamePagesShareLayoutWithoutInlineScripts` |
| Rafraîchissement ciblé sans dépendance | `data-refresh` sur l'échéance qui change la page, repli : temps absolus rendus par le serveur | `TestPagesAskForOneTargetedRefresh` |
| Responsive, focus visible, lien d'évitement, labels | `web/static/app.css`, `layout.html` | `TestEveryPlayerScreenHoldsItsAccessibilityBasics` |
| Hostiles jamais signalés par la seule couleur | icône et texte | `TestEveryPlayerScreenHoldsItsAccessibilityBasics` |
| Textes UI séparés des identifiants | `internal/web/labels` | audit de code |
| Assets embarqués sans pipeline Node | `web/assets.go` (existant) | `go build` |

**Reporté :** le flux SSE n'est pas livré. La spécification l'autorise mais ne
l'exige que pour « signaler un rafraîchissement ciblé, avec repli lent ». Le
repli — un en-tête de rafraîchissement et le rechargement de page — est livré et
testé ; le canal temps réel est laissé à une version ultérieure plutôt que
livré à moitié.

## 7. Performance, observabilité et release (spec §56, §62, brief 7)

| Exigence | Où | Preuve |
| --- | --- | --- |
| Métriques internes, lisibles sur `/admin/metrics` | `internal/observability/metrics.go` | `TestMetricsCountWhatWentThroughAndNothingElse` |
| Logs et compteurs sans secrets | `internal/web/middleware.go`, `internal/observability` | `TestRequestLoggerCorrelatesWithoutLeakingSecrets`, `TestLogsAndCountersNeverCarryASecret` |
| Bancs production, événements, combat, galaxie, IA | `tests/*_bench_test.go`, `internal/domain/*` | `docs/operations/performance.md` |
| Aucune boucle active au repos | `Worker` (existant) | `TestScheduleKeepsTheWorkerAsleepBetweenReflections` |
| Budgets documentés | `docs/operations/performance.md` | revue documentaire |
| Matrice de builds et notes de version | `Makefile`, `docs/operations/release.md` | cross-builds |

## Scénarios de la spécification (§63)

| Scénario | Test |
| --- | --- |
| A premier boot | `tests/bootstrap_test.go` |
| B économie | `tests/economy_test.go` |
| C construction | `tests/economy_test.go` |
| D flotte | `TestScenarioDTransportRoundTrip` |
| E attaque | `TestScenarioEFromEspionageToRecycling` |
| F crash avant commit | `TestScenarioFCrashBeforeCommitReplaysOnce` |
| G ACS | `TestScenarioGGroupedAttackIsOneBattle` |
| H IA hors ligne | `TestScenarioHAnOfflineArtificialPlayerReactsLegitimately` |
| I modération | `TestScenarioIBanBlocksLoginNotEmpire` |
| J ruleset non rétroactif | `TestScenarioJRulesetChangeKeepsRunningFleetsUnchanged` |
