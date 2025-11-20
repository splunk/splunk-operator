package terms

import (
	"os"
	"strings"
	"sync/atomic"
)

const (
	EnvVarName   = "SPLUNK_GENERAL_TERMS"
	ExpectedFlag = "--accept-sgt-current-at-splunk-com"
)

var accepted atomic.Bool

func InitFromEnv() {
	got := strings.TrimSpace(os.Getenv(EnvVarName))
	accepted.Store(got == ExpectedFlag)
}

func Accepted() bool {
	return accepted.Load()
}
