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

package scheduler

import (
	"fmt"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/golang/glog"
)

type NodeInfo interface {
	GetNodeInfo(nodeID string) (*api.Node, error)
}

type StaticNodeInfo struct {
	*api.NodeList
}

func (nodes StaticNodeInfo) GetNodeInfo(nodeID string) (*api.Node, error) {
	for ix := range nodes.Items {
		if nodes.Items[ix].Name == nodeID {
			return &nodes.Items[ix], nil
		}
	}
	return nil, fmt.Errorf("failed to find node: %s, %#v", nodeID, nodes)
}

type ClientNodeInfo struct {
	*client.Client
}

func (nodes ClientNodeInfo) GetNodeInfo(nodeID string) (*api.Node, error) {
	return nodes.Nodes().Get(nodeID)
}

func isVolumeConflict(volume api.Volume, pod *api.Pod) bool {
	if volume.Source.GCEPersistentDisk == nil {
		return false
	}
	pdName := volume.Source.GCEPersistentDisk.PDName

	manifest := &(pod.Spec)
	for ix := range manifest.Volumes {
		if manifest.Volumes[ix].Source.GCEPersistentDisk != nil &&
			manifest.Volumes[ix].Source.GCEPersistentDisk.PDName == pdName {
			return true
		}
	}
	return false
}

// NoDiskConflict evaluates if a pod can fit due to the volumes it requests, and those that
// are already mounted. Some times of volumes are mounted onto node machines.  For now, these mounts
// are exclusive so if there is already a volume mounted on that node, another pod can't schedule
// there. This is GCE specific for now.
// TODO: migrate this into some per-volume specific code?
func NoDiskConflict(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	manifest := &(pod.Spec)
	for ix := range manifest.Volumes {
		for podIx := range existingPods {
			if isVolumeConflict(manifest.Volumes[ix], &existingPods[podIx]) {
				return false, nil
			}
		}
	}
	return true, nil
}

type ResourceFit struct {
	info NodeInfo
}

type resourceRequest struct {
	milliCPU int64
	memory   int64
}

func getResourceRequest(pod *api.Pod) resourceRequest {
	result := resourceRequest{}
	for ix := range pod.Spec.Containers {
		result.memory += pod.Spec.Containers[ix].Memory.Value()
		result.milliCPU += pod.Spec.Containers[ix].CPU.MilliValue()
	}
	return result
}

// PodFitsResources calculates fit based on requested, rather than used resources
func (r *ResourceFit) PodFitsResources(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	podRequest := getResourceRequest(&pod)
	if podRequest.milliCPU == 0 && podRequest.memory == 0 {
		// no resources requested always fits.
		return true, nil
	}
	info, err := r.info.GetNodeInfo(node)
	if err != nil {
		return false, err
	}
	milliCPURequested := int64(0)
	memoryRequested := int64(0)
	for ix := range existingPods {
		existingRequest := getResourceRequest(&existingPods[ix])
		milliCPURequested += existingRequest.milliCPU
		memoryRequested += existingRequest.memory
	}

	totalMilliCPU := info.Spec.Capacity.Get(api.ResourceCPU).MilliValue()
	totalMemory := info.Spec.Capacity.Get(api.ResourceMemory).Value()

	fitsCPU := totalMilliCPU == 0 || (totalMilliCPU-milliCPURequested) >= podRequest.milliCPU
	fitsMemory := totalMemory == 0 || (totalMemory-memoryRequested) >= podRequest.memory
	glog.V(3).Infof("Calculated fit: cpu: %s, memory %s", fitsCPU, fitsMemory)

	return fitsCPU && fitsMemory, nil
}

func NewResourceFitPredicate(info NodeInfo) FitPredicate {
	fit := &ResourceFit{
		info: info,
	}
	return fit.PodFitsResources
}

func NewSelectorMatchPredicate(info NodeInfo) FitPredicate {
	selector := &NodeSelector{
		info: info,
	}
	return selector.PodSelectorMatches
}

type NodeSelector struct {
	info NodeInfo
}

func (n *NodeSelector) PodSelectorMatches(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	if len(pod.Spec.NodeSelector) == 0 {
		return true, nil
	}
	selector := labels.SelectorFromSet(pod.Spec.NodeSelector)
	minion, err := n.info.GetNodeInfo(node)
	if err != nil {
		return false, err
	}
	return selector.Matches(labels.Set(minion.Labels)), nil
}

func PodFitsHost(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	if len(pod.Spec.Host) == 0 {
		return true, nil
	}
	return pod.Spec.Host == node, nil
}

type NodeLabelChecker struct {
	info     NodeInfo
	labels   []string
	presence bool
}

