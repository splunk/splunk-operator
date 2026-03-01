package enterprise

import (
	"os"
	"strings"
)

// isMultiContainerPodEnabled gates the new multi-container pod layout without changing CRDs.
// When disabled, the operator behaves exactly as before.
func isMultiContainerPodEnabled() bool {
	v := strings.TrimSpace(os.Getenv("SPLUNK_POD_ARCH"))
	return strings.EqualFold(v, "multi-container") || strings.EqualFold(v, "multicontainer")
}
