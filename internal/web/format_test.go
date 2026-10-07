package web

import (
	"testing"
	"time"
)

func TestLocalTimeUsesTheConfiguredFallbackAndDaylightSaving(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "summer", at: time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC), want: "13:12:13 CEST"},
		{name: "winter", at: time.Date(2042, time.January, 10, 11, 12, 13, 0, time.UTC), want: "12:12:13 CET"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := localTime(test.at, paris, "15:04:05 MST"); got != test.want {
				t.Fatalf("localTime() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestInstantStaysUniversal(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2042, time.September, 10, 13, 12, 13, 0, paris)
	if got, want := instant(at), "2042-09-10T11:12:13Z"; got != want {
		t.Fatalf("instant() = %q, want %q", got, want)
	}
}
