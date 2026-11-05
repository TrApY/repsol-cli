package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// TestMain installs the real command entry point as the "repsol" command of
// the scripts, so every script runs Main in a subprocess of the test binary.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"repsol": func() { os.Exit(Main()) }})
}

// TestScripts runs the txtar scripts against the in-process fake backends.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			srv := fakerepsol.New()
			env.Defer(srv.Close)

			env.Setenv("REPSOL_API_BASE_URL", srv.APIURL())
			env.Setenv("REPSOL_GIGYA_BASE_URL", srv.GigyaURL())
			env.Setenv("REPSOL_ADDRESS_BASE_URL", srv.PlacesURL())
			env.Setenv("XDG_CONFIG_HOME", filepath.Join(env.WorkDir, ".config"))
			env.Setenv("HOME", filepath.Join(env.WorkDir, "home"))
			unsetEnv(env, "REPSOL_ERROR_FORMAT")

			env.Values["fake"] = srv
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"fake":     cmdFake,
			"filemode": cmdFilemode,
		},
	})
}

// unsetEnv removes an environment variable from the script environment.
func unsetEnv(env *testscript.Env, key string) {
	kept := make([]string, 0, len(env.Vars))
	for _, kv := range env.Vars {
		if name, _, ok := strings.Cut(kv, "="); ok && name == key {
			continue
		}
		kept = append(kept, kv)
	}
	env.Vars = kept
}

// fakeServer returns the fake backend of the running script.
func fakeServer(ts *testscript.TestScript) *fakerepsol.Server {
	srv, ok := ts.Value("fake").(*fakerepsol.Server)
	if !ok {
		ts.Fatalf("the fake backend is missing; TestScripts sets it up in Setup")
	}
	return srv
}

// cmdFake drives the fake backends from a script. It never reaches the
// network.
func cmdFake(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("fake does not support !")
	}
	if len(args) == 0 {
		ts.Fatalf("usage: fake count PATH N | fake fail PATH STATUS | fake check online-policy|order|solicitude | fake waylet on|off|pay-after N|pay ID | fake reject-token TOKEN")
	}

	srv := fakeServer(ts)
	switch args[0] {
	case "count":
		if len(args) != 3 {
			ts.Fatalf("usage: fake count PATH N")
		}
		want, err := strconv.Atoi(args[2])
		ts.Check(err)
		if got := srv.Count(args[1]); got != want {
			ts.Fatalf("%s: %d requests, want %d", args[1], got, want)
		}
	case "fail":
		if len(args) != 3 {
			ts.Fatalf("usage: fake fail PATH STATUS")
		}
		status, err := strconv.Atoi(args[2])
		ts.Check(err)
		srv.FailNext(args[1], status)
	case "check":
		if len(args) != 2 {
			ts.Fatalf("usage: fake check online-policy|order|solicitude")
		}
		switch args[1] {
		case "online-policy":
			srv.SetCheck(repsolapi.CartCheck{Status: "EE04", OnlinePolicy: true, OrderType: "SOLICITUDE"})
		case "order":
			srv.SetCheck(repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER"})
		case "solicitude":
			srv.SetCheck(repsolapi.CartCheck{Status: "EE04", OrderType: "SOLICITUDE"})
		default:
			ts.Fatalf("fake check: unknown outcome %q", args[1])
		}
	case "waylet":
		cmdFakeWaylet(ts, srv, args[1:])
	case "reject-token":
		if len(args) != 2 {
			ts.Fatalf("usage: fake reject-token TOKEN")
		}
		srv.RejectToken(args[1])
	default:
		ts.Fatalf("fake: unknown command %q", args[0])
	}
}

// cmdFakeWaylet drives the Waylet side of the fake: whether the agency accepts
// it, and when the owner "pays".
func cmdFakeWaylet(ts *testscript.TestScript, srv *fakerepsol.Server, args []string) {
	const usage = "usage: fake waylet on|off|pay-after N|pay ID"
	if len(args) == 0 {
		ts.Fatalf(usage)
	}
	switch args[0] {
	case "on", "off":
		srv.SetWayletPayment(args[0] == "on")
	case "pay-after":
		if len(args) != 2 {
			ts.Fatalf(usage)
		}
		n, err := strconv.Atoi(args[1])
		ts.Check(err)
		srv.SetWayletPayAfter(n)
	case "pay":
		if len(args) != 2 {
			ts.Fatalf(usage)
		}
		if !srv.ConfirmWaylet(args[1]) {
			ts.Fatalf("fake waylet pay: no unpaid payment %q", args[1])
		}
	default:
		ts.Fatalf(usage)
	}
}

// cmdFilemode checks the permission bits of a file.
func cmdFilemode(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 2 {
		ts.Fatalf("usage: filemode PATH OCTAL")
	}
	path := ts.MkAbs(args[0])
	want, err := strconv.ParseUint(args[1], 8, 32)
	ts.Check(err)

	info, err := os.Stat(path)
	ts.Check(err)
	got := uint64(info.Mode().Perm())

	if ok := got == want; ok == neg {
		if neg {
			ts.Fatalf("%s: file mode is %04o, want anything else", path, got)
		}
		ts.Fatalf("%s: file mode is %04o, want %04o", path, got, want)
	}
}
