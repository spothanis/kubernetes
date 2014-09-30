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

ROOT_DIR="$(dirname ${BASH_SOURCE})/.."
source "${ROOT_DIR}/cluster/kube-env.sh"
source "${ROOT_DIR}/cluster/${KUBERNETES_PROVIDER}/util.sh"

# Detect the OS name/arch so that we can find our binary
case "$(uname -s)" in
  Darwin)
    host_os=darwin
  ;;
  Linux)
    host_os=linux
  ;;
  *)
    echo "Unsupported host OS.  Must be Linux or Mac OS X." >&2
    exit 1
esac

case "$(uname -m)" in
  x86_64*)
    host_arch=amd64
  ;;
  i?86_64*)
    host_arch=amd64
  ;;
  amd64*)
    host_arch=amd64
  ;;
  arm*)
    host_arch=arm
  ;;
  i?86*)
    host_arch=x86
  ;;
  *)
  echo "Unsupported host arch. Must be x86_64, 386 or arm." >&2
  exit 1
esac

kubecfg="${ROOT_DIR}/_output/build/${host_os}/${host_arch}/kubecfg"
if [[ ! -x "$kubecfg" ]]; then
  kubecfg="${ROOT_DIR}/platforms/${host_os}/${host_arch}/kubecfg"
fi
if [[ ! -x "$kubecfg" ]]; then
  echo "It looks as if you don't have a compiled version of Kubernetes.  If you" >&2
  echo "are running from a clone of the git repo, please run ./build/make-cross.sh." >&2
  echo "Note that this requires having Docker installed.  If you are running " >&2
  echo "from a release tarball, something is wrong.  Look at " >&2
  echo "http://kubernetes.io/ for information on how to contact the "
  echo "development team for help." >&2
  exit 1
fi

# When we are using vagrant it has hard coded auth.  We repeat that here so that
# we don't clobber auth that might be used for a publicly facing cluster.
if [ "$KUBERNETES_PROVIDER" == "vagrant" ]; then
  cat >~/.kubernetes_vagrant_auth <<EOF
{
  "User": "vagrant",
  "Password": "vagrant"
}
EOF
  auth_config=(
    "-auth" "$HOME/.kubernetes_vagrant_auth"
    "-insecure_skip_tls_verify"
  )
else
  auth_config=()
fi

detect-master > /dev/null
if [[ "$KUBE_MASTER_IP" != "" ]] && [[ "$KUBERNETES_MASTER" == "" ]]; then
  export KUBERNETES_MASTER=https://${KUBE_MASTER_IP}
fi

"$kubecfg" "${auth_config[@]}" "$@"
