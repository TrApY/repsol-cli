package cli

import (
	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// doctorHint points at the commands that show what is missing.
const doctorHint = `run "repsol auth status" and "repsol address show" to see what is missing`

// newDoctorCommand builds doctor.
func newDoctorCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the configuration and the Repsol backends",
		Long: `Probe the local configuration, the stored session, the Repsol API, the
stored address and its agency. Nothing is written. The command exits 1 with
code "doctor_failed" when a check fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := state.getApp()
			if err != nil {
				return err
			}
			report := a.Doctor(cmd.Context())
			if err := state.render(report); err != nil {
				return err
			}
			if !report.OK {
				return apperr.New(apperr.Generic, "doctor_failed",
					"one or more doctor checks failed").WithHint(doctorHint)
			}
			return nil
		},
	}
}
