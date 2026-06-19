package devssh

import "time"

// logElapsed records how long an operation took in the debug log.
func logElapsed(operation string, start time.Time) {
	logDebug("%s took %s", operation, time.Since(start).Round(time.Millisecond))
}
