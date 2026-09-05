# ADR 0001 — Choix du socle

Statut : accepté.

## Décisions

- Go 1.27.x minimum, conformément à la spécification.
- `net/http`, `html/template`, `embed` et `log/slog` pour limiter le runtime.
- `database/sql` avec `modernc.org/sqlite`, sans CGO.
- SQLite en WAL, lectures concurrentes et un chemin d'écriture sérialisé.
- HTML rendu serveur, CSS natif et JavaScript vanilla facultatif.
- Argon2id via `golang.org/x/crypto` pour les credentials.
- horloge et RNG injectées aux frontières déterministes.
- état relationnel authoritative complété par deux journaux append-only, sans
  event sourcing complet.

## Conséquences

Le binaire final n'a besoin d'aucun service externe. La concurrence d'écriture
est volontairement bornée. Toute dépendance supplémentaire devra réduire un
risque concret, être maintenue, compatible avec la distribution ciblée et
documentée dans un ADR.

