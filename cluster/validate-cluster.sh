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

# Bring up a Kubernetes cluster.
#
# If the full release name (gs://<bucket>/<release>) is passed in then we take
# that directly.  If not then we assume we are doing development stuff and take
# the defaults in the release config.

# exit on any error
set -e

source $(dirname $0)/kube-env.sh
source $(dirname $0)/$KUBERNETES_PROVIDER/util.sh

detect-minions > /dev/null

MINIONS_FILE=/tmp/minions
$(dirname $0)/kubecfg.sh -template '{{range.Items}}{{.ID}}:{{end}}' list minions > ${MINIONS_FILE}

for (( i=0; i<${#MINION_NAMES[@]}; i++)); do
    count=$(grep -c ${MINION_NAMES[i]} ${MINIONS_FILE})
    if [ "$count" == "0" ]; then
	echo "Failed to find ${MINION_NAMES[i]}, cluster is probably broken."
        exit 1
    fi
done
echo "Cluster validation succeeded"

