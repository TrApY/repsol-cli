package cli

import (
	"bufio"
	"errors"
	"io"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// readSecretLine reads the first line of r and strips the trailing CRLF. Inner
// '=' signs and spaces are preserved, because tokens, gmids and passwords may
// contain them.
func readSecretLine(r io.Reader) (string, error) {
	if r == nil {
		return "", apperr.New(apperr.Usage, "missing_input",
			"no input is available")
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", apperr.New(apperr.Usage, "missing_input",
			"the input could not be read").WithCause(err)
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		return "", apperr.New(apperr.Usage, "missing_input",
			"the input is empty")
	}
	return line, nil
}
