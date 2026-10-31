package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// Permissions of the stored files. The credentials file carries the login
// token and, when the owner opted in, the password, so it stays private.
const (
	privateMode fs.FileMode = 0o600
	configMode  fs.FileMode = 0o644
	dirMode     fs.FileMode = 0o700
)

// Profile is the non-secret configuration of one profile.
type Profile struct {
	Address *repsolapi.Address `toml:"address,omitempty"`
}

// Config is the content of config.toml.
type Config struct {
	DefaultProfile string             `toml:"default_profile,omitempty"`
	Profiles       map[string]Profile `toml:"profiles,omitempty"`
}

// Credentials is the secret content of one profile in credentials.toml.
type Credentials struct {
	LoginToken string    `toml:"login_token,omitempty"`
	GMID       string    `toml:"gmid,omitempty"`
	Email      string    `toml:"email,omitempty"`
	Password   string    `toml:"password,omitempty"`
	UID        string    `toml:"uid,omitempty"`
	FirstName  string    `toml:"first_name,omitempty"`
	UpdatedAt  time.Time `toml:"updated_at,omitempty"`
}

// credentialsFile is the on-disk layout of credentials.toml: every profile
// lives under [profiles.<name>].
type credentialsFile struct {
	Profiles map[string]Credentials `toml:"profiles,omitempty"`
}

// Store reads and writes the config and credential files at the given paths.
// Use Store{Paths: DefaultPaths()} for the real files.
type Store struct {
	Paths Paths
}

// LoadConfig reads config.toml. A missing file loads as the zero Config.
func (s Store) LoadConfig() (Config, error) {
	var cfg Config
	if err := loadFile(s.Paths.Config, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// UpdateConfig applies fn to the config under the lock and writes the result
// atomically. A missing file starts from the zero Config.
func (s Store) UpdateConfig(fn func(*Config) error) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	cfg, err := s.LoadConfig()
	if err != nil {
		return err
	}
	if err := fn(&cfg); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return writeFailed(s.Paths.Config, err)
	}
	return WriteAtomic(s.Paths.Config, configMode, data)
}

// LoadCredentials reads the entry of one profile. found is false when the
// profile is absent, including when credentials.toml does not exist yet.
func (s Store) LoadCredentials(profile string) (Credentials, bool, error) {
	var f credentialsFile
	if err := loadFile(s.Paths.Credentials, &f); err != nil {
		return Credentials{}, false, err
	}
	cred, found := f.Profiles[profile]
	return cred, found, nil
}

// UpdateCredentials applies fn to one profile's credentials under the lock and
// writes the file atomically. A missing profile starts from the zero value.
func (s Store) UpdateCredentials(profile string, fn func(*Credentials) error) error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	var f credentialsFile
	if err := loadFile(s.Paths.Credentials, &f); err != nil {
		return err
	}
	cred := f.Profiles[profile]
	if err := fn(&cred); err != nil {
		return err
	}
	if f.Profiles == nil {
		f.Profiles = make(map[string]Credentials)
	}
	f.Profiles[profile] = cred
	return s.writeCredentials(f)
}

// DeleteCredentials removes one profile under the lock. It reports whether the
// profile existed.
func (s Store) DeleteCredentials(profile string) (bool, error) {
	release, err := s.lock()
	if err != nil {
		return false, err
	}
	defer release()

	var f credentialsFile
	if err := loadFile(s.Paths.Credentials, &f); err != nil {
		return false, err
	}
	if _, found := f.Profiles[profile]; !found {
		return false, nil
	}
	delete(f.Profiles, profile)
	return true, s.writeCredentials(f)
}

// writeCredentials marshals and atomically stores credentials.toml.
func (s Store) writeCredentials(f credentialsFile) error {
	data, err := toml.Marshal(f)
	if err != nil {
		return writeFailed(s.Paths.Credentials, err)
	}
	return WriteAtomic(s.Paths.Credentials, privateMode, data)
}

// lock creates the application directory, then takes the exclusive file lock.
// The returned function releases it. The lock file itself is created 0600 by
// the flock package.
func (s Store) lock() (func(), error) {
	dir := filepath.Dir(s.Paths.Lock)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, apperr.New(apperr.Generic, "config_write_failed",
			fmt.Sprintf("create directory %s", dir)).WithCause(err)
	}
	l := flock.New(s.Paths.Lock)
	if err := l.Lock(); err != nil {
		return nil, apperr.New(apperr.Generic, "config_lock_failed",
			fmt.Sprintf("lock %s", s.Paths.Lock)).WithCause(err)
	}
	return func() { _ = l.Unlock() }, nil
}

// loadFile decodes a TOML file into dst. A missing file leaves dst untouched
// and is not an error.
func loadFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return apperr.New(apperr.Generic, "config_read_failed",
			fmt.Sprintf("read %s", path)).WithCause(err)
	}
	if err := toml.Unmarshal(data, dst); err != nil {
		// The decoder quotes the offending input, and credentials.toml holds
		// the password, so the cause is deliberately dropped: the error names
		// the path and never the content.
		return apperr.New(apperr.Generic, "config_invalid",
			fmt.Sprintf("invalid TOML in %s", path))
	}
	return nil
}

// WriteAtomic writes data to path through a temporary file in the same
// directory, then renames it into place, so a reader never sees a partial
// file. The temporary file is created 0600 and chmodded to perm before the
// rename, so the destination is never visible with wider permissions.
func WriteAtomic(path string, perm fs.FileMode, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return apperr.New(apperr.Generic, "config_write_failed",
			fmt.Sprintf("create directory %s", dir)).WithCause(err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return apperr.New(apperr.Generic, "config_write_failed",
			fmt.Sprintf("create temporary file in %s", dir)).WithCause(err)
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once the rename succeeds

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return writeFailed(path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return writeFailed(path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return writeFailed(path, err)
	}
	if err := tmp.Close(); err != nil {
		return writeFailed(path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return writeFailed(path, err)
	}
	return nil
}

// writeFailed builds the error for a failed write. The cause is OS or encoder
// state; neither carries credentials.
func writeFailed(path string, cause error) error {
	return apperr.New(apperr.Generic, "config_write_failed",
		fmt.Sprintf("write %s", path)).WithCause(cause)
}

// profilePattern is the accepted shape of a profile name.
var profilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ResolveProfile picks a profile name by precedence: flag, environment,
// configured default, then "default". Invalid names are a usage error.
func ResolveProfile(flag, env string, cfg Config) (string, error) {
	name := "default"
	switch {
	case flag != "":
		name = flag
	case env != "":
		name = env
	case cfg.DefaultProfile != "":
		name = cfg.DefaultProfile
	}
	if !profilePattern.MatchString(name) {
		return "", apperr.New(apperr.Usage, "invalid_profile",
			fmt.Sprintf("invalid profile name %q: use 1-32 characters from a-z, 0-9, '-' and '_', starting with a letter or digit", name))
	}
	return name, nil
}
