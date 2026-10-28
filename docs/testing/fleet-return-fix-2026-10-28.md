# Vérification des retours de flotte bloqués

Date : 28 octobre 2026. Branche : `fix/stuck-fleet-returns`.

## Correction et reprise

La flotte 27141 de `KaLiMH3R0` reste en retour dans la sauvegarde de diagnostic
car son événement 1218313 a épuisé cinq tentatives sur l'erreur
`economy: production time cannot move backwards`. La même erreur bloque sa
construction 27134.

La correction économique `4f03ab5` et la migration de reprise 24, présentes
dans la branche, couvrent déjà cette cause. La migration existante est
conservée avec son checksum. Elle sera appliquée au démarrage du serveur si
elle manque dans la base utilisée.

Les nouveaux tests vérifient ensemble la reprise d'un recyclage épuisé,
la restitution des vaisseaux et de la cargaison, la libération du slot et
l'absence de double crédit après redélivrance. Ils couvrent également les
retours normaux et rappelés vers une planète ou une lune. Le test de migration
vérifie les événements de flotte et préserve les événements terminés,
annulés ou échoués pour une autre cause.

La page flotte rafraîchit désormais l'arrivée pendant l'aller, la fin du
stationnement pendant la garde, et le retour pour une flotte en retour ou
rappelée. Une arrivée historique ne déclenche plus des rechargements pendant
tout le trajet de retour. Les tests HTTP vérifient ces quatre états et la
disparition d'une flotte après son retour.

Dans Chrome sans interface graphique, les quatre pages rendues ont été
exécutées avec le vrai `web/static/app.js` et une horloge serveur contrôlée.
Sur une fenêtre de sept secondes simulées, chaque page reste chargée une seule
fois lorsque son échéance est encore à venir. À échéance, elle recharge une
fois ; la réponse suivante simule la transition appliquée et ne boucle pas.
Ces huit vérifications couvrent aller, stationnement, retour et rappel.

## Reprise de la flotte du joueur sur une copie isolée

Une copie cohérente de `debug.universe-at-war.db` a été créée avec l'API de
sauvegarde SQLite depuis une connexion en lecture seule. Sur cette copie
jetable, les autres événements en attente ont été annulés pour isoler le
retour et éviter que d'autres actions de simulation modifient les stocks.
La migration normale a ensuite été appliquée et le processeur d'événements
exécuté avec l'horloge fixée au dernier calcul économique de l'origine,
le 27 octobre à 08:23:57 UTC.

| Contrôle | Résultat |
| --- | --- |
| État de la flotte 27141 | `returning → completed` |
| Événement 1218313 | `completed`, une tentative après réactivation |
| Recycleurs sur l'origine | 286 → 516, soit les 230 attendus |
| Métal sur l'origine | 39 847 300 → 43 068 100, soit 3 220 800 crédités |
| Emplacements de flotte utilisés par le joueur | 0 après le retour |
| Journal du retour | Une entrée |
| Redélivrance du retour | Aucun crédit supplémentaire de vaisseaux ou ressources |

La base source conserve la flotte en état `returning` et son événement à cinq
tentatives avec l'erreur initiale. Le serveur réel n'a pas été modifié : sa
version et ses migrations restent à vérifier avant déploiement.

## Contrôles automatisés

- `go test -timeout 30m ./...` : réussi.
- `go vet ./...` : réussi.
- Formatage Go et `git diff --check` : réussis.
- `staticcheck ./internal/web ./internal/storage/sqlite ./tests` : réussi.
- `go test -race -timeout 5m ./tests ./internal/storage/sqlite ./internal/web
  -run 'Test(WebFleet|LateSettlementMigration|LateFleetReturns|AFleetReturning|Recall)'
  -count=1` : réussi.
- `staticcheck ./...` : signale l'erreur préexistante `SA4006` dans
  `internal/ai/military.go:76`, sans modification de ce fichier par ce correctif.

Le détecteur de concurrence couvre les scénarios de flotte et de reprise du
correctif. La suite complète a été vérifiée sans instrumentation.
