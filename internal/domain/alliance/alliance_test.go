package alliance

import (
	"errors"
	"testing"
)

func TestPermissionsPerRole(t *testing.T) {
	permissions := []Permission{Invite, Expel, EditProfile, Diplomacy, LeadOperation, TransferCharge}
	expected := map[Role]map[Permission]bool{
		Founder: {Invite: true, Expel: true, EditProfile: true, Diplomacy: true, LeadOperation: true, TransferCharge: true},
		Officer: {Invite: true, Expel: true, LeadOperation: true},
		Member:  {},
	}
	for role, allowed := range expected {
		for _, permission := range permissions {
			if got := role.Can(permission); got != allowed[permission] {
				t.Fatalf("%s.Can(%s) = %v, want %v", role, permission, got, allowed[permission])
			}
		}
	}
	if Role("captain").Valid() || Role("captain").Can(Invite) {
		t.Fatal("an unknown role holds no permission")
	}
}

func TestExpellingNeverClimbsTheHierarchy(t *testing.T) {
	tests := []struct {
		actor, target Role
		want          bool
	}{
		{actor: Founder, target: Member, want: true},
		{actor: Founder, target: Officer, want: true},
		{actor: Founder, target: Founder},
		{actor: Officer, target: Member, want: true},
		{actor: Officer, target: Officer},
		{actor: Officer, target: Founder},
		{actor: Member, target: Member},
	}
	for _, test := range tests {
		if got := test.actor.CanExpel(test.target); got != test.want {
			t.Fatalf("%s expelling %s = %v, want %v", test.actor, test.target, got, test.want)
		}
	}
}

func TestNamesAndTagsAreNormalized(t *testing.T) {
	name, err := NormalizeName("  Les Corsaires  ")
	if err != nil || name != "Les Corsaires" {
		t.Fatalf("NormalizeName() = %q %v", name, err)
	}
	for _, invalid := range []string{"", "ab", "  x  ", string(make([]rune, 33))} {
		if _, err := NormalizeName(invalid); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("NormalizeName(%q) error = %v", invalid, err)
		}
	}

	tag, err := NormalizeTag(" cor ")
	if err != nil || tag != "COR" {
		t.Fatalf("NormalizeTag() = %q %v", tag, err)
	}
	for _, invalid := range []string{"", "a", "TOOLONGTAG", "CO R", "CÔR", "co-r"} {
		if _, err := NormalizeTag(invalid); !errors.Is(err, ErrInvalidTag) {
			t.Fatalf("NormalizeTag(%q) error = %v", invalid, err)
		}
	}
}

func TestRelationsAreDeclarative(t *testing.T) {
	if !Pact.Valid() || !War.Valid() {
		t.Fatal("a pact and a war are relations")
	}
	if Relation("vassal").Valid() {
		t.Fatal("an unknown relation was accepted")
	}
}
