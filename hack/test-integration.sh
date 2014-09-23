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

source $(dirname $0)/util.sh
source $(dirname $0)/config-go.sh

function cleanup()
{
    set +e
    kill ${ETCD_PID} 1>&2 2>/dev/null
    rm -rf ${ETCD_DIR} 1>&2 2>/dev/null
    echo
    echo "Complete"
}

# Stop right away if the build fails
set -e

if [[ -z $KUBE_NO_BUILD_INTEGRATION ]]; then
    $(dirname $0)/build-go.sh cmd/integration
fi

start_etcd

trap cleanup EXIT SIGINT

echo
echo Integration test cases ...
echo
GOFLAGS="-tags 'integration no-docker'" \
    ${KUBE_REPO_ROOT}/hack/test-go.sh test/integration
# leave etcd running if integration tests fail
trap "echo etcd still running" EXIT

echo
echo Integration scenario ...
echo
${KUBE_TARGET}/bin/integration

# nuke etcd
trap cleanup EXIT SIGINT
