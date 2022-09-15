#!/bin/bash

# Copyright 2022 Splunk

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

#This script is used to retrieve and report the state of the container
#Although not actively in the container, it can be used to check the health
#of the splunk instance
#NOTE: If you plan on running the splunk container while keeping Splunk
# inactive for long periods of time, this script may give misleading
# health results

# If exists, Source the liveness level
[[ -f /opt/splunk/etc/k8_liveness_driver.sh ]] && source /opt/splunk/etc/k8_liveness_driver.sh

# Get the HTTP proto type
get_http_proto_type() {
    if [[ "false" == "$SPLUNKD_SSL_ENABLE" || "false" == "$(/opt/splunk/bin/splunk btool server list | grep enableSplunkdSSL | cut -d\  -f 3)" ]]; then
      echo "http"
    else
      echo "https"
    fi
}

# Check if the Splunkd process is running or not
liveness_probe_check_splunkd_process() {
  SPLUNK_PROCESS_ID=`ps ax | grep "splunkd.*start" | grep -v grep | head -1 | cut -d' ' -f2`

  #If NO_HEALTHCHECK is NOT defined, then we want the healthcheck
  state="$(< $CONTAINER_ARTIFACT_DIR/splunk-container.state)"
  case "$state" in
  running|started)
      if [[ "" != "$SPLUNK_PROCESS_ID" ]]; then
         exit 0
      fi

      # Goes to the the pod event
      echo "Splunkd not running"
      exit 1
  ;;
  *)
      exit 1
  esac
}

# Default liveness probe checks for the mgmt port reachability
liveness_probe_default() {
    HTTP_SCHEME=$(get_http_proto_type)

    #If NO_HEALTHCHECK is NOT defined, then we want the healthcheck
    state="$(< $CONTAINER_ARTIFACT_DIR/splunk-container.state)"

    case "$state" in
    running|started)
        curl --max-time 30 --fail --insecure $HTTP_SCHEME://localhost:8089/
        if [[ $? == 0 ]]; then
            exit 0
        fi

        echo "Mgmt. port is not reachable"
        exit 1
    ;;
    *)
        exit 1
    esac
}

if [[ "" == "$NO_HEALTHCHECK" ]]; then
    case $K8_OPERATOR_LIVENESS_LEVEL in
    1)
        liveness_probe_check_splunkd_process
    ;;
    *)
        liveness_probe_default
    ;;
    esac

else
	#If NO_HEALTHCHECK is defined, ignore the healthcheck
	exit 0
fi