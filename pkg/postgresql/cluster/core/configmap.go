package core

import "slices"

import pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"

const (
	configMapKeySuperUserName      = "SUPER_USER_NAME"
	configMapKeySuperUserSecretRef = "SUPER_USER_SECRET_REF"
	configMapKeyServerCASecretRef  = "SERVER_CA_SECRET_REF"
)

func withSuperUser(name, secretRef string) pgconninfo.Option {
	return func(builder *pgconninfo.Builder) {
		builder.SetRequired(configMapKeySuperUserName, name)
		builder.SetRequired(configMapKeySuperUserSecretRef, secretRef)
	}
}

func withServerCA(secretRef string) pgconninfo.Option {
	return func(builder *pgconninfo.Builder) {
		builder.SetOptional(configMapKeyServerCASecretRef, secretRef)
	}
}

func buildClusterConfigMapData(endpoints pgconninfo.Endpoints, superUserName, superUserSecretRef, serverCASecretRef string) (map[string]string, []string, error) {
	return pgconninfo.BuildConfigMapData(
		endpoints,
		withSuperUser(superUserName, superUserSecretRef),
		withServerCA(serverCASecretRef),
	)
}

func requiredClusterConfigMapKeys() []string {
	keys := pgconninfo.RequiredKeys()
	keys = append(keys,
		configMapKeySuperUserName,
		configMapKeySuperUserSecretRef,
	)
	return slices.Clone(keys)
}
