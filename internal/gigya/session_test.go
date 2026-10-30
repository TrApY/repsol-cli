package gigya

import (
	"os"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

func openTestdata(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestSessionFromHAR(t *testing.T) {
	session, err := SessionFromHAR(openTestdata(t, "testdata/session.har"))
	if err != nil {
		t.Fatalf("SessionFromHAR: %v", err)
	}
	want := Session{LoginToken: "st2.newest", GMID: "gmid-newest"}
	if session != want {
		t.Fatalf("session = %+v, want %+v", session, want)
	}
}

func TestSessionFromHARErrors(t *testing.T) {
	t.Run("no gmid", func(t *testing.T) {
		_, err := SessionFromHAR(openTestdata(t, "testdata/no_gmid.har"))
		typed := checkAppError(t, err, apperr.Usage, "har_no_gmid")
		if !strings.Contains(typed.Hint, "with sensitive data") {
			t.Errorf("hint = %q, want it to mention exporting with sensitive data", typed.Hint)
		}
	})

	t.Run("no session", func(t *testing.T) {
		_, err := SessionFromHAR(openTestdata(t, "testdata/no_session.har"))
		checkAppError(t, err, apperr.Usage, "har_no_session")
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := SessionFromHAR(strings.NewReader("not json"))
		checkAppError(t, err, apperr.Usage, "har_invalid")
	})
}

func TestParseSessionLine(t *testing.T) {
	ok := []struct {
		in   string
		want Session
	}{
		{"glt_" + APIKey + "=st2.a=b==; gmid=gmid.ver4.x==\r\n", Session{"st2.a=b==", "gmid.ver4.x=="}},
		{"  login_token=t1 ;gmid=g1 ", Session{"t1", "g1"}},
		{"gmid=g1; other=1; login_token=t1", Session{"t1", "g1"}},
		{"login_token=t1; gmid=g1\r\n", Session{"t1", "g1"}},
	}
	for _, tc := range ok {
		t.Run("ok/"+tc.in, func(t *testing.T) {
			got, err := ParseSessionLine(tc.in)
			if err != nil {
				t.Fatalf("ParseSessionLine(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseSessionLine(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}

	bad := []string{"login_token=t1", "gmid=g1", ""}
	for _, in := range bad {
		t.Run("bad/"+in, func(t *testing.T) {
			_, err := ParseSessionLine(in)
			checkAppError(t, err, apperr.Usage, "invalid_session_line")
		})
	}
}
