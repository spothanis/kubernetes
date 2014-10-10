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

# Common utilties, variables and checks for all build scripts.
set -o errexit
set -o nounset
set -o pipefail

cd $(dirname "${BASH_SOURCE}")/..

source hack/config-go.sh

# Incoming options
#
readonly KUBE_BUILD_RUN_IMAGES="${KUBE_BUILD_RUN_IMAGES:-n}"
readonly KUBE_GCS_UPLOAD_RELEASE="${KUBE_GCS_UPLOAD_RELEASE:-n}"
readonly KUBE_GCS_NO_CACHING="${KUBE_GCS_NO_CACHING:-y}"
readonly KUBE_GCS_MAKE_PUBLIC="${KUBE_GCS_MAKE_PUBLIC:-y}"
# KUBE_GCS_RELEASE_BUCKET default: kubernetes-releases-${project_hash}
# KUBE_GCS_RELEASE_PREFIX default: devel/
# KUBE_GCS_DOCKER_REG_PREFIX default: docker-reg/



# Constants
readonly KUBE_REPO_ROOT="${PWD}"

readonly KUBE_BUILD_IMAGE_REPO=kube-build
readonly KUBE_BUILD_IMAGE_TAG=build
readonly KUBE_BUILD_IMAGE="${KUBE_BUILD_IMAGE_REPO}:${KUBE_BUILD_IMAGE_TAG}"

readonly KUBE_GO_PACKAGE="github.com/GoogleCloudPlatform/kubernetes"

# We set up a volume so that we have the same _output directory from one run of
# the container to the next.
#
# Note that here "LOCAL" is local to the docker daemon.  In the boot2docker case
# this is still inside the VM.  We use the same directory in both cases though.
readonly LOCAL_OUTPUT_ROOT="${KUBE_REPO_ROOT}/_output"
readonly LOCAL_OUTPUT_BUILD="${LOCAL_OUTPUT_ROOT}/build"
readonly REMOTE_OUTPUT_ROOT="/go/src/${KUBE_GO_PACKAGE}/_output"
readonly REMOTE_OUTPUT_DIR="${REMOTE_OUTPUT_ROOT}/build"
readonly DOCKER_CONTAINER_NAME=kube-build
readonly DOCKER_MOUNT="-v ${LOCAL_OUTPUT_BUILD}:${REMOTE_OUTPUT_DIR}"

readonly KUBE_CLIENT_BINARIES=(
  kubecfg
)

readonly KUBE_SERVER_BINARIES=(
  apiserver
  controller-manager
  kubelet
  proxy
  scheduler
)

readonly KUBE_SERVER_PLATFORMS=(
  linux/amd64
)

readonly KUBE_RUN_IMAGE_BASE="kubernetes"
readonly KUBE_RUN_IMAGES=(
  apiserver
  controller-manager
  proxy
  scheduler
  kubelet
)


# This is where the final release artifacts are created locally
readonly RELEASE_DIR="${LOCAL_OUTPUT_ROOT}/release-tars"

# ---------------------------------------------------------------------------
# Basic setup functions

# Verify that the right utilities and such are installed for building Kube.
function kube::build::verify_prereqs() {
  if [[ -z "$(which docker)" ]]; then
    echo "Can't find 'docker' in PATH, please fix and retry." >&2
    echo "See https://docs.docker.com/installation/#installation for installation instructions." >&2
    return 1
  fi

  if kube::build::is_osx; then
    if [[ -z "$(which boot2docker)" ]]; then
      echo "It looks like you are running on Mac OS X and boot2docker can't be found." >&2
      echo "See: https://docs.docker.com/installation/mac/" >&2
      return 1
    fi
    if [[ $(boot2docker status) != "running" ]]; then
      echo "boot2docker VM isn't started.  Please run 'boot2docker start'" >&2
      return 1
    fi
  fi

  if ! docker info > /dev/null 2>&1 ; then
    echo "Can't connect to 'docker' daemon.  please fix and retry." >&2
    echo >&2
    echo "Possible causes:" >&2
    echo "  - On Mac OS X, boot2docker VM isn't started" >&2
    echo "  - On Mac OS X, DOCKER_HOST env variable isn't set approriately" >&2
    echo "  - On Linux, user isn't in 'docker' group.  Add and relogin." >&2
    echo "  - On Linux, Docker daemon hasn't been started or has crashed" >&2
    return 1
  fi
}

