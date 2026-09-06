# Budgets de performance et observabilité

Toutes les mesures ci-dessous viennent des bancs du dépôt, sur une machine de
développement (Xeon E5-2697 v2, SQLite en WAL, base sur disque local). Elles
sont des ordres de grandeur, pas des garanties : ce qui compte est qu'aucune
d'elles ne dépend du nombre de joueurs connectés.

| Banc | Ce qu'il mesure | Mesure | Budget |
| --- | --- | ---: | --- |
| `BenchmarkSettleLazy` | un règlement paresseux de production | 267 ns, 0 allocation | < 10 µs |
| `BenchmarkResolveLargeBattle` | 100 000 contre 100 000 vaisseaux | 133 ms, 94 allocations | < 2 s |
| `BenchmarkFleetLaunch` | un lancement complet en base | 3,2 ms | < 50 ms |
| `BenchmarkReportsPage` | la page rapports sur 10 000 rapports | 0,85 ms | < 50 ms |
| `BenchmarkGalaxyPage` | un système plein avec débris partout | 1,4 ms | < 50 ms |
| `BenchmarkEventProcessorBacklog` | un arriéré d'événements planifiés | 438 ms | < 2 s |
| `BenchmarkEventStorm` | 2 000 événements dus d'un coup | 880 ms, soit 0,44 ms par événement | < 2 ms par événement |
| `BenchmarkArtificialReflections` | un cycle complet de réflexion d'une IA | 19 ms | < 100 ms |

Pour les rejouer :

```sh
go test -bench=. -benchtime=20x -run=^$ ./internal/domain/... ./tests/
go test -bench=BenchmarkResolveLargeBattle -run=^$ -benchmem ./internal/domain/combat/
```

Profils CPU et mémoire sur les charges lourdes :

```sh
go test -bench=BenchmarkEventStorm -run=^$ -cpuprofile cpu.out -memprofile mem.out ./tests/
go tool pprof -top cpu.out
```

## Au repos

Entre deux échéances, le worker dort : il calcule la prochaine échéance depuis
la base et attend jusque-là, ou jusqu'à un réveil explicite. Aucune boucle
active ne tourne, ce que prouve
`TestScheduleKeepsTheWorkerAsleepBetweenReflections` : après une réflexion, plus
rien n'est dû et le prochain événement est dans le futur.

## Compteurs

Le processus tient des compteurs en mémoire, lisibles par un administrateur sur
`/admin/metrics` en texte brut :

```text
requests 128
average_request_ms 2.40
server_failures 0
events 512
event_failures 0
reflections 24
status_200 120
status_303 8
```

Ils ne contiennent que des nombres : ni chemin, ni compte, ni jeton. Les
journaux structurés portent la corrélation (`request_id`, méthode, chemin,
statut, durée) et jamais un secret : ni cookie, ni chaîne de requête, ni mot de
passe. `UAW_LOG_LEVEL` choisit le niveau.
