// Command repsol orders bottled gas from Repsol for the owner, paid on
// delivery.
package main

import (
	"os"

	"github.com/felipe9962/repsol-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
