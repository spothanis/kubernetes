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

#!/bin/bash

source $(dirname $0)/kube-env.sh
source $(dirname $0)/$KUBERNETES_PROVIDER/util.sh

CLOUDCFG=$(dirname $0)/../output/go/bin/kubecfg
if [ ! -x $CLOUDCFG ]; then
  echo "Could not find kubecfg binary. Run hack/build-go.sh to build it."
  exit 1
fi

detect-master > /dev/null

# detect-master returns this if there is no master found.
if [ "$KUBE_MASTER_IP" == "external-ip" ]; then
  KUBE_MASTER_IP=""
fi

if [ "$KUBERNETES_MASTER" == "" ]; then
  if [ "${KUBE_MASTER_IP}" != "" ]; then
    $CLOUDCFG -h https://${KUBE_MASTER_IP} $@
    exit $?
  fi
fi
$CLOUDCFG $@
