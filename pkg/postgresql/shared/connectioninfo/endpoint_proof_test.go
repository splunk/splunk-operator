package connectioninfo

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointProofIncludesPortAndOptionalKeyPresence(t *testing.T) {
	endpoints := Endpoints{
		RWHost:        "green-rw.ns.svc.cluster.local",
		RHost:         "green-r.ns.svc.cluster.local",
		ROUnavailable: true,
		PoolerEnabled: true,
	}
	data, err := RoutingData(endpoints)
	require.NoError(t, err)
	assert.Equal(t, "", data[KeyClusterROEndpoint])
	assert.Equal(t, "", data[KeyPoolerRWEndpoint])
	assert.Equal(t, "", data[KeyPoolerROEndpoint])

	actual := maps.Clone(data)
	actual["DATABASE_NAME"] = "app"
	assert.True(t, EndpointDataMatches(actual, data))
	assert.Equal(t, FingerprintEndpointData(actual), FingerprintEndpointData(data))

	actual[KeyDefaultClusterPort] = "5433"
	assert.False(t, EndpointDataMatches(actual, data))
	assert.NotEqual(t, FingerprintEndpointData(actual), FingerprintEndpointData(data))

	actual = maps.Clone(data)
	delete(actual, KeyPoolerROEndpoint)
	assert.False(t, EndpointDataMatches(actual, data))
	assert.NotEqual(t, FingerprintEndpointData(actual), FingerprintEndpointData(data))
}

func TestEndpointKeysReturnsCopy(t *testing.T) {
	keys := EndpointKeys()
	require.Len(t, keys, 6)
	keys[0] = "changed"
	assert.Equal(t, KeyClusterRWEndpoint, EndpointKeys()[0])
}
