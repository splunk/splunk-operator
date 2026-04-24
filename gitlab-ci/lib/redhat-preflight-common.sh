#!/bin/sh

. "${CI_PROJECT_DIR}/gitlab-ci/lib/pipeline-common.sh"

install_preflight_release_binary() {
  preflight_version="$1"
  preflight_bin_dir="$2"

  require_nonempty "${preflight_version}" "PIPELINE_PREFLIGHT_VERSION"
  mkdir -p "${preflight_bin_dir}"
  PATH="${preflight_bin_dir}:${PATH}"
  export PATH

  if command -v preflight >/dev/null 2>&1; then
    return 0
  fi

  os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch_name="$(uname -m)"
  case "${arch_name}" in
    x86_64|amd64)
      arch_name="amd64"
      ;;
    aarch64|arm64)
      arch_name="arm64"
      ;;
    *)
      echo "Unsupported architecture for preflight binary: ${arch_name}" >&2
      return 1
      ;;
  esac

  # TODO CSPL-4731: replace public GitHub URLs with internal mirror once
  # artifact mirroring is set up for the SOK staging environment.
  curl -fsSL \
    "https://github.com/redhat-openshift-ecosystem/openshift-preflight/releases/download/${preflight_version}/preflight-${os_name}-${arch_name}" \
    -o "${preflight_bin_dir}/preflight"
  chmod 0755 "${preflight_bin_dir}/preflight"
}

prepare_preflight_dockerconfig() {
  dockerconfig_path="$1"
  shift

  if [ -n "${PIPELINE_PREFLIGHT_DOCKERCONFIG:-}" ]; then
    materialize_file_secret "${PIPELINE_PREFLIGHT_DOCKERCONFIG}" "${dockerconfig_path}"
    return 0
  fi

  printf '%s\n' '{"auths":{}}' > "${dockerconfig_path}"
  auth_added="false"
  for image_ref in "$@"; do
    registry_host="$(registry_host_from_image_ref "${image_ref}")"
    username="$(first_nonempty "${PIPELINE_PREFLIGHT_REGISTRY_USERNAME:-}" "${PIPELINE_DOCKER_USERNAME:-}" "")"
    password="$(first_nonempty "${PIPELINE_PREFLIGHT_REGISTRY_PASSWORD:-}" "${PIPELINE_DOCKER_PASSWORD:-}" "")"
    if registry_login_password_for_host "${registry_host}" "${username}" "${password}"; then
      auth_b64="$(printf '%s:%s' "${REGISTRY_LOGIN_USERNAME}" "${REGISTRY_LOGIN_PASSWORD}" | base64 | tr -d '\n')"
      jq --arg host "${registry_host}" \
         --arg user "${REGISTRY_LOGIN_USERNAME}" \
         --arg pass "${REGISTRY_LOGIN_PASSWORD}" \
         --arg auth "${auth_b64}" \
         '.auths[$host] = {username:$user,password:$pass,auth:$auth}' \
         "${dockerconfig_path}" > "${dockerconfig_path}.tmp"
      mv "${dockerconfig_path}.tmp" "${dockerconfig_path}"
      auth_added="true"
    fi
  done

  if [ "${auth_added}" != "true" ]; then
    rm -f "${dockerconfig_path}"
    return 1
  fi
}
