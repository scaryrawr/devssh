// Package devssh provides a Go API for starting devssh-enhanced OpenSSH
// sessions.
//
// The high-level Run and NewSession APIs connect to a normal OpenSSH host
// alias or target, start a ControlMaster, prepare helper scripts, manage local
// browser and notification services, configure forwards, run the port monitor,
// and clean up the session lifecycle.
package devssh
