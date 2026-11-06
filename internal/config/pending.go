package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// PendingPayment is the Waylet payment a profile started and has not seen
// resolved yet. The CLI keeps it between runs: once the payment page is
// handed out, Repsol has no order until the owner pays, so the history cannot
// tell that another order would pay the same delivery twice.
type PendingPayment struct {
	// PaymentID names the payment, as the payment lookup takes it. It is the
	// Waylet id carried by the payment link (see repsolapi.WayletPaymentID),
	// not the id the cart answered: the lookup does not know that one. A file
	// written before the two were told apart holds the cart id here.
	PaymentID string `toml:"payment_id"`
	// CartID is the id POST /cart answered, kept when it differs from
	// PaymentID so that either name finds this payment. It is empty when the
	// two are the same, and in a file written before they were told apart.
	CartID string `toml:"cart_id,omitempty"`
	// StartedAt is when the payment was started.
	StartedAt time.Time `toml:"started_at"`
}

// Is reports whether id names this payment: the id the lookup takes, or the id
// the cart answered.
func (p PendingPayment) Is(id string) bool {
	return id != "" && (id == p.PaymentID || (p.CartID != "" && id == p.CartID))
}

// pendingFile is the on-disk layout of one profile's payment-<profile>.toml.
type pendingFile struct {
	Payment *PendingPayment `toml:"pending_payment,omitempty"`
}

// pendingPath is the file that holds the pending payment of a profile. It lives
// next to the lock files and, like credentials.toml, is private to the user:
// the payment id is redacted from every log, so it is not written to the
// world-readable config.toml either. Logging out does not remove it, because a
// payment the owner can still pay outlives the session.
func (s Store) pendingPath(profile string) (string, error) {
	if !profilePattern.MatchString(profile) {
		return "", apperr.New(apperr.Usage, "invalid_profile",
			fmt.Sprintf("invalid profile name %q", profile))
	}
	return filepath.Join(filepath.Dir(s.Paths.Lock), "payment-"+profile+".toml"), nil
}

// LoadPendingPayment reads the pending payment of a profile. found is false
// when there is none.
func (s Store) LoadPendingPayment(profile string) (PendingPayment, bool, error) {
	path, err := s.pendingPath(profile)
	if err != nil {
		return PendingPayment{}, false, err
	}
	var f pendingFile
	if err := loadFile(path, &f); err != nil {
		return PendingPayment{}, false, err
	}
	if f.Payment == nil || f.Payment.PaymentID == "" {
		return PendingPayment{}, false, nil
	}
	return *f.Payment, true, nil
}

// SavePendingPayment records the pending payment of a profile under the store
// lock. It replaces the one already there: a profile tracks one payment.
func (s Store) SavePendingPayment(profile string, p PendingPayment) error {
	path, err := s.pendingPath(profile)
	if err != nil {
		return err
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	data, err := toml.Marshal(pendingFile{Payment: &p})
	if err != nil {
		return writeFailed(path, err)
	}
	return WriteAtomic(path, privateMode, data)
}

// ClearPendingPayment drops the pending payment of a profile when it is the
// payment paymentID (under either of its ids), or whatever it is when
// paymentID is empty. A different
// pending payment, started after the one that was resolved, is left alone.
func (s Store) ClearPendingPayment(profile, paymentID string) error {
	path, err := s.pendingPath(profile)
	if err != nil {
		return err
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	var f pendingFile
	if err := loadFile(path, &f); err != nil {
		return err
	}
	if f.Payment == nil {
		return nil
	}
	if paymentID != "" && !f.Payment.Is(paymentID) {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return writeFailed(path, err)
	}
	return nil
}

// DiscardPendingPayment drops whatever the profile remembers about a payment,
// without reading it first. It is the way out of a state file that can no longer
// be read: ClearPendingPayment has to load the file to compare ids, and fails on
// it.
func (s Store) DiscardPendingPayment(profile string) error {
	path, err := s.pendingPath(profile)
	if err != nil {
		return err
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return writeFailed(path, err)
	}
	return nil
}
