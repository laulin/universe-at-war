package tests

import (
	"testing"

	appeconomy "universeatwar/internal/app/economy"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

// world wires the same services as the serve command so that acceptance tests
// exercise the production composition instead of a parallel assembly.
type world struct {
	Database *storagesqlite.Database
	Clock    *appclock.Fake
	Events   *storagesqlite.EventProcessor
	Economy  appeconomy.Service
}

func newWorld(t *testing.T, database *storagesqlite.Database, clock *appclock.Fake) *world {
	t.Helper()
	catalogue := building.DefaultCatalogue()
	economyRepository := storagesqlite.NewEconomyRepository(database.Write(), catalogue)
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	economyRepository.RegisterHandlers(events)
	return &world{
		Database: database,
		Clock:    clock,
		Events:   events,
		Economy: appeconomy.Service{
			Clock:      clock,
			Repository: economyRepository,
			Catalogue:  catalogue,
			Completer:  events,
		},
	}
}
