package impl

import (
	managermodel "github.com/splunk/splunk-operator/pkg/gateway/splunk/model/services/cluster/manager"
)

func getHealthContentFromDTO(data interface{}) managermodel.ClusterManagerHealthContent {
	config := managermodel.ClusterManagerHealthContent{}
	/*m := data.(map[string]interface{})
	    config := managermodel.HealthContent{}
	    if name, ok := m["access_logging_for_heartbeats"].(bool); ok {
			config.AccessLoggingForHeartbeats = name
		} */
	return config
}
