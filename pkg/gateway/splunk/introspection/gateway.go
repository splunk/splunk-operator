package introspection

import (
	"context"
	splunkmodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model"
)

// EventPublisher is a function type for publishing events associated
// with gateway functions.
type EventPublisher func(reason, message string)

// Factory is the interface for creating new Gateway objects.
type Factory interface {
	NewGateway(ctx context.Context, sad *splunkmodel.SplunkCredentials, publisher EventPublisher) (Gateway, error)
}

// Gateway holds the state information for talking to
// splunk gateway backend.
type Gateway interface {

	// Heading: Introspect API list

	// Get information about the volume (logical drives) in use by the Splunk deployment.
	// endpoint: /services/data/index-volumes
	GetIndexVolumes() error

	// List the recognized indexes on the server.
	//  endpoint: /services/data/indexes
	GetIndexes() error

	// List bucket attributes for all indexes.
	// endpoint: /services/data/indexes-extended
	GetIndexAllBucketInformation() error

	// Get disk usage information about all summaries in an indexer.
	// endpoint: /services/data/summaries
	GetDataSummaries() error

	// Shows the overall health of a distributed deployment. The health of the deployment can be red, yellow, or green. The overall health of the deployment is based on the health of all features reporting to it.
	// Authentication and Authorization:
	// 		Requires the admin role or list_health capability.
	// endpoint: /services/server/health/deployment
	GetServerDeploymentHealth() error

	// Shows the overall health of splunkd. The health of splunkd can be red, yellow, or green. The health of splunkd is based on the health of all features reporting to it.
	// Authentication and Authorization:
	// 		Requires the admin role or list_health capability.
	// Get health status of distributed deployment features.
	// endpoint: https://<host>:<mPort>/services/server/health/deployment/details
	GetServerDeploymentHealthDetails() error

	// Shows the overall health of splunkd. The health of splunkd can be red, yellow, or green. The health of splunkd is based on the health of all features reporting to it.
	// /services/server/health/splunkd
	GetSplunkdHealth() error

	// Shows the overall health of the splunkd health status tree, as well as each feature node and its respective color. For unhealthy nodes (non-green), the output includes reasons, indicators, thresholds, messages, and so on.
	// Authentication and Authorization:
	// 			Requires the admin role or list_health capability.
	// /services/server/health/splunkd/details
	GetSplunkdHealthDetails() error

	// Shows the overall health of splunkd. The health of splunkd can be red, yellow, or green. The health of splunkd is based on the health of all features reporting to it.
	// Authentication and Authorization
	// 		Requires the admin role or list_health capability.
	// Get the health status of splunkd
	// endpoint: https://<host>:<mPort>/services/server/health/splunkd
	GetServerHealthConfig() error

	// Access information about the currently running Splunk instance.
	// Note: This endpoint provides information on the currently running Splunk instance. Some values returned
	// in the GET response reflect server status information. However, this endpoint is meant to provide
	// information on the currently running instance, not the machine where the instance is running.
	// Server status values returned by this endpoint should be considered deprecated and might not continue
	// to be accessible from this endpoint. Use server/sysinfo to access server status instead.
	//  endpoint: https://<host>:<mPort>/services/server/info
	GetServerInfo() error

	// Access system introspection artifacts.
	// endpoint: https://<host>:<mPort>/services/server/introspection
	GetServerIntrospection() error

	// List server/status child resources.
	// endpoint: https://<host>:<mPort>/services/server/status
	GetServerStatus() error

	// Access search job information.
	// endpoint: https://<host>:<mPort>/services/server/status/dispatch-artifacts
	GetServerDispatchArtifactsStatus() error

	// Access information about the private BTree database.
	// GET
	// Access private BTree database information.
	// endpoint: https://<host>:<mPort>/services/server/status/fishbucket
	GetServerFishBucketStatus() error

	// Check for system file irregularities.
	// endpoint: https://<host>:<mPort>/services/server/status/installed-file-integrity
	GetServerInstalledFileIntegrityStatus() error

	// Access search concurrency metrics for a standalone Splunk Enterprise instance.
	// Get search concurrency limits for a standalone Splunk Enterprise instance.
	// endpoint: https://<host>:<mPort>/services/server/status/limits/search-concurrency
	GetServerSearchConcurrencyLimitsStatus() error

	// Access disk utilization information for filesystems that have Splunk objects, such as indexes, volumes, and logs. A filesystem can span multiple physical disk partitions.
	// Get disk utilization information.
	// endpoint: https://<host>:<mPort>/services/server/status/partitions-space
	GetServerPartitionSpaceStatus() error

	// Get current resource (CPU, RAM, VM, I/O, file handle) utilization for entire host, and per Splunk-related processes.
	// endpoint: https://<host>:<mPort>/services/server/status/resource-usage
	GetServerResourceUsageStatus() error

	// Access host-level dynamic CPU utilization and paging information.
	// endpoint: https://<host>:<mPort>/services/server/status/resource-usage/hostwide
	GetServerHostwideResourceUsageState() error

	// Access the most recent disk I/O statistics for each disk. This endpoint is currently supported for Linux, Windows, and Solaris. By default this endpoint is updated every 60s seconds.
	// endpoint: https://<host>:<mPort>/services/server/status/resource-usage/iostats
	GetServerIostatResourceUsageStatus() error

	// Access operating system resource utilization information.
	// endpoint: https://<host>:<mPort>/services/server/status/resource-usage/splunk-processes
	GetSplunkProcessesResourceUsageStatus() error

	// Exposes relevant information about the resources and OS settings of the machine where Splunk Enterprise is running.
	// Usage details
	// This endpoint provides status information for the server where the current Splunk instance is running.
	// The GET request response includes Kernel Transparent Huge Pages (THP) and ulimit status.
	// Note: Some properties returned by this endpoint are also returned by server/info. However,
	// the server/info endpoint is meant to provide information on the currently running Splunk instance and not
	// the machine where the instance is running. Server status values returned by server/info should be considered
	// deprecated and might not continue to be accessible from this endpoint. Use the server/sysinfo endpoint for
	// server information instead.
	GetServerSysInfo() error
}
