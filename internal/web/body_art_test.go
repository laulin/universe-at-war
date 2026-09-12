package web

import (
	"testing"

	"universeatwar/internal/domain/building"
)

func TestBodySurfaceArtFollowsCompletedBuildings(t *testing.T) {
	tests := []struct {
		name   string
		levels building.Levels
		moon   bool
		want   string
	}{
		{name: "empty frontier", levels: building.Levels{}, want: "frontier"},
		{name: "first mine", levels: building.Levels{building.MetalMine: 1}, want: "mining"},
		{name: "energy colony", levels: building.Levels{building.MetalMine: 4, building.SolarPlant: 7}, want: "energy"},
		{name: "industrial colony", levels: building.Levels{building.Shipyard: 5}, want: "industrial"},
		{name: "research colony", levels: building.Levels{building.ResearchLab: 7, building.Shipyard: 5}, want: "research"},
		{name: "fortress", levels: building.Levels{building.MissileSilo: 4}, want: "fortress"},
		{name: "advanced metropolis", levels: building.Levels{building.Terraformer: 1}, want: "metropolis"},
		{name: "moon", levels: building.Levels{building.JumpGate: 1}, moon: true, want: "moon"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := bodySurfaceArtSlot(test.levels, test.moon); got != test.want {
				t.Fatalf("bodySurfaceArtSlot() = %q, want %q", got, test.want)
			}
		})
	}
}
