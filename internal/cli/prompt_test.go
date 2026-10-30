package cli

import (
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// TestReadSecretLineTrimsCRLF covers Review Focus 4: a value pasted or piped
// from another tool keeps its inner '=' signs and loses the trailing CRLF.
func TestReadSecretLineTrimsCRLF(t *testing.T) {
	got, err := readSecretLine(strings.NewReader("pw=with=signs\r\nnext"))
	if err != nil {
		t.Fatalf("readSecretLine error = %v", err)
	}
	if want := "pw=with=signs"; got != want {
		t.Fatalf("readSecretLine = %q, want %q", got, want)
	}

	if _, err := readSecretLine(strings.NewReader("")); apperr.ExitCode(err) != int(apperr.Usage) {
		t.Fatalf("readSecretLine(empty) exit code = %d, want %d", apperr.ExitCode(err), apperr.Usage)
	}

	got, err = readSecretLine(strings.NewReader("correct horse\n"))
	if err != nil {
		t.Fatalf("readSecretLine error = %v", err)
	}
	if want := "correct horse"; got != want {
		t.Fatalf("readSecretLine = %q, want %q", got, want)
	}
}
