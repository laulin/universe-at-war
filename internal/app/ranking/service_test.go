package ranking

import (
	"context"
	"testing"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/rules"
)

type repositoryStub struct{ snapshot Snapshot }

func (r repositoryStub) Snapshot(context.Context) (Snapshot, error) { return r.snapshot, nil }

func TestListComputesAndSortsEveryCategory(t *testing.T) {
	service := Service{
		Catalogues: catalogue.Default(),
		Repository: repositoryStub{snapshot: Snapshot{
			Rules: rules.Default(),
			Players: []PlayerAssets{
				{PlayerID: 1, AccountID: 11, Name: "Architecte", Buildings: []Level{{ID: "metal_storage", Level: 1}}, Research: []Level{{ID: "energy_technology", Level: 1}}, Units: []Quantity{{ID: "light_fighter", Quantity: 1}}},
				{PlayerID: 2, AccountID: 12, Name: "Amiral", Artificial: true, Units: []Quantity{{ID: "light_fighter", Quantity: 2}}},
			},
		}},
	}
	principal := appauth.Principal{AccountID: 11}

	total, err := service.List(context.Background(), principal, Total)
	if err != nil {
		t.Fatalf("List(total) error = %v", err)
	}
	if len(total) != 2 || total[0].Name != "Amiral" || total[0].Points != 8 || !total[0].Artificial {
		t.Fatalf("total ranking = %+v", total)
	}
	if total[1].Name != "Architecte" || total[1].Points != 6 || !total[1].Own {
		t.Fatalf("own total entry = %+v", total[1])
	}
	if total[1].Score.Economy != 1 || total[1].Score.Research != 1 || total[1].Score.Military != 4 {
		t.Fatalf("score breakdown = %+v", total[1].Score)
	}

	economy, err := service.List(context.Background(), principal, Economy)
	if err != nil {
		t.Fatalf("List(economy) error = %v", err)
	}
	if economy[0].Name != "Architecte" || economy[0].Points != 1 || economy[0].Position != 1 {
		t.Fatalf("economy ranking = %+v", economy)
	}
}

func TestBuildingInvestmentsAreCumulativePerPlanet(t *testing.T) {
	service := Service{
		Catalogues: catalogue.Default(),
		Repository: repositoryStub{snapshot: Snapshot{
			Rules: rules.Default(),
			Players: []PlayerAssets{{
				PlayerID: 1, AccountID: 1, Name: "Miner",
				Buildings: []Level{{ID: "metal_storage", Level: 2}, {ID: "metal_storage", Level: 1}},
			}},
		}},
	}
	entries, err := service.List(context.Background(), appauth.Principal{AccountID: 1}, Economy)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	// 1,000 + 2,000 on the first planet and 1,000 on the second.
	if entries[0].Points != 4 {
		t.Fatalf("economy points = %d, want 4", entries[0].Points)
	}
}
