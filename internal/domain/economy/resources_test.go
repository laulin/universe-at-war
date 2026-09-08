package economy

import (
	"math"
	"testing"
)

func TestResourcesDebit(t *testing.T) {
	tests := []struct {
		name    string
		stock   Resources
		cost    Resources
		want    Resources
		wantErr bool
	}{
		{name: "exact", stock: Resources{Metal: 10, Crystal: 20, Deuterium: 30}, cost: Resources{Metal: 10, Crystal: 20, Deuterium: 30}},
		{name: "partial", stock: Resources{Metal: 10, Crystal: 20, Deuterium: 30}, cost: Resources{Metal: 1, Crystal: 2, Deuterium: 3}, want: Resources{Metal: 9, Crystal: 18, Deuterium: 27}},
		{name: "insufficient", stock: Resources{Metal: 10}, cost: Resources{Metal: 11}, wantErr: true},
		{name: "negative cost", stock: Resources{Metal: 10}, cost: Resources{Metal: -1}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.stock.Debit(test.cost)
			if (err != nil) != test.wantErr {
				t.Fatalf("Debit() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("Debit() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRefundStopsAtTheStorageCapacity(t *testing.T) {
	stock := Resources{Metal: 900, Crystal: 100, Deuterium: 0}
	capacity := Resources{Metal: 1000, Crystal: 1000, Deuterium: 1000}

	credited, lost := stock.Refund(Resources{Metal: 300, Crystal: 50}, capacity)
	if credited != (Resources{Metal: 1000, Crystal: 150}) {
		t.Fatalf("credited = %#v", credited)
	}
	if lost != (Resources{Metal: 200}) {
		t.Fatalf("lost = %#v", lost)
	}
}

func TestRefundIntoAFullStoreKeepsNothing(t *testing.T) {
	full := Resources{Metal: 1000}
	credited, lost := full.Refund(Resources{Metal: 40}, Resources{Metal: 1000})
	if credited != full || lost != (Resources{Metal: 40}) {
		t.Fatalf("credited = %#v, lost = %#v", credited, lost)
	}
}

// A moon has no store to fill, so nothing of a refund is ever lost there.
func TestRefundOnABodyWithoutLimitsLosesNothing(t *testing.T) {
	stock := Resources{Metal: 5}
	boundless := Resources{Metal: math.MaxInt64, Crystal: math.MaxInt64, Deuterium: math.MaxInt64}
	credited, lost := stock.Refund(Resources{Metal: 1_000_000}, boundless)
	if credited != (Resources{Metal: 1_000_005}) || lost != (Resources{}) {
		t.Fatalf("credited = %#v, lost = %#v", credited, lost)
	}
}
