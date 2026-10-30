package app

import (
	"context"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// DoctorCheck is the outcome of one doctor probe.
type DoctorCheck struct {
	// Name is the check identifier: config, credentials, session, api, address
	// or agency.
	Name string `json:"name"`
	// OK reports whether the probe succeeded.
	OK bool `json:"ok"`
	// Detail explains a failure, or carries a short success value such as the
	// agency code.
	Detail string `json:"detail,omitempty"`
}

// DoctorReport is the outcome of every doctor probe.
type DoctorReport struct {
	// OK reports whether every check succeeded.
	OK bool `json:"ok"`
	// Checks are the probes in their documented order.
	Checks []DoctorCheck `json:"checks"`
}

// Doctor probes the local configuration and the Repsol backends without
// writing anything: it loads both TOML files, signs the stored session, asks the
// API for its health, reads the stored address and resolves its agency. A check
// that depends on a failed one is skipped and says so.
func (a *App) Doctor(ctx context.Context) DoctorReport {
	checks := make([]DoctorCheck, 0, 6)

	// config: both local files must be readable and parse.
	_, configErr := a.store.LoadConfig()
	checks = append(checks, probe("config", configErr, ""))

	// credentials: the credential store must be readable. A profile without an
	// entry is not a failure; the session check reports the missing session.
	_, _, credentialsErr := a.store.LoadCredentials(a.profile)
	checks = append(checks, probe("credentials", credentialsErr, ""))

	// session: the app must be able to sign with the stored or environment
	// session. Sign only reads from Gigya unless it has to log in again.
	session := DoctorCheck{Name: "session"}
	switch {
	case credentialsErr != nil:
		session.Detail = skippedCheck("credentials")
	default:
		if _, err := a.session.Sign(ctx); err != nil {
			session.Detail = checkDetail(err)
		} else {
			session.OK = true
		}
	}
	checks = append(checks, session)

	// api: the Repsol API must answer its read-only health route.
	api := DoctorCheck{Name: "api"}
	if err := a.api.Health(ctx); err != nil {
		api.Detail = checkDetail(err)
	} else {
		api.OK = true
	}
	checks = append(checks, api)

	// address: the profile must have a stored delivery address.
	address := DoctorCheck{Name: "address"}
	var addr repsolapi.Address
	switch {
	case configErr != nil:
		address.Detail = skippedCheck("config")
	default:
		stored, err := a.loadAddress()
		if err != nil {
			address.Detail = checkDetail(err)
		} else {
			addr = stored
			address.OK = true
		}
	}
	checks = append(checks, address)

	// agency: the API must resolve an agency for the stored address.
	agency := DoctorCheck{Name: "agency"}
	switch {
	case !api.OK:
		agency.Detail = skippedCheck("api")
	case !address.OK:
		agency.Detail = skippedCheck("address")
	default:
		code, err := a.currentAgency(ctx, addr)
		if err != nil {
			agency.Detail = checkDetail(err)
		} else {
			agency.OK = true
			agency.Detail = code
		}
	}
	checks = append(checks, agency)

	report := DoctorReport{OK: true, Checks: checks}
	for _, check := range checks {
		if !check.OK {
			report.OK = false
		}
	}
	return report
}

// probe turns the error of a check into a DoctorCheck. The detail is the
// human-readable message, which never carries a secret.
func probe(name string, err error, okDetail string) DoctorCheck {
	if err != nil {
		return DoctorCheck{Name: name, Detail: checkDetail(err)}
	}
	return DoctorCheck{Name: name, OK: true, Detail: okDetail}
}

// skippedCheck is the detail of a check that depends on a failed one.
func skippedCheck(name string) string {
	return "skipped: " + name + " failed"
}

// checkDetail returns the message of err without the wrapped cause.
func checkDetail(err error) string {
	return apperr.From(err).Message
}
