package model

import "time"

// Result holds the response from a call in the Provsioner API.
type Result struct {
	// Dirty indicates whether the splunk object needs to be saved.
	Dirty bool
	// RequeueAfter indicates how long to wait before making the same
	// Provisioner call again. The request should only be requeued if
	// Dirty is also true.
	RequeueAfter time.Duration
	// Any error message produced by the provisioner.
	ErrorMessage string
}
