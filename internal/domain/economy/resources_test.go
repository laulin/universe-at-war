package economy

import "testing"

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
