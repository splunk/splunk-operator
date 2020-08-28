package enterprise

const (
	// Less than zero version error
	lessThanOrEqualToZeroVersionError = "Versions shouldn't be <= 0"

	// Non-integer version error
	nonIntegerVersionError = "Failed to convert non integer string to integer value"

	// Non-matching string error
	nonMatchingStringError = "Non-matching string secretName %s versionedSecretIdentifier %s"

	// Missing Token error
	missingTokenError = "Couldn't convert to splunk readable format, %s token missing"
)
