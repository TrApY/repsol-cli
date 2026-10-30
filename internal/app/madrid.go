package app

import (
	"time"
	_ "time/tzdata" // embeds the IANA database, so Europe/Madrid exists on every platform
)

// madrid is the time zone the delivery day is measured in. A delivery counts
// as pending when its date is today or later in Madrid.
var madrid = mustLoad("Europe/Madrid")

// mustLoad returns the named location, or UTC when it cannot be loaded. The
// embedded tzdata makes the fallback unreachable in a normal build.
func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
