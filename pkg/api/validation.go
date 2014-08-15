/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"strings"

	errs "github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
)

func validateVolumes(volumes []Volume) (util.StringSet, errs.ErrorList) {
	allErrs := errs.ErrorList{}

	allNames := util.StringSet{}
	for i := range volumes {
		vol := &volumes[i] // so we can set default values
		el := errs.ErrorList{}
		// TODO(thockin) enforce that a source is set once we deprecate the implied form.
		if vol.Source != nil {
			el = validateSource(vol.Source)
		}
		if !util.IsDNSLabel(vol.Name) {
			el.Append(errs.NewInvalid("Volume.Name", vol.Name))
		} else if allNames.Has(vol.Name) {
			el.Append(errs.NewDuplicate("Volume.Name", vol.Name))
		}
		if len(el) == 0 {
			allNames.Insert(vol.Name)
		} else {
			allErrs.Append(el...)
		}
	}
	return allNames, allErrs
}

func validateSource(source *VolumeSource) errs.ErrorList {
	numVolumes := 0
	allErrs := errs.ErrorList{}
	if source.HostDirectory != nil {
		numVolumes++
		allErrs.Append(validateHostDir(source.HostDirectory)...)
	}
	if source.EmptyDirectory != nil {
		numVolumes++
		//EmptyDirs have nothing to validate
	}
	if numVolumes != 1 {
		allErrs.Append(errs.NewInvalid("Volume.Source", source))
	}
	return allErrs
}

func validateHostDir(hostDir *HostDirectory) errs.ErrorList {
	allErrs := errs.ErrorList{}
	if hostDir.Path == "" {
		allErrs.Append(errs.NewNotFound("HostDir.Path", hostDir.Path))
	}
	return allErrs
}

var supportedPortProtocols = util.NewStringSet("TCP", "UDP")

func validatePorts(ports []Port) errs.ErrorList {
	allErrs := errs.ErrorList{}

	allNames := util.StringSet{}
	for i := range ports {
		port := &ports[i] // so we can set default values
		if len(port.Name) > 0 {
			if len(port.Name) > 63 || !util.IsDNSLabel(port.Name) {
				allErrs.Append(errs.NewInvalid("Port.Name", port.Name))
			} else if allNames.Has(port.Name) {
				allErrs.Append(errs.NewDuplicate("Port.name", port.Name))
			} else {
				allNames.Insert(port.Name)
			}
		}
		if !util.IsValidPortNum(port.ContainerPort) {
			allErrs.Append(errs.NewInvalid("Port.ContainerPort", port.ContainerPort))
		}
		if port.HostPort == 0 {
			port.HostPort = port.ContainerPort
		} else if !util.IsValidPortNum(port.HostPort) {
			allErrs.Append(errs.NewInvalid("Port.HostPort", port.HostPort))
		}
		if len(port.Protocol) == 0 {
			port.Protocol = "TCP"
		} else if !supportedPortProtocols.Has(strings.ToUpper(port.Protocol)) {
			allErrs.Append(errs.NewNotSupported("Port.Protocol", port.Protocol))
		}
	}
	return allErrs
}

func validateEnv(vars []EnvVar) errs.ErrorList {
	allErrs := errs.ErrorList{}

	for i := range vars {
		ev := &vars[i] // so we can set default values
		if len(ev.Name) == 0 {
			allErrs.Append(errs.NewInvalid("EnvVar.Name", ev.Name))
		}
		if !util.IsCIdentifier(ev.Name) {
			allErrs.Append(errs.NewInvalid("EnvVar.Name", ev.Name))
		}
	}
	return allErrs
}

func validateVolumeMounts(mounts []VolumeMount, volumes util.StringSet) errs.ErrorList {
	allErrs := errs.ErrorList{}

	for i := range mounts {
		mnt := &mounts[i] // so we can set default values
		if len(mnt.Name) == 0 {
			allErrs.Append(errs.NewInvalid("VolumeMount.Name", mnt.Name))
		} else if !volumes.Has(mnt.Name) {
			allErrs.Append(errs.NewNotFound("VolumeMount.Name", mnt.Name))
		}
		if len(mnt.MountPath) == 0 {
			// Backwards compat.
			if len(mnt.Path) == 0 {
				allErrs.Append(errs.NewInvalid("VolumeMount.MountPath", mnt.MountPath))
			} else {
				glog.Warning("DEPRECATED: VolumeMount.Path has been replaced by VolumeMount.MountPath")
				mnt.MountPath = mnt.Path
				mnt.Path = ""
			}
		}
		if len(mnt.MountType) != 0 {
			glog.Warning("DEPRECATED: VolumeMount.MountType will be removed. The Volume struct will handle types")
		}
	}
	return allErrs
}

// AccumulateUniquePorts runs an extraction function on each Port of each Container,
// accumulating the results and returning an error if any ports conflict.
func AccumulateUniquePorts(containers []Container, accumulator map[int]bool, extract func(*Port) int) errs.ErrorList {
	allErrs := errs.ErrorList{}

	for ci := range containers {
		ctr := &containers[ci]
		for pi := range ctr.Ports {
			port := extract(&ctr.Ports[pi])
			if accumulator[port] {
				allErrs.Append(errs.NewDuplicate("Port", port))
			} else {
				accumulator[port] = true
			}
		}
	}
	return allErrs
}

