#!/bin/bash

reg_name='kind-registry'
reg_port=$(echo $PRIVATE_REGISTRY | cut -d':' -f2)

function deleteCluster() {
  kind delete cluster --name=${TEST_CLUSTER_NAME}
  if [ $? -ne 0 ]; then
    echo "Unable to delete cluster - ${TEST_CLUSTER_NAME}"
    return 1
  fi

  docker rm -f ${reg_name}  
  if [ $? -ne 0 ]; then
    echo "Unable to delete private registry - ${reg_name}"
    return 1
  fi
  return 0
}

function createCluster() {
  echo "Using private registry at ${PRIVATE_REGISTRY}"
  # Deploy KIND cluster if not deploy
  rc=$(which kind)
  if [ -z "$rc" ]; then
    echo "Kind is not installed or in the PATH. Please install kind from https://kind.sigs.k8s.io/docs/user/quick-start/"
    return 1
  fi

  found=$(kind get clusters | grep "^${TEST_CLUSTER_NAME}$")
  if [ -z "$found" ]; then
    # create registry container unless it already exists
    running="$(docker inspect -f '{{.State.Running}}' "${reg_name}" 2>/dev/null || true)"
    if [ "${running}" != 'true' ]; then
      docker run \
       -d --restart=always -p "${reg_port}:5000" --name "${reg_name}" \
       registry:2
    fi
    reg_ip="$(docker inspect -f '{{.NetworkSettings.IPAddress}}' "${reg_name}")"

    workerNodes="- role: worker"
    for i in $(seq 2 $NUM_WORKERS);do
      workerNodes="${workerNodes}"$'\n'"- role: worker"
    done    

# create a cluster with the local registry enabled in containerd
cat <<EOF | kind create cluster --name "${TEST_CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
${workerNodes}
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${reg_port}"]
    endpoint = ["http://${reg_ip}:5000"]
EOF
fi

    # Output
    echo "KIND cluster nodes:"
    kind get nodes --name=${TEST_CLUSTER_NAME}
}
