package client

var invalidUrlByteArray = []byte{0x7F}

const (
	awsRegionEndPointDelimiter = "|"

	// Timeout for http clients used with appFramework
	appFrameworkHttpclientTimeout = 2000
)
