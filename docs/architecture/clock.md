# Horloge testable

Le domaine reçoit toujours l'instant effectif depuis l'application.

```go
type Clock interface {
	Now() time.Time
}
```

`SystemClock` retourne une valeur UTC. `FakeClock`, réservé aux tests et outils
de développement protégés, peut être figé et avancé sans `sleep`.

Les calculs reçoivent explicitement un intervalle `[lastSettledAt, now]`. Ils
refusent un temps antérieur, appliquent production et plafonds puis retournent
un nouveau snapshot sans lire l'horloge. Les pauses d'univers seront traduites
par une horloge d'univers/politique applicative, jamais par une réécriture des
timestamps historiques.

