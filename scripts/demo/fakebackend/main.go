// Command fakebackend serves the in-process fake of Repsol's backends for the
// README demo. It prints the environment that points the CLI at the fake and
// runs until it is interrupted. Every value it serves is synthetic and it never
// reaches the network.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
)

func main() {
	srv := fakerepsol.New()
	defer srv.Close()

	env := [][2]string{
		{"REPSOL_API_BASE_URL", srv.APIURL()},
		{"REPSOL_GIGYA_BASE_URL", srv.GigyaURL()},
		{"REPSOL_ADDRESS_BASE_URL", srv.PlacesURL()},
		{"REPSOL_LOGIN_TOKEN", fakerepsol.ValidToken},
		{"REPSOL_GMID", fakerepsol.ValidGMID},
	}
	for _, kv := range env {
		fmt.Printf("export %s=%s\n", kv[0], kv[1])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}
