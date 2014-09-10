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

# This script sets up a go workspace locally and builds all go components.
# You can 'source' this file if you want to set up GOPATH in your local shell.

# --- Helper Functions ---

# Function kube::version_ldflags() prints the value that needs to be passed to
# the -ldflags parameter of go build in order to set the Kubernetes based on the
# git tree status.
kube::version_ldflags() {
  (
    # Run this in a subshell to prevent settings/variables from leaking.
    set -o errexit
    set -o nounset
    set -o pipefail

    unset CDPATH

    cd "${KUBE_REPO_ROOT}"

    declare -a ldflags=()
    if [[ -n ${KUBE_GIT_COMMIT-} ]] || KUBE_GIT_COMMIT=$(git rev-parse "HEAD^{commit}" 2>/dev/null); then
      ldflags+=(-X "${KUBE_GO_PACKAGE}/pkg/version.gitCommit" "${KUBE_GIT_COMMIT}")

      if [[ -z ${KUBE_GIT_TREE_STATE-} ]]; then
        # Check if the tree is dirty.  default to dirty
        if git_status=$(git status --porcelain 2>/dev/null) && [[ -z ${git_status} ]]; then
          KUBE_GIT_TREE_STATE="clean"
        else
          KUBE_GIT_TREE_STATE="dirty"
        fi
      fi
      ldflags+=(-X "${KUBE_GO_PACKAGE}/pkg/version.gitTreeState" "${KUBE_GIT_TREE_STATE}")

      # Use git describe to find the version based on annotated tags.
      if [[ -n ${KUBE_GIT_VERSION-} ]] || KUBE_GIT_VERSION=$(git describe --abbrev=14 "${KUBE_GIT_COMMIT}^{commit}" 2>/dev/null); then
        if [[ "${KUBE_GIT_TREE_STATE}" == "dirty" ]]; then
          # git describe --dirty only considers changes to existing files, but
          # that is problematic since new untracked .go files affect the build,
          # so use our idea of "dirty" from git status instead.
          KUBE_GIT_VERSION+="-dirty"
        fi
        ldflags+=(-X "${KUBE_GO_PACKAGE}/pkg/version.gitVersion" "${KUBE_GIT_VERSION}")

        # Try to match the "git describe" output to a regex to try to extract
        # the "major" and "minor" versions and whether this is the exact tagged
        # version or whether the tree is between two tagged versions.
        if [[ "${KUBE_GIT_VERSION}" =~ ^v([0-9]+)\.([0-9]+)([.-].*)?$ ]]; then
          git_major=${BASH_REMATCH[1]}
          git_minor=${BASH_REMATCH[2]}
          if [[ -n "${BASH_REMATCH[3]}" ]]; then
            git_minor+="+"
          fi
          ldflags+=(
            -X "${KUBE_GO_PACKAGE}/pkg/version.gitMajor" "${git_major}"
            -X "${KUBE_GO_PACKAGE}/pkg/version.gitMinor" "${git_minor}"
          )
        fi
      fi
    fi

    # The -ldflags parameter takes a single string, so join the output.
    echo "${ldflags[*]-}"
  )
}

# kube::setup_go_environment will check that the `go` commands is available in
# ${PATH}. If not running on Travis, it will also check that the Go version is
# good enough for the Kubernetes build.
#
# Also set ${GOPATH} and environment variables needed by Go.
kube::setup_go_environment() {
  if [[ -z "$(which go)" ]]; then
    echo "Can't find 'go' in PATH, please fix and retry." >&2
    echo "See http://golang.org/doc/install for installation instructions." >&2
    exit 1
  fi

  # Travis continuous build uses a head go release that doesn't report
  # a version number, so we skip this check on Travis.  Its unnecessary
  # there anyway.
  if [[ "${TRAVIS:-}" != "true" ]]; then
    local go_version
    go_version=($(go version))
    if [[ "${go_version[2]}" < "go1.2" ]]; then
      echo "Detected go version: ${go_version[*]}." >&2
      echo "Kubernetes requires go version 1.2 or greater." >&2
      echo "Please install Go version 1.2 or later" >&2
      exit 1
    fi
  fi

  GOPATH=${KUBE_TARGET}
  # Append KUBE_EXTRA_GOPATH to the GOPATH if it is defined.
  if [[ -n ${KUBE_EXTRA_GOPATH:-} ]]; then
    GOPATH=${GOPATH}:${KUBE_EXTRA_GOPATH}
  fi
  # Set GOPATH to point to the tree maintained by `godep`.
  GOPATH="${GOPATH}:${KUBE_REPO_ROOT}/Godeps/_workspace"
  export GOPATH

  # Unset GOBIN in case it already exists in the current session.
  unset GOBIN
}


# kube::default_build_targets return list of all build targets
kube::default_build_targets() {
  echo "cmd/proxy"
  echo "cmd/apiserver"
  echo "cmd/controller-manager"
  echo "cmd/kubelet"
  echo "cmd/kubecfg"
  echo "plugin/cmd/scheduler"
}

# kube::binaries_from_targets take a list of build targets and return the
# full go package to be built
kube::binaries_from_targets() {
  local target
  for target; do
    echo "${KUBE_GO_PACKAGE}/${target}"
  done
}
# --- Environment Variables ---

# KUBE_REPO_ROOT  - Path to the top of the build tree.
# KUBE_TARGET     - Path where output Go files are saved.
# KUBE_GO_PACKAGE - Full name of the Kubernetes Go package.

# Make ${KUBE_REPO_ROOT} an absolute path.
KUBE_REPO_ROOT=$(
  set -eu
  unset CDPATH
  scripts_dir=$(dirname "${BASH_SOURCE[0]}")
  cd "${scripts_dir}"
  cd ..
  pwd
)
export KUBE_REPO_ROOT

KUBE_TARGET="${KUBE_REPO_ROOT}/_output/go"
mkdir -p "${KUBE_TARGET}"
export KUBE_TARGET

KUBE_GO_PACKAGE=github.com/GoogleCloudPlatform/kubernetes
export KUBE_GO_PACKAGE

(
  # Create symlink named ${KUBE_GO_PACKAGE} under _output/go/src.
  # So that Go knows how to import Kubernetes sources by full path.
  # Use a subshell to avoid leaking these variables.

  set -eu
  go_pkg_dir="${KUBE_TARGET}/src/${KUBE_GO_PACKAGE}"
  go_pkg_basedir=$(dirname "${go_pkg_dir}")
  mkdir -p "${go_pkg_basedir}"
  rm -f "${go_pkg_dir}"
  # TODO: This symlink should be relative.
  ln -s "${KUBE_REPO_ROOT}" "${go_pkg_dir}"
)

