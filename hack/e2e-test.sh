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

# Starts a Kubernetes cluster, verifies it can do basic things, and shuts it
# down.

# Exit on error
set -e

# Use testing config
export KUBE_CONFIG_FILE="config-test.sh"
source $(dirname $0)/../cluster/util.sh

# Build a release
$(dirname $0)/../release/release.sh

# Now bring a test cluster up with that release.
$(dirname $0)/../cluster/kube-up.sh

# Auto shutdown cluster when we exit
function shutdown-test-cluster () {
  echo "Shutting down test cluster in background."
  gcutil deletefirewall  \
    --project ${PROJECT} \
    --norespect_terminal_width \
    --force \
    ${MINION_TAG}-http-alt &
  $(dirname $0)/../cluster/kube-down.sh > /dev/null &
}
trap shutdown-test-cluster EXIT

# Detect the project into $PROJECT if it isn't set
detect-project

# Open up port 8080 so nginx containers on minions can be reached
gcutil addfirewall \
  --norespect_terminal_width \
  --project ${PROJECT} \
  --target_tags ${MINION_TAG} \
  --allowed tcp:80 \
  --network ${NETWORK} \
  ${MINION_TAG}-http-alt &

CLOUDCGF="$(dirname $0)/../cluster/cloudcfg.sh"
GUESTBOOK="$(dirname $0)/../.examples/guestbook"

# Launch the guestbook example
$CLOUDCFG -c "${GUESTBOOK}/redis-master.json" create /pods
$CLOUDCFG -c "${GUESTBOOK}/redis-master-service.json" create /services
$CLOUDCFG -c "${GUESTBOOK}/redis-slave-controller.json" create /replicationControllers

sleep 5

# Count number of pods-- should be 5 plus two lines of header
PODS_FOUND=$($CLOUDCFG list pods | wc -l)

echo $PODS_FOUND

exit 0

# Container turn up on a clean cluster can take a while for the docker image pull.
# Sleep for 2 minutes just to be sure.
echo "Waiting for containers to come up."
sleep 120

# Get minion IP addresses
detect-minions

# Verify that something is listening (nginx should give us a 404)
for (( i=0; i<${#KUBE_MINION_IP_ADDRESSES[@]}; i++)); do
  IP_ADDRESS=${KUBE_MINION_IP_ADDRESSES[$i]}
  echo "Trying to reach nginx instance that should be running at ${IP_ADDRESS}:8080..."
  curl "http://${IP_ADDRESS}:8080"
done

