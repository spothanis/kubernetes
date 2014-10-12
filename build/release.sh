#! /bin/bash

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

# Build a Kubernetes release.  This will build the binaries, create the Docker
# images and other build artifacts.  All intermediate artifacts will be hosted
# publicly on Google Cloud Storage currently.

set -o errexit
set -o nounset
set -o pipefail

KUBE_ROOT=$(dirname "${BASH_SOURCE}")/..
source "$KUBE_ROOT/build/common.sh"

KUBE_RELEASE_RUN_TESTS=${KUBE_RELEASE_RUN_TESTS-y}

kube::build::verify_prereqs
kube::build::build_image
kube::build::run_build_command build/build-image/make-binaries.sh
kube::build::run_build_command build/build-image/make-cross.sh

if [[ $KUBE_RELEASE_RUN_TESTS =~ ^[yY]$ ]]; then
  kube::build::run_build_command build/build-image/run-tests.sh
  kube::build::run_build_command build/build-image/run-integration.sh
fi

kube::build::copy_output
kube::build::run_image
kube::release::package_tarballs

kube::release::${KUBERNETES_PROVIDER}::release