# ---------------------------------------------------------------------------
# Utility functions

function kube::build::is_osx() {
  [[ "$OSTYPE" == "darwin"* ]]
}

function kube::build::clean_output() {
  # Clean out the output directory if it exists.
  if kube::build::is_osx ; then
    if kube::build::build_image_built ; then
      echo "+++ Cleaning out boot2docker _output/"
      kube::build::run_build_command rm -rf "${REMOTE_OUTPUT_ROOT}" || true
    else
      echo "!!! Build image not built.  Assuming boot2docker _output is clean."
    fi
  fi

  echo "+++ Cleaning out _output directory"
  rm -rf "${LOCAL_OUTPUT_ROOT}"
}

# ---------------------------------------------------------------------------
# Building

# Returns 0 if the image is already built.  Otherwise 1
function kube::build::build_image_built() {
  # We cannot just specify the IMAGE here as `docker images` doesn't behave as
  # expected.  See: https://github.com/docker/docker/issues/8048
  docker images | grep -q "${KUBE_BUILD_IMAGE_REPO}\s*${KUBE_BUILD_IMAGE_TAG}"
}

# Set up the context directory for the kube-build image and build it.
function kube::build::build_image() {
  local -r build_context_dir="${LOCAL_OUTPUT_ROOT}/images/${KUBE_BUILD_IMAGE}"
  local -r source=(
    api
    build
    cmd
    examples
    Godeps/Godeps.json
    Godeps/_workspace/src
    hack
    LICENSE
    pkg
    plugin
    README.md
    third_party
  )
  mkdir -p "${build_context_dir}"
  tar czf "${build_context_dir}/kube-source.tar.gz" "${source[@]}"
  cat >"${build_context_dir}/kube-version-defs" <<EOF
KUBE_LD_FLAGS="$(kube::version_ldflags)"
EOF
  cp build/build-image/Dockerfile ${build_context_dir}/Dockerfile
  kube::build::docker_build "${KUBE_BUILD_IMAGE}" "${build_context_dir}"
}