// Checks for colliding Port.HostPort values across a slice of containers.
func checkHostPortConflicts(containers []Container) errs.ErrorList {
	allPorts := map[int]bool{}
	return AccumulateUniquePorts(containers, allPorts, func(p *Port) int { return p.HostPort })
}

func validateContainers(containers []Container, volumes util.StringSet) errs.ErrorList {
	allErrs := errs.ErrorList{}

	allNames := util.StringSet{}
	for i := range containers {
		ctr := &containers[i] // so we can set default values
		if !util.IsDNSLabel(ctr.Name) {
			allErrs.Append(errs.NewInvalid("Container.Name", ctr.Name))
		} else if allNames.Has(ctr.Name) {
			allErrs.Append(errs.NewDuplicate("Container.Name", ctr.Name))
		} else {
			allNames.Insert(ctr.Name)
		}
		if len(ctr.Image) == 0 {
			allErrs.Append(errs.NewInvalid("Container.Image", ctr.Name))
		}
		allErrs.Append(validatePorts(ctr.Ports)...)
		allErrs.Append(validateEnv(ctr.Env)...)
		allErrs.Append(validateVolumeMounts(ctr.VolumeMounts, volumes)...)
	}
	// Check for colliding ports across all containers.
	// TODO(thockin): This really is dependent on the network config of the host (IP per pod?)
	// and the config of the new manifest.  But we have not specced that out yet, so we'll just
	// make some assumptions for now.  As of now, pods share a network namespace, which means that
	// every Port.HostPort across the whole pod must be unique.
	allErrs.Append(checkHostPortConflicts(containers)...)

	return allErrs
}

var supportedManifestVersions = util.NewStringSet("v1beta1", "v1beta2")

// ValidateManifest tests that the specified ContainerManifest has valid data.
// This includes checking formatting and uniqueness.  It also canonicalizes the
// structure by setting default values and implementing any backwards-compatibility
// tricks.
func ValidateManifest(manifest *ContainerManifest) []error {
	allErrs := errs.ErrorList{}

	if len(manifest.Version) == 0 {
		allErrs.Append(errs.NewInvalid("ContainerManifest.Version", manifest.Version))
	} else if !supportedManifestVersions.Has(strings.ToLower(manifest.Version)) {
		allErrs.Append(errs.NewNotSupported("ContainerManifest.Version", manifest.Version))
	}
	allVolumes, errs := validateVolumes(manifest.Volumes)
	if len(errs) != 0 {
		allErrs.Append(errs...)
	}
	allErrs.Append(validateContainers(manifest.Containers, allVolumes)...)
	return []error(allErrs)
}

func ValidatePodState(podState *PodState) []error {
	allErrs := errs.ErrorList(ValidateManifest(&podState.Manifest))
	if podState.RestartPolicy.Type == "" {
		podState.RestartPolicy.Type = RestartAlways
	} else if podState.RestartPolicy.Type != RestartAlways &&
		podState.RestartPolicy.Type != RestartOnFailure &&
		podState.RestartPolicy.Type != RestartNever {
		allErrs.Append(errs.NewNotSupported("PodState.RestartPolicy.Type", podState.RestartPolicy.Type))
	}

	return []error(allErrs)
}

// Pod tests if required fields in the pod are set.
func ValidatePod(pod *Pod) []error {
	allErrs := errs.ErrorList{}
	if pod.ID == "" {
		allErrs.Append(errs.NewInvalid("Pod.ID", pod.ID))
	}
	allErrs.Append(ValidatePodState(&pod.DesiredState)...)
	return []error(allErrs)
}

// ValidateService tests if required fields in the service are set.
func ValidateService(service *Service) []error {
	allErrs := errs.ErrorList{}
	if service.ID == "" {
		allErrs.Append(errs.NewInvalid("Service.ID", service.ID))
	} else if !util.IsDNS952Label(service.ID) {
		allErrs.Append(errs.NewInvalid("Service.ID", service.ID))
	}
	if labels.Set(service.Selector).AsSelector().Empty() {
		allErrs.Append(errs.NewInvalid("Service.Selector", service.Selector))
	}
	return []error(allErrs)
}

// ValidateReplicationController tests if required fields in the replication controller are set.
func ValidateReplicationController(controller *ReplicationController) []error {
	el := []error{}
	if controller.ID == "" {
		el = append(el, errs.NewInvalid("ReplicationController.ID", controller.ID))
	}
	if labels.Set(controller.DesiredState.ReplicaSelector).AsSelector().Empty() {
		el = append(el, errs.NewInvalid("ReplicationController.ReplicaSelector", controller.DesiredState.ReplicaSelector))
	}
	if controller.DesiredState.Replicas < 0 {
		el = append(el, errs.NewInvalid("ReplicationController.Replicas", controller.DesiredState.Replicas))
	}
	el = append(el, ValidateManifest(&controller.DesiredState.PodTemplate.DesiredState.Manifest)...)
	return el
}
