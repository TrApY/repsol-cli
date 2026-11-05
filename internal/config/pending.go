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
	// PaymentID names the payment, as the payment lookup takes it.
	PaymentID string `toml:"payment_id"`
	// StartedAt is when the payment was started.
	StartedAt time.Time `toml:"started_at"`
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
// payment paymentID, or whatever it is when paymentID is empty. A different
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
	if paymentID != "" && f.Payment.PaymentID != paymentID {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return writeFailed(path, err)
	}
	return nil
}
