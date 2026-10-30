// Package config stores the per-profile configuration and credentials of the
// CLI in two TOML files under the XDG config home.
//
// config.toml holds non-secret data and is readable by everyone. credentials.toml
// holds the login token, the account metadata and, when the owner opted in, the
// password; it is private to the user, written atomically and guarded by a file
// lock so concurrent agent runs never lose an update.
package config

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// AppDir is the application directory below the XDG config home.
const AppDir = "repsol-cli"

// Paths holds the absolute paths of the files the store reads and writes.
type Paths struct {
	// Config is config.toml, world-readable and free of secrets.
	Config string
	// Credentials is credentials.toml, private to the user.
	Credentials string
	// Lock serialises updates to both files across processes.
	Lock string
}

// DefaultPaths returns the XDG paths of the application files. It creates
// nothing; the store creates the directory on the first write.
func DefaultPaths() Paths {
	dir := filepath.Join(xdg.ConfigHome, AppDir)
	credentials := filepath.Join(dir, "credentials.toml")
	return Paths{
		Config:      filepath.Join(dir, "config.toml"),
		Credentials: credentials,
		Lock:        credentials + ".lock",
	}
}