# Builds the runtime image.  Assumes that the appropriate binaries are already
# built and in _output/build/.
function kube::build::run_image() {
  [[ "${KUBE_BUILD_RUN_IMAGES}" == "y" ]] || return 0

  local -r build_context_base="${LOCAL_OUTPUT_ROOT}/images/${KUBE_RUN_IMAGE_BASE}"

  # First build the base image.  This one brings in all of the binaries.
  mkdir -p "${build_context_base}"
  tar czf "${build_context_base}/kube-bins.tar.gz" \
    -C "${LOCAL_OUTPUT_ROOT}/build/linux/amd64" \
    "${KUBE_RUN_IMAGES[@]}"
  cp -R build/run-images/base/* "${build_context_base}/"
  kube::build::docker_build "${KUBE_RUN_IMAGE_BASE}" "${build_context_base}"

  local b
  for b in "${KUBE_RUN_IMAGES[@]}" ; do
    local sub_context_dir="${build_context_base}-$b"
    mkdir -p "${sub_context_dir}"
    cp -R build/run-images/$b/* "${sub_context_dir}/"
    kube::build::docker_build "${KUBE_RUN_IMAGE_BASE}-$b" "${sub_context_dir}"
  done
}

# Build a docker image from a Dockerfile.
# $1 is the name of the image to build
# $2 is the location of the "context" directory, with the Dockerfile at the root.
function kube::build::docker_build() {
  local -r image=$1
  local -r context_dir=$2
  local -r build_cmd="docker build -t ${image} ${context_dir}"

  echo "+++ Building Docker image ${image}. This can take a while."
  set +e # We are handling the error here manually
  local docker_output
  docker_output=$(${build_cmd} 2>&1)
  if [[ $? -ne 0 ]]; then
    set -e
    echo "+++ Docker build command failed for ${image}" >&2
    echo >&2
    echo "${docker_output}" >&2
    echo >&2
    echo "To retry manually, run:" >&2
    echo >&2
    echo "  ${build_cmd}" >&2
    echo >&2
    return 1
  fi
  set -e
}

function kube::build::clean_image() {
  local -r image=$1

  echo "+++ Deleting docker image ${image}"
  docker rmi ${image} 2> /dev/null || true
}

function kube::build::clean_images() {
  kube::build::clean_image "${KUBE_BUILD_IMAGE}"

  kube::build::clean_image "${KUBE_RUN_IMAGE_BASE}"

  local b
  for b in "${KUBE_RUN_IMAGES[@]}" ; do
    kube::build::clean_image "${KUBE_RUN_IMAGE_BASE}-${b}"
  done

  echo "+++ Cleaning all other untagged docker images"
  docker rmi $(docker images | awk '/^<none>/ {print $3}') 2> /dev/null || true
}

# Run a command in the kube-build image.  This assumes that the image has
# already been built.  This will sync out all output data from the build.
function kube::build::run_build_command() {
  [[ -n "$@" ]] || { echo "Invalid input." >&2; return 4; }

  local -r docker="docker run --name=${DOCKER_CONTAINER_NAME} --attach=stdout --attach=stderr --attach=stdin --tty ${DOCKER_MOUNT} ${KUBE_BUILD_IMAGE}"

  # Remove the container if it is left over from some previous aborted run
  docker rm ${DOCKER_CONTAINER_NAME} >/dev/null 2>&1 || true
  ${docker} "$@"

  # Remove the container after we run.  '--rm' might be appropriate but it
  # appears that sometimes it fails. See
  # https://github.com/docker/docker/issues/3968
  docker rm ${DOCKER_CONTAINER_NAME} >/dev/null 2>&1 || true
}

# If the Docker server is remote, copy the results back out.
function kube::build::copy_output() {
  if kube::build::is_osx; then
    # When we are on the Mac with boot2docker we need to copy the results back
    # out.  Ideally we would leave the container around and use 'docker cp' to
    # copy the results out.  However, that doesn't work for mounted volumes
    # currently (https://github.com/dotcloud/docker/issues/1992).  And it is
    # just plain broken (https://github.com/dotcloud/docker/issues/6483).
    #
    # The easiest thing I (jbeda) could figure out was to launch another
    # container pointed at the same volume, tar the output directory and ship
    # that tar over stdou.
    local -r docker="docker run -a stdout --name=${DOCKER_CONTAINER_NAME} ${DOCKER_MOUNT} ${KUBE_BUILD_IMAGE}"

    # Kill any leftover container
    docker rm ${DOCKER_CONTAINER_NAME} >/dev/null 2>&1 || true

    echo "+++ Syncing back _output directory from boot2docker VM"
    rm -rf "${LOCAL_OUTPUT_BUILD}"
    mkdir -p "${LOCAL_OUTPUT_BUILD}"
    ${docker} sh -c "tar c -C ${REMOTE_OUTPUT_DIR} . ; sleep 1"  \
      | tar xv -C "${LOCAL_OUTPUT_BUILD}"

    # Remove the container after we run.  '--rm' might be appropriate but it
    # appears that sometimes it fails. See
    # https://github.com/docker/docker/issues/3968
    docker rm ${DOCKER_CONTAINER_NAME} >/dev/null 2>&1 || true

    # I (jbeda) also tried getting rsync working using 'docker run' as the
    # 'remote shell'.  This mostly worked but there was a hang when
    # closing/finishing things off. Ug.
    #
    # local DOCKER="docker run -i --rm --name=${DOCKER_CONTAINER_NAME} ${DOCKER_MOUNT} ${KUBE_BUILD_IMAGE}"
    # DOCKER+=" bash -c 'shift ; exec \"\$@\"' --"
    # rsync --blocking-io -av -e "${DOCKER}" foo:${REMOTE_OUTPUT_DIR}/ ${LOCAL_OUTPUT_BUILD}
  fi
}

# ---------------------------------------------------------------------------
# Build final release artifacts
function kube::release::package_tarballs() {
  # Clean out any old releases
  rm -rf "${RELEASE_DIR}"
  mkdir -p "${RELEASE_DIR}"

  kube::release::package_client_tarballs
  kube::release::package_server_tarballs
  kube::release::package_salt_tarball
  kube::release::package_full_tarball
}

# Package up all of the cross compiled clients.  Over time this should grow into
# a full SDK
function kube::release::package_client_tarballs() {
   # Find all of the built kubecfg binaries
  local platform platforms
  platforms=($(cd "${LOCAL_OUTPUT_ROOT}/build" ; echo */*))
  for platform in "${platforms[@]}" ; do
    local platform_tag=${platform/\//-} # Replace a "/" for a "-"
    echo "+++ Building tarball: client $platform_tag"

    local release_stage="${LOCAL_OUTPUT_ROOT}/release-stage/client/${platform_tag}/kubernetes"
    rm -rf "${release_stage}"
    mkdir -p "${release_stage}/client/bin"

    # This fancy expression will expand to prepend a path
    # (${LOCAL_OUTPUT_ROOT}/build/${platform}/) to every item in the
    # KUBE_CLIENT_BINARIES array.
    cp "${KUBE_CLIENT_BINARIES[@]/#/${LOCAL_OUTPUT_ROOT}/build/${platform}/}" \
      "${release_stage}/client/bin/"

    local package_name="${RELEASE_DIR}/kubernetes-client-${platform_tag}.tar.gz"
    tar czf "${package_name}" -C "${release_stage}/.." .
  done
}

