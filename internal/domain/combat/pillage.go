package combat

import (
	"math"

	"universeatwar/internal/domain/economy"
)

// Pillage is what the winner can carry away. The four passes spread the hold
// over the three resources, so the total taken is exactly the smaller of the
// remaining capacity and the pillageable stock.
func Pillage(stock economy.Resources, capacity int64, ratio float64) economy.Resources {
	if capacity <= 0 || ratio <= 0 || math.IsNaN(ratio) {
		return economy.Resources{}
	}
	if ratio > 1 {
		ratio = 1
	}
	available := [3]int64{
		floored(float64(stock.Metal) * ratio),
		floored(float64(stock.Crystal) * ratio),
		floored(float64(stock.Deuterium) * ratio),
	}
	var loot [3]int64
	share := capacity / 3
	for index := range loot {
		loot[index] = min64(available[index], share)
	}
	free := capacity - loot[0] - loot[1] - loot[2]
	half := free / 2
	loot[0] += min64(available[0]-loot[0], half)
	loot[1] += min64(available[1]-loot[1], half)
	free = capacity - loot[0] - loot[1] - loot[2]
	loot[2] += min64(available[2]-loot[2], free)
	free = capacity - loot[0] - loot[1] - loot[2]
	loot[0] += min64(available[0]-loot[0], free)
	free = capacity - loot[0] - loot[1] - loot[2]
	loot[1] += min64(available[1]-loot[1], free)
	return economy.Resources{Metal: loot[0], Crystal: loot[1], Deuterium: loot[2]}
}

func min64(first, second int64) int64 {
	if first < second {
		return first
	}
	return second
}
