package app

import (
	"context"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// historyAt is a past order at the synthetic address with the given floor and
// door.
func historyAt(reference, street, number, postalCode, floor, door string) repsolapi.HistoryEntry {
	return repsolapi.HistoryEntry{
		ReferenceSAC: reference,
		DeliveryDate: "2026-10-01",
		Agency:       repsolapi.HistoryAgency{AgencyCode: fakerepsol.AgencyCode},
		Address: repsolapi.HistoryAddress{
			LocalityCode: fakerepsol.LocalityCode,
			Street:       street,
			Number:       number,
			PostalCode:   postalCode,
			Floor:        floor,
			Door:         door,
		},
	}
}

func newSignedInApp(t *testing.T) (*App, *fakerepsol.Server, config.Store) {
	t.Helper()
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	return a, fake, st
}

// An address set without a floor and a door takes them from the newest order
// the history holds for the same street, number and postal code, and says so.
func TestSetAddressTakesFloorAndDoorFromTheHistory(t *testing.T) {
	a, fake, st := newSignedInApp(t)
	fake.SetHistory([]repsolapi.HistoryEntry{
		historyAt("20000003", "Otra", "1", fakerepsol.PostalCode, "9", "Z"),
		historyAt("20000002", "Falsa", "1", fakerepsol.PostalCode, "3", "C"),
		historyAt("20000001", "Falsa", "1", fakerepsol.PostalCode, "5", "D"),
	})

	view, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo"})
	if err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	if view.Floor != "3" || view.Door != "C" {
		t.Errorf("floor/door = %q/%q, want 3/C from the newest matching order", view.Floor, view.Door)
	}
	if !strings.Contains(view.Note, "history") || !strings.Contains(view.Note, "floor 3") || !strings.Contains(view.Note, "door C") {
		t.Errorf("note = %q, want it to say the floor and door came from the history", view.Note)
	}
	if addr := requireProfileAddress(t, st); addr.Floor != "3" || addr.Door != "C" {
		t.Errorf("stored floor/door = %q/%q, want 3/C", addr.Floor, addr.Door)
	}
}

// What the user typed wins, and nothing is looked up.
func TestSetAddressKeepsTheFloorAndDoorTheUserGave(t *testing.T) {
	a, fake, _ := newSignedInApp(t)
	fake.SetHistory([]repsolapi.HistoryEntry{
		historyAt("20000001", "Falsa", "1", fakerepsol.PostalCode, "3", "C"),
	})

	view, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo", Floor: "2", Door: "B"})
	if err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	if view.Floor != "2" || view.Door != "B" || view.Note != "" {
		t.Errorf("floor/door/note = %q/%q/%q, want 2/B and no note", view.Floor, view.Door, view.Note)
	}
	if got := fake.Count("/api/order-history"); got != 0 {
		t.Errorf("order-history calls = %d, want 0 when the user gave both", got)
	}
}

// An order for another number or postal code says nothing about this address.
func TestSetAddressIgnoresHistoryOfOtherAddresses(t *testing.T) {
	a, fake, _ := newSignedInApp(t)
	fake.SetHistory([]repsolapi.HistoryEntry{
		historyAt("20000003", "Falsa", "2", fakerepsol.PostalCode, "3", "C"),
		historyAt("20000002", "Falsa", "1", "28002", "4", "D"),
		historyAt("20000001", "Falsa", "1", fakerepsol.PostalCode, "", ""),
	})

	view, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo"})
	if err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	if view.Floor != "" || view.Door != "" || view.Note != "" {
		t.Errorf("floor/door/note = %q/%q/%q, want none", view.Floor, view.Door, view.Note)
	}
}

// The history is a convenience: without a session, or when it cannot be read,
// the address is stored as before.
func TestSetAddressWorksWhenTheHistoryCannotBeRead(t *testing.T) {
	a, _, st, _ := newTestApp(t)
	if _, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo"}); err != nil {
		t.Fatalf("SetAddress without a session: %v", err)
	}
	requireProfileAddress(t, st)

	a, fake, st := newSignedInApp(t)
	fake.FailNext("/api/order-history", 500)
	view, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo"})
	if err != nil {
		t.Fatalf("SetAddress with a failing history: %v", err)
	}
	if view.Floor != "" || view.Note != "" {
		t.Errorf("floor/note = %q/%q, want none", view.Floor, view.Note)
	}
	requireProfileAddress(t, st)
}