# Package up all of the server binaries
function kube::release::package_server_tarballs() {
  local platform
  for platform in "${KUBE_SERVER_PLATFORMS[@]}" ; do
    local platform_tag=${platform/\//-} # Replace a "/" for a "-"
    echo "+++ Building tarball: server $platform_tag"

    local release_stage="${LOCAL_OUTPUT_ROOT}/release-stage/server/${platform_tag}/kubernetes"
    rm -rf "${release_stage}"
    mkdir -p "${release_stage}/server/bin"

    # This fancy expression will expand to prepend a path
    # (${LOCAL_OUTPUT_ROOT}/build/${platform}/) to every item in the
    # KUBE_SERVER_BINARIES array.
    cp "${KUBE_SERVER_BINARIES[@]/#/${LOCAL_OUTPUT_ROOT}/build/${platform}/}" \
      "${release_stage}/server/bin/"

    local package_name="${RELEASE_DIR}/kubernetes-server-${platform_tag}.tar.gz"
    tar czf "${package_name}" -C "${release_stage}/.." .
  done
}

# Package up the salt configuration tree.  This is an optional helper to getting
# a cluster up and running.
function kube::release::package_salt_tarball() {
  echo "+++ Building tarball: salt"

  local release_stage="${LOCAL_OUTPUT_ROOT}/release-stage/salt/kubernetes"
  rm -rf "${release_stage}"
  mkdir -p "${release_stage}"

  cp -R "${KUBE_REPO_ROOT}/cluster/saltbase" "${release_stage}/"

  local package_name="${RELEASE_DIR}/kubernetes-salt.tar.gz"
  tar czf "${package_name}" -C "${release_stage}/.." .
}

# This is all the stuff you need to run/install kubernetes.  This includes:
#   - precompiled binaries for client
#   - Cluster spin up/down scripts and configs for various cloud providers
#   - tarballs for server binary and salt configs that are ready to be uploaded
#     to master by whatever means appropriate.
function kube::release::package_full_tarball() {
  echo "+++ Building tarball: full"

  local release_stage="${LOCAL_OUTPUT_ROOT}/release-stage/full/kubernetes"
  rm -rf "${release_stage}"
  mkdir -p "${release_stage}"

  cp -R "${LOCAL_OUTPUT_ROOT}/build" "${release_stage}/platforms"

  # We want everything in /cluster except saltbase.  That is only needed on the
  # server.
  cp -R "${KUBE_REPO_ROOT}/cluster" "${release_stage}/"
  rm -rf "${release_stage}/cluster/saltbase"

  mkdir -p "${release_stage}/server"
  cp "${RELEASE_DIR}/kubernetes-salt.tar.gz" "${release_stage}/server/"
  cp "${RELEASE_DIR}"/kubernetes-server-*.tar.gz "${release_stage}/server/"

  mkdir -p "${release_stage}/third_party"
  cp -R "${KUBE_REPO_ROOT}/third_party/htpasswd" "${release_stage}/third_party/htpasswd"

  cp -R "${KUBE_REPO_ROOT}/examples" "${release_stage}/"
  cp "${KUBE_REPO_ROOT}/README.md" "${release_stage}/"
  cp "${KUBE_REPO_ROOT}/LICENSE" "${release_stage}/"
  cp "${KUBE_REPO_ROOT}/Vagrantfile" "${release_stage}/"

  local package_name="${RELEASE_DIR}/kubernetes.tar.gz"
  tar czf "${package_name}" -C "${release_stage}/.." .
}


# ---------------------------------------------------------------------------
# GCS Release

function kube::release::gcs::release() {
  [[ "${KUBE_GCS_UPLOAD_RELEASE}" == "y" ]] || return 0

  kube::release::gcs::verify_prereqs
  kube::release::gcs::ensure_release_bucket
  kube::release::gcs::push_images
  kube::release::gcs::copy_release_tarballs
}

# Verify things are set up for uploading to GCS
function kube::release::gcs::verify_prereqs() {
  if [[ -z "$(which gsutil)" || -z "$(which gcloud)" ]]; then
    echo "Releasing Kubernetes requires gsutil and gcloud.  Please download,"
    echo "install and authorize through the Google Cloud SDK: "
    echo
    echo "  https://developers.google.com/cloud/sdk/"
    return 1
  fi

  if [[ -z "${GCLOUD_ACCOUNT-}" ]]; then
    GCLOUD_ACCOUNT=$(gcloud auth list 2>/dev/null | awk '/(active)/ { print $2 }')
  fi
  if [[ -z "${GCLOUD_ACCOUNT}" ]]; then
    echo "No account authorized through gcloud.  Please fix with:"
    echo
    echo "  gcloud auth login"
    return 1
  fi

  if [[ -z "${GCLOUD_PROJECT-}" ]]; then
    GCLOUD_PROJECT=$(gcloud config list project | awk '{project = $3} END {print project}')
  fi
  if [[ -z "${GCLOUD_PROJECT}" ]]; then
    echo "No account authorized through gcloud.  Please fix with:"
    echo
    echo "  gcloud config set project <project id>"
    return 1
  fi
}

# Create a unique bucket name for releasing Kube and make sure it exists.
function kube::release::gcs::ensure_release_bucket() {
  local project_hash
  if which md5 > /dev/null 2>&1; then
    project_hash=$(md5 -q -s "$GCLOUD_PROJECT")
  else
    project_hash=$(echo -n "$GCLOUD_PROJECT" | md5sum)
  fi
  project_hash=${project_hash:0:5}
  KUBE_GCS_RELEASE_BUCKET=${KUBE_GCS_RELEASE_BUCKET-kubernetes-releases-${project_hash}}
  KUBE_GCS_RELEASE_PREFIX=${KUBE_GCS_RELEASE_PREFIX-devel/}
  KUBE_GCS_DOCKER_REG_PREFIX=${KUBE_GCS_DOCKER_REG_PREFIX-docker-reg/}

  if ! gsutil ls gs://${KUBE_GCS_RELEASE_BUCKET} >/dev/null 2>&1 ; then
    echo "Creating Google Cloud Storage bucket: $RELEASE_BUCKET"
    gsutil mb gs://${KUBE_GCS_RELEASE_BUCKET}
  fi
}

function kube::release::gcs::ensure_docker_registry() {
  local -r reg_container_name="gcs-registry"

  local -r running=$(docker inspect ${reg_container_name} 2>/dev/null \
    | build/json-extractor.py 0.State.Running 2>/dev/null)

  [[ "$running" != "true" ]] || return 0

  # Grovel around and find the OAuth token in the gcloud config
  local -r boto=~/.config/gcloud/legacy_credentials/${GCLOUD_ACCOUNT}/.boto
  local -r refresh_token=$(grep 'gs_oauth2_refresh_token =' $boto | awk '{ print $3 }')

  if [[ -z "$refresh_token" ]]; then
    echo "Couldn't find OAuth 2 refresh token in ${boto}" >&2
    return 1
  fi

  # If we have an old one sitting around, remove it
  docker rm ${reg_container_name} >/dev/null 2>&1 || true

  echo "+++ Starting GCS backed Docker registry"
  local docker="docker run -d --name=${reg_container_name} "
  docker+="-e GCS_BUCKET=${KUBE_GCS_RELEASE_BUCKET} "
  docker+="-e STORAGE_PATH=${KUBE_GCS_DOCKER_REG_PREFIX} "
  docker+="-e GCP_OAUTH2_REFRESH_TOKEN=${refresh_token} "
  docker+="-p 127.0.0.1:5000:5000 "
  docker+="google/docker-registry"

  ${docker}

  # Give it time to spin up before we start throwing stuff at it
  sleep 5
}

function kube::release::gcs::push_images() {
  [[ "${KUBE_BUILD_RUN_IMAGES}" == "y" ]] || return 0

  kube::release::gcs::ensure_docker_registry

  # Tag each of our run binaries with the right registry and push
  local b image_name
  for b in "${KUBE_RUN_IMAGES[@]}" ; do
    image_name="${KUBE_RUN_IMAGE_BASE}-${b}"
    echo "+++ Tagging and pushing ${image_name} to GCS bucket ${KUBE_GCS_RELEASE_BUCKET}"
    docker tag "${KUBE_RUN_IMAGE_BASE}-$b" "localhost:5000/${image_name}"
    docker push "localhost:5000/${image_name}"
    docker rmi "localhost:5000/${image_name}"
  done
}

function kube::release::gcs::copy_release_tarballs() {
  # TODO: This isn't atomic.  There will be points in time where there will be
  # no active release.  Also, if something fails, the release could be half-
  # copied.  The real way to do this would perhaps to have some sort of release
  # version so that we are never overwriting a destination.
  local -r gcs_destination="gs://${KUBE_GCS_RELEASE_BUCKET}/${KUBE_GCS_RELEASE_PREFIX}"
  local gcs_options=()

  if [[ ${KUBE_GCS_NO_CACHING} == "y" ]]; then
    gcs_options=("-h" "Cache-Control:private, max-age=0")
  fi

  echo "+++ Copying client tarballs to ${gcs_destination}"

  # First delete all objects at the destination
  gsutil -q rm -f -R "${gcs_destination}" >/dev/null 2>&1 || true

  # Now upload everything in release directory
  gsutil -m "${gcs_options[@]-}" cp -r "${RELEASE_DIR}"/* "${gcs_destination}" >/dev/null 2>&1

  if [[ ${KUBE_GCS_MAKE_PUBLIC} == "y" ]]; then
    gsutil acl ch -R -g all:R "${gcs_destination}" >/dev/null 2>&1
  fi

  gsutil ls -lh "${gcs_destination}"
}
