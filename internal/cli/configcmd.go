package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
)

// defaultProfileKey is the only configuration key the config command exposes.
const defaultProfileKey = "default_profile"

// configPathsView is the output of config path.
type configPathsView struct {
	// Config is the path of config.toml.
	Config string `json:"config"`
	// Credentials is the path of credentials.toml.
	Credentials string `json:"credentials"`
}

// newConfigCommand builds the config command and its subcommands.
func newConfigCommand(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and change the configuration",
	}
	cmd.AddCommand(
		newConfigPathCommand(state),
		newConfigGetCommand(state),
		newConfigSetCommand(state),
		newConfigListCommand(state),
	)
	return cmd
}

// newConfigPathCommand builds config path.
func newConfigPathCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the paths of the configuration files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			paths := config.DefaultPaths()
			return state.render(configPathsView{
				Config:      paths.Config,
				Credentials: paths.Credentials,
			})
		},
	}
}

// newConfigGetCommand builds config get.
func newConfigGetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "get KEY",
		Short: "Print one configuration value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireConfigKey(args[0]); err != nil {
				return err
			}
			cfg, err := configStore().LoadConfig()
			if err != nil {
				return err
			}
			return state.render(configValues(cfg))
		},
	}
}

// newConfigSetCommand builds config set.
func newConfigSetCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Set one configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			if err := requireConfigKey(key); err != nil {
				return err
			}
			if err := validateProfileName(value); err != nil {
				return err
			}
			store := configStore()
			err := store.UpdateConfig(func(cfg *config.Config) error {
				cfg.DefaultProfile = value
				return nil
			})
			if err != nil {
				return err
			}
			return state.render(map[string]string{defaultProfileKey: value})
		},
	}
}

// newConfigListCommand builds config list.
func newConfigListCommand(state *rootState) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print every configuration value",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := configStore().LoadConfig()
			if err != nil {
				return err
			}
			return state.render(configValues(cfg))
		},
	}
}

// configStore returns the store of the real configuration files.
func configStore() config.Store {
	return config.Store{Paths: config.DefaultPaths()}
}

// configValues renders the configuration as a key/value object.
func configValues(cfg config.Config) map[string]string {
	return map[string]string{defaultProfileKey: cfg.DefaultProfile}
}

// requireConfigKey rejects every key but default_profile.
func requireConfigKey(key string) error {
	if key != defaultProfileKey {
		return apperr.New(apperr.Usage, "unknown_key",
			fmt.Sprintf("unknown config key %q", key)).
			WithHint(fmt.Sprintf("the only key is %q", defaultProfileKey))
	}
	return nil
}

// validateProfileName applies the ResolveProfile rules to a configured default
// profile. An empty value is not a profile name.
func validateProfileName(value string) error {
	if value == "" {
		return apperr.New(apperr.Usage, "invalid_profile",
			"invalid profile name \"\": use 1-32 characters from a-z, 0-9, '-' and '_', starting with a letter or digit")
	}
	if _, err := config.ResolveProfile(value, "", config.Config{}); err != nil {
		return err
	}
	return nil
}
