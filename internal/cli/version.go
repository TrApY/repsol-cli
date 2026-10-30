package cli

import (
	"runtime"

	"github.com/spf13/cobra"
)

// versionInfo is the output of version.
type versionInfo struct {
	// Version is the released version, or "dev".
	Version string `json:"version"`
	// Commit is the git commit the binary was built from.
	Commit string `json:"commit"`
	// Date is the build date.
	Date string `json:"date"`
	// Go is the Go version the binary was built with.
	Go string `json:"go"`
}

// newVersionCommand builds version.
func newVersionCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return state.render(versionInfo{
				Version: Version,
				Commit:  Commit,
				Date:    Date,
				Go:      runtime.Version(),
			})
		},
	}
}
