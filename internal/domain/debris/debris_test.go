package debris

import "testing"

func TestHarvestSharesTheHoldBetweenBothResources(t *testing.T) {
	tests := []struct {
		name     string
		field    Field
		capacity int64
		want     Field
	}{
		{name: "empty hold", field: Field{Metal: 100, Crystal: 100}, capacity: 0},
		{name: "empty field", capacity: 1000},
		{name: "hold larger than the field", field: Field{Metal: 300, Crystal: 200}, capacity: 10000, want: Field{Metal: 300, Crystal: 200}},
		{name: "even split", field: Field{Metal: 1000, Crystal: 1000}, capacity: 1000, want: Field{Metal: 500, Crystal: 500}},
		{name: "only metal left", field: Field{Metal: 1000}, capacity: 600, want: Field{Metal: 600}},
		{name: "crystal takes the remainder", field: Field{Metal: 100, Crystal: 1000}, capacity: 600, want: Field{Metal: 100, Crystal: 500}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Harvest(test.field, test.capacity)
			if got != test.want {
				t.Fatalf("Harvest() = %+v, want %+v", got, test.want)
			}
			if got.Total() > test.capacity {
				t.Fatalf("the harvest %d exceeds the hold %d", got.Total(), test.capacity)
			}
			if got.Metal > test.field.Metal || got.Crystal > test.field.Crystal {
				t.Fatalf("the harvest %+v exceeds the field %+v", got, test.field)
			}
		})
	}
}

func TestCapacityIsBoundedByRecyclersAndHold(t *testing.T) {
	if got := Capacity(2, 20000, 45000, 0); got != 40000 {
		t.Fatalf("Capacity() = %d, want the recyclers to bind at 40000", got)
	}
	if got := Capacity(4, 20000, 45000, 0); got != 45000 {
		t.Fatalf("Capacity() = %d, want the hold to bind at 45000", got)
	}
	if got := Capacity(4, 20000, 45000, 45000); got != 0 {
		t.Fatalf("Capacity() = %d, want nothing left in a full hold", got)
	}
}
