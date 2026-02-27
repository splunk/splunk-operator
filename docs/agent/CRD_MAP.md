# CRD Map

`PROJECT` is the source of truth for kind and API version mapping.
This map is optimized for fast navigation in the codebase.

## Primary Controller Map

| Kind | API Version | Type File | Controller | Enterprise Logic |
| --- | --- | --- | --- | --- |
| Standalone | v4 | `api/v4/standalone_types.go` | `internal/controller/standalone_controller.go` | `pkg/splunk/enterprise/standalone.go` |
| IndexerCluster | v4 | `api/v4/indexercluster_types.go` | `internal/controller/indexercluster_controller.go` | `pkg/splunk/enterprise/indexercluster.go` |
| SearchHeadCluster | v4 | `api/v4/searchheadcluster_types.go` | `internal/controller/searchheadcluster_controller.go` | `pkg/splunk/enterprise/searchheadcluster.go` |
| ClusterManager | v4 | `api/v4/clustermanager_types.go` | `internal/controller/clustermanager_controller.go` | `pkg/splunk/enterprise/clustermanager.go` |
| LicenseManager | v4 | `api/v4/licensemanager_types.go` | `internal/controller/licensemanager_controller.go` | `pkg/splunk/enterprise/licensemanager.go` |
| MonitoringConsole | v4 | `api/v4/monitoringconsole_types.go` | `internal/controller/monitoringconsole_controller.go` | `pkg/splunk/enterprise/monitoringconsole.go` |
| ClusterMaster (legacy) | v3 | `api/v3/clustermaster_types.go` | `internal/controller/clustermaster_controller.go` | `pkg/splunk/enterprise/clustermaster.go` |
| LicenseMaster (legacy) | v3 | `api/v3/licensemaster_types.go` | `internal/controller/licensemaster_controller.go` | `pkg/splunk/enterprise/licensemaster.go` |

## Shared Types
- Common spec fields and phases live in `api/v4/common_types.go`.
- Legacy common types live in `api/v3/common_types.go`.

## Docs Pointers
- Spec field reference: `docs/CustomResources.md`
- Example manifests: `docs/Examples.md`
