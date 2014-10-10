#!/bin/bash

# Copyright 2014 Google Inc. All rights reserved.
#
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

# Launches an nginx container and verifies it can be reached. Assumes that
# we're being called by hack/e2e-test.sh (we use some env vars it sets up).
set -o errexit
set -o nounset
set -o pipefail

source "${KUBE_ROOT}/cluster/kube-env.sh"
source "${KUBE_ROOT}/cluster/$KUBERNETES_PROVIDER/util.sh"


CONTROLLER_NAME=update-demo

function validate() {
  local num_replicas=$1
  local container_image_version=$2

  # Container turn up on a clean cluster can take a while for the docker image pull.
  local num_running=0
  while [[ $num_running -ne $num_replicas ]]; do
    echo "Waiting for all containers in pod to come up. Currently: ${num_running}/${num_replicas}"
    sleep 2

    local pod_id_list
    pod_id_list=($($KUBECFG -template='{{range.Items}}{{.ID}} {{end}}' -l simpleService="${CONTROLLER_NAME}" list pods))

    echo "  ${#pod_id_list[@]} out of ${num_replicas} created"

    local id
    num_running=0
    for id in "${pod_id_list[@]}"; do
      local template_string current_status current_image host_ip
      template_string="{{and ((index .CurrentState.Info \"${CONTROLLER_NAME}\").State.Running) .CurrentState.Info.net.State.Running}}"
      current_status=$($KUBECFG -template="${template_string}" get "pods/$id")
      if [ "$current_status" != "{0001-01-01 00:00:00 +0000 UTC}" ]; then
        echo "  $id is created but not running"
        continue
      fi

      template_string="{{(index .CurrentState.Info \"${CONTROLLER_NAME}\").Image}}"
      current_image=$($KUBECFG -template="${template_string}" get "pods/$id")
      if [[ "$current_image" != "${DOCKER_HUB_USER}/update-demo:${container_image_version}" ]]; then
        echo "  ${id} is created but running wrong image"
        continue
      fi


      host_ip=$($KUBECFG -template='{{.CurrentState.HostIP}}' get pods/$id)
      curl -s --max-time 5 --fail http://${host_ip}:8080/data.json \
          | grep -q ${container_image_version} || {
        echo "  ${id} is running the right image but curl to contents failed or returned wrong info"
        continue

      }

      echo "  ${id} is verified up and running"

      ((num_running++)) || true
    done
  done
  return 0
}

export DOCKER_HUB_USER=jbeda

# Launch a container
${KUBE_ROOT}/examples/update-demo/2-create-replication-controller.sh

function teardown() {
  echo "Cleaning up test artifacts"
  ${KUBE_ROOT}/examples/update-demo/5-down.sh
}

trap "teardown" EXIT

validate 2 nautilus

${KUBE_ROOT}/examples/update-demo/3-scale.sh 1
sleep 2
validate 1 nautilus

${KUBE_ROOT}/examples/update-demo/3-scale.sh 2
sleep 2
validate 2 nautilus

${KUBE_ROOT}/examples/update-demo/4-rolling-update.sh kitten 1s
sleep 2
validate 2 kitten

exit 0
