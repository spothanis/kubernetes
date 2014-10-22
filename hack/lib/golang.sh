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

# The golang package that we are building.
readonly KUBE_GO_PACKAGE=github.com/GoogleCloudPlatform/kubernetes
readonly KUBE_GOPATH="${KUBE_OUTPUT}/go"

# The set of server targets that we are only building for Linux
readonly KUBE_SERVER_TARGETS=(
  cmd/proxy
  cmd/apiserver
  cmd/controller-manager
  cmd/kubelet
  plugin/cmd/scheduler
)
readonly KUBE_SERVER_BINARIES=("${KUBE_SERVER_TARGETS[@]##*/}")

# The server platform we are building on.
readonly KUBE_SERVER_PLATFORMS=(
  linux/amd64
)

# The set of client targets that we are building for all platforms
readonly KUBE_CLIENT_TARGETS=(
  cmd/kubecfg
  cmd/kubectl
)
readonly KUBE_CLIENT_BINARIES=("${KUBE_CLIENT_TARGETS[@]##*/}")

# The set of test targets that we are building for all platforms
readonly KUBE_TEST_TARGETS=(
  cmd/e2e
  cmd/integration
)
readonly KUBE_TEST_BINARIES=("${KUBE_TEST_TARGETS[@]##*/}")

# If we update this we need to also update the set of golang compilers we build
# in 'build/build-image/Dockerfile'
readonly KUBE_CLIENT_PLATFORMS=(
  linux/amd64
  linux/386
  linux/arm
  darwin/amd64
  darwin/386
)

readonly KUBE_ALL_TARGETS=(
  "${KUBE_SERVER_TARGETS[@]}"
  "${KUBE_CLIENT_TARGETS[@]}"
  "${KUBE_TEST_TARGETS[@]}"
)
readonly KUBE_ALL_BINARIES=("${KUBE_ALL_TARGETS[@]##*/}")


# kube::binaries_from_targets take a list of build targets and return the
# full go package to be built
kube::golang::binaries_from_targets() {
  local target
  for target; do
    echo "${KUBE_GO_PACKAGE}/${target}"
  done
}

# Asks golang what it thinks the host platform is.  The go tool chain does some
# slightly different things when the target platform matches the host platform.
kube::golang::host_platform() {
  echo "$(go env GOHOSTOS)/$(go env GOHOSTARCH)"
}

kube::golang::current_platform() {
  local os="${GOOS-}"
  if [[ -z $os ]]; then
    os=$(go env GOHOSTOS)
  fi

  local arch="${GOARCH-}"
  if [[ -z $arch ]]; then
    arch=$(go env GOHOSTARCH)
  fi

  echo "$os/$arch"
}

# Takes the the platform name ($1) and sets the appropriate golang env variables
# for that platform.
kube::golang::set_platform_envs() {
  [[ -n ${1-} ]] || {
    kube::log::error_exit "!!! Internal error.  No platform set in kube::golang::set_platform_envs"
  }

  export GOOS=${platform%/*}
  export GOARCH=${platform##*/}
}

kube::golang::unset_platform_envs() {
  unset GOOS
  unset GOARCH
}

# Create the GOPATH tree under $KUBE_OUTPUT
kube::golang::create_gopath_tree() {
  local go_pkg_dir="${KUBE_GOPATH}/src/${KUBE_GO_PACKAGE}"
  local go_pkg_basedir=$(dirname "${go_pkg_dir}")

  mkdir -p "${go_pkg_basedir}"
  rm -f "${go_pkg_dir}"

  # TODO: This symlink should be relative.
  ln -s "${KUBE_ROOT}" "${go_pkg_dir}"
}

