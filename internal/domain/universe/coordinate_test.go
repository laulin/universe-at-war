package universe

import "testing"

func TestParseCoordinate(t *testing.T) {
	limits := Limits{Galaxies: 3, Systems: 100, Positions: 15}
	tests := []struct {
		name    string
		value   string
		want    Coordinate
		wantErr bool
	}{
		{name: "valid", value: "2:42:8", want: Coordinate{Galaxy: 2, System: 42, Position: 8}},
		{name: "lower bounds", value: "1:1:1", want: Coordinate{Galaxy: 1, System: 1, Position: 1}},
		{name: "upper bounds", value: "3:100:15", want: Coordinate{Galaxy: 3, System: 100, Position: 15}},
		{name: "missing part", value: "1:2", wantErr: true},
		{name: "extra part", value: "1:2:3:4", wantErr: true},
		{name: "not numeric", value: "1:x:3", wantErr: true},
		{name: "zero", value: "0:1:1", wantErr: true},
		{name: "out of bounds", value: "1:101:1", wantErr: true},
		{name: "whitespace", value: " 1:2:3", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCoordinate(test.value, limits)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseCoordinate() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("ParseCoordinate() = %#v, want %#v", got, test.want)
			}
			if err == nil && got.String() != test.value {
				t.Fatalf("String() = %q, want %q", got.String(), test.value)
			}
		})
	}
}

func FuzzParseCoordinate(f *testing.F) {
	f.Add("1:1:1")
	f.Add("3:100:15")
	f.Add("invalid")
	limits := Limits{Galaxies: 3, Systems: 100, Positions: 15}
	f.Fuzz(func(t *testing.T, value string) {
		coordinate, err := ParseCoordinate(value, limits)
		if err != nil {
			return
		}
		if err := coordinate.Validate(limits); err != nil {
			t.Fatalf("accepted coordinate is invalid: %v", err)
		}
		parsedAgain, err := ParseCoordinate(coordinate.String(), limits)
		if err != nil || parsedAgain != coordinate {
			t.Fatalf("round trip failed: %#v, %v", parsedAgain, err)
		}
	})
}