func NewNodeLabelPredicate(info NodeInfo, labels []string, presence bool) FitPredicate {
	labelChecker := &NodeLabelChecker{
		info:     info,
		labels:   labels,
		presence: presence,
	}
	return labelChecker.CheckNodeLabelPresence
}

// CheckNodeLabelPresence checks whether a particular label exists on a minion or not, regardless of its value
// Consider the cases where the minions are placed in regions/zones/racks and these are identified by labels
// In some cases, it is required that only minions that are part of ANY of the defined regions/zones/racks be selected
//
// Alternately, eliminating minions that have a certain label, regardless of value, is also useful
// A minion may have a label with "retiring" as key and the date as the value
// and it may be desirable to avoid scheduling new pods on this minion
func (n *NodeLabelChecker) CheckNodeLabelPresence(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	var exists bool
	minion, err := n.info.GetNodeInfo(node)
	if err != nil {
		return false, err
	}
	for _, label := range n.labels {
		exists = labels.Set(minion.Labels).Has(label)
		if (exists && !n.presence) || (!exists && n.presence) {
			return false, nil
		}
	}
	return true, nil
}

type ServiceAffinity struct {
	podLister     PodLister
	serviceLister ServiceLister
	nodeInfo      NodeInfo
	labels        []string
}

func NewServiceAffinityPredicate(podLister PodLister, serviceLister ServiceLister, nodeInfo NodeInfo, labels []string) FitPredicate {
	affinity := &ServiceAffinity{
		podLister:     podLister,
		serviceLister: serviceLister,
		nodeInfo:      nodeInfo,
		labels:        labels,
	}
	return affinity.CheckServiceAffinity
}

func (s *ServiceAffinity) CheckServiceAffinity(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	var affinitySelector labels.Selector

	// check if the pod being scheduled has the affinity labels specified
	affinityLabels := map[string]string{}
	labelsExist := true
	for _, l := range s.labels {
		if labels.Set(pod.Labels).Has(l) {
			affinityLabels[l] = labels.Set(pod.Labels).Get(l)
		} else {
			// the current pod does not specify all the labels, look in the existing service pods
			labelsExist = false
		}
	}

	// skip looking at other pods in the service if the current pod defines all the required affinity labels
	if !labelsExist {
		service, err := s.serviceLister.GetPodService(pod)
		if err == nil {
			selector := labels.SelectorFromSet(service.Spec.Selector)
			servicePods, err := s.podLister.ListPods(selector)
			if err != nil {
				return false, err
			}
			if len(servicePods) > 0 {
				// consider any service pod and fetch the minion its hosted on
				otherMinion, err := s.nodeInfo.GetNodeInfo(servicePods[0].Status.Host)
				if err != nil {
					return false, err
				}
				for _, l := range s.labels {
					// If the pod being scheduled has the label value specified, do not override it
					if _, exists := affinityLabels[l]; exists {
						continue
					}
					if labels.Set(otherMinion.Labels).Has(l) {
						affinityLabels[l] = labels.Set(otherMinion.Labels).Get(l)
					}
				}
			}
		}
	}

	// if there are no existing pods in the service, consider all minions
	if len(affinityLabels) == 0 {
		affinitySelector = labels.Everything()
	} else {
		affinitySelector = labels.Set(affinityLabels).AsSelector()
	}

	minion, err := s.nodeInfo.GetNodeInfo(node)
	if err != nil {
		return false, err
	}

	// check if the minion matches the selector
	return affinitySelector.Matches(labels.Set(minion.Labels)), nil
}

func PodFitsPorts(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	existingPorts := getUsedPorts(existingPods...)
	wantPorts := getUsedPorts(pod)
	for wport := range wantPorts {
		if wport == 0 {
			continue
		}
		if existingPorts[wport] {
			return false, nil
		}
	}
	return true, nil
}

func getUsedPorts(pods ...api.Pod) map[int]bool {
	ports := make(map[int]bool)
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			for _, podPort := range container.Ports {
				ports[podPort.HostPort] = true
			}
		}
	}
	return ports
}

// MapPodsToMachines obtains a list of pods and pivots that list into a map where the keys are host names
// and the values are the list of pods running on that host.
func MapPodsToMachines(lister PodLister) (map[string][]api.Pod, error) {
	machineToPods := map[string][]api.Pod{}
	// TODO: perform more targeted query...
	pods, err := lister.List(labels.Everything())
	if err != nil {
		return map[string][]api.Pod{}, err
	}
	for _, scheduledPod := range pods {
		host := scheduledPod.Status.Host
		machineToPods[host] = append(machineToPods[host], scheduledPod)
	}
	return machineToPods, nil
}