# kube::golang::setup_env will check that the `go` commands is available in
# ${PATH}. If not running on Travis, it will also check that the Go version is
# good enough for the Kubernetes build.
#
# Input Vars:
#   KUBE_EXTRA_GOPATH - If set, this is included in created GOPATH
#   KUBE_NO_GODEPS - If set, we don't add 'Godeps/_workspace' to GOPATH
#
# Output Vars:
#   export GOPATH - A modified GOPATH to our created tree along with extra
#     stuff.
#   export GOBIN - This is actively unset if already set as we want binaries
#     placed in a predictable place.
kube::golang::setup_env() {
  kube::golang::create_gopath_tree

  if [[ -z "$(which go)" ]]; then
    kube::log::usage_from_stdin <<EOF

Can't find 'go' in PATH, please fix and retry.
See http://golang.org/doc/install for installation instructions.

EOF
    exit 2
  fi

  # Travis continuous build uses a head go release that doesn't report
  # a version number, so we skip this check on Travis.  It's unnecessary
  # there anyway.
  if [[ "${TRAVIS:-}" != "true" ]]; then
    local go_version
    go_version=($(go version))
    if [[ "${go_version[2]}" < "go1.2" ]]; then
      kube::log::usage_from_stdin <<EOF

Detected go version: ${go_version[*]}.
Kubernetes requires go version 1.2 or greater.
Please install Go version 1.2 or later.

EOF
      exit 2
    fi
  fi

  GOPATH=${KUBE_GOPATH}

  # Append KUBE_EXTRA_GOPATH to the GOPATH if it is defined.
  if [[ -n ${KUBE_EXTRA_GOPATH:-} ]]; then
    GOPATH="${GOPATH}:${KUBE_EXTRA_GOPATH}"
  fi

  # Append the tree maintained by `godep` to the GOPATH unless KUBE_NO_GODEPS
  # is defined.
  if [[ -z ${KUBE_NO_GODEPS:-} ]]; then
    GOPATH="${GOPATH}:${KUBE_ROOT}/Godeps/_workspace"
  fi
  export GOPATH

  # Unset GOBIN in case it already exists in the current session.
  unset GOBIN
}

# This will take binaries from $GOPATH/bin and copy them to the appropriate
# place in ${KUBE_OUTPUT_BINDIR}
#
# Ideally this wouldn't be necessary and we could just set GOBIN to
# KUBE_OUTPUT_BINDIR but that won't work in the face of cross compilation.  'go
# install' will place binaries that match the host platform directly in $GOBIN
# while placing cross compiled binaries into `platform_arch` subdirs.  This
# complicates pretty much everything else we do around packaging and such.
kube::golang::place_bins() {
  local host_platform
  host_platform=$(kube::golang::host_platform)

  kube::log::status "Placing binaries"

  local platform
  for platform in "${KUBE_CLIENT_PLATFORMS[@]}"; do
    # The substitution on platform_src below will replace all slashes with
    # underscores.  It'll transform darwin/amd64 -> darwin_amd64.
    local platform_src="/${platform//\//_}"
    if [[ $platform == $host_platform ]]; then
      platform_src=""
    fi

    local full_binpath_src="${KUBE_GOPATH}/bin${platform_src}"
    if [[ -d "${full_binpath_src}" ]]; then
      mkdir -p "${KUBE_OUTPUT_BINPATH}/${platform}"
      find "${full_binpath_src}" -maxdepth 1 -type f -exec \
        rsync -pt {} "${KUBE_OUTPUT_BINPATH}/${platform}" \;
    fi
  done
}

# Build binaries targets specified
#
# Input:
#   $@ - targets and go flags.  If no targets are set then all binaries targets
#     are built.
#   KUBE_BUILD_PLATFORMS - Incoming variable of targets to build for.  If unset
#     then just the host architecture is built.
kube::golang::build_binaries() {
  # Create a sub-shell so that we don't pollute the outer environment
  (
    # Check for `go` binary and set ${GOPATH}.
    kube::golang::setup_env

    # Fetch the version.
    local version_ldflags
    version_ldflags=$(kube::version::ldflags)

    # Use eval to preserve embedded quoted strings.
    local goflags
    eval "goflags=(${KUBE_GOFLAGS:-})"

    local -a targets=()
    local arg
    for arg; do
      if [[ "${arg}" == -* ]]; then
        # Assume arguments starting with a dash are flags to pass to go.
        goflags+=("${arg}")
      else
        targets+=("${arg}")
      fi
    done

    if [[ ${#targets[@]} -eq 0 ]]; then
      targets=("${KUBE_ALL_TARGETS[@]}")
    fi

    local -a platforms=("${KUBE_BUILD_PLATFORMS[@]:+${KUBE_BUILD_PLATFORMS[@]}}")
    if [[ ${#platforms[@]} -eq 0 ]]; then
      platforms=("$(kube::golang::host_platform)")
    fi

    local binaries
    binaries=($(kube::golang::binaries_from_targets "${targets[@]}"))

    local platform
    for platform in "${platforms[@]}"; do
      kube::golang::set_platform_envs "${platform}"
      kube::log::status "Building go targets for ${platform}:" "${targets[@]}"
      go install "${goflags[@]:+${goflags[@]}}" \
          -ldflags "${version_ldflags}" \
          "${binaries[@]}"
    done
  )
}
