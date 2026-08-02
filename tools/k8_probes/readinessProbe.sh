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

if [[ "" == "$NO_HEALTHCHECK" ]]; then
    # If liveness level level degraded, use the container state for the readiness probe(legacy logic)
    # shellcheck source=/dev/null
    [[ -f "$SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH" ]] && source "$SPLUNK_OPERATOR_K8_LIVENESS_DRIVER_FILE_PATH"

    # When the Alpha indexer lifecycle contract is enabled, remove an indexer
    # from traffic readiness whenever its configured HEC listener is enabled
    # but not serving. Read effective local Splunk configuration rather than
    # assuming HEC is enabled or hard-coding HTTP/HTTPS. This check must run
    # before the legacy liveness-level bypass because that bypass is used
    # during intentional indexer decommission.
    if [[ "true" == "$SPLUNK_OPERATOR_INDEXER_SERVING_READINESS" && "splunk_indexer" == "$SPLUNK_ROLE" ]]; then
        # K8_OPERATOR_LIVENESS_LEVEL is also set on every peer while App
        # Framework owns a Splunk-managed cluster-bundle restart. It is a
        # liveness policy, not proof that this peer is the one Operator-owned
        # lifecycle target. Only the explicit target hold marker withdraws the
        # peer before decommission and Kubernetes replacement.
        if [[ "true" == "$SPLUNK_OPERATOR_LIFECYCLE_HOLD" ]]; then
            echo "Indexer is in an Operator-owned lifecycle transition"
            exit 1
        fi
        if ! hec_btool_output="$(/opt/splunk/bin/splunk btool inputs list http 2>/dev/null)"; then
            echo "Unable to read effective Indexer HEC configuration"
            exit 1
        fi
        hec_config="$(
            printf '%s\n' "$hec_btool_output" |
            /usr/bin/awk '
                $1 == "[http]" { in_http=1; next }
                $1 ~ /^\[/ { in_http=0 }
                in_http && ($1 == "disabled" || $1 == "enableSSL" || $1 == "port") {
                    print $1 "=" tolower($3)
                }
            '
        )"
        hec_disabled="$(printf '%s\n' "$hec_config" | /usr/bin/awk -F= '$1 == "disabled" { print $2; exit }')"
        if [[ "0" == "$hec_disabled" || "false" == "$hec_disabled" ]]; then
            hec_ssl="$(printf '%s\n' "$hec_config" | /usr/bin/awk -F= '$1 == "enableSSL" { print $2; exit }')"
            hec_port="$(printf '%s\n' "$hec_config" | /usr/bin/awk -F= '$1 == "port" { print $2; exit }')"
            if [[ -z "$hec_port" ]]; then
                hec_port="8088"
            elif [[ ! "$hec_port" =~ ^[0-9]+$ ]] || (( hec_port < 1 || hec_port > 65535 )); then
                echo "Indexer HEC is enabled with an invalid port"
                exit 1
            fi
            hec_scheme="https"
            if [[ "0" == "$hec_ssl" || "false" == "$hec_ssl" ]]; then
                hec_scheme="http"
            fi
            if ! curl --silent --show-error --max-time 1 --fail --insecure \
                "$hec_scheme://127.0.0.1:$hec_port/services/collector/health" >/dev/null; then
                echo "Indexer HEC endpoint is enabled but not serving"
                exit 1
            fi
        fi
    fi

    if [[ "1" == "$K8_OPERATOR_LIVENESS_LEVEL" ]]; then
       /bin/grep started /opt/container_artifact/splunk-container.state
       exit $?
    fi

    if [[ "false" == "$SPLUNKD_SSL_ENABLE" || "false" == "$(/opt/splunk/bin/splunk btool server list | grep enableSplunkdSSL | cut -d\  -f 3)" ]]; then
      SCHEME="http"
        else
      SCHEME="https"
    fi
        #If NO_HEALTHCHECK is NOT defined, then we want the healthcheck
        state="$(< "$CONTAINER_ARTIFACT_DIR"/splunk-container.state)"

        case "$state" in
        running|started)
            curl --max-time 30 --fail --insecure $SCHEME://localhost:8089/
            exit $?
        ;;
        *)
            exit 1
        esac
else
        #If NO_HEALTHCHECK is defined, ignore the healthcheck
        exit 0
fi
