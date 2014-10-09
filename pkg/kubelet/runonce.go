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

package kubelet

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet/dockertools"
	"github.com/golang/glog"
)

const (
	RunOnceManifestDelay     = 1 * time.Second
	RunOnceMaxRetries        = 10
	RunOnceRetryDelay        = 1 * time.Second
	RunOnceRetryDelayBackoff = 2
)

type RunPodResult struct {
	Pod  *Pod
	Info api.PodInfo
	Err  error
}

// RunOnce polls from one configuration update and run the associated pods.
func (kl *Kubelet) RunOnce(updates <-chan PodUpdate) ([]RunPodResult, error) {
	select {
	case u := <-updates:
		glog.Infof("processing manifest with %d pods", len(u.Pods))
		result, err := kl.runOnce(u.Pods)
		glog.Infof("finished processing %d pods", len(u.Pods))
		return result, err
	case <-time.After(RunOnceManifestDelay):
		return nil, fmt.Errorf("no pod manifest update after %v", RunOnceManifestDelay)
	}
}

// runOnce runs a given set of pods and returns their status.
func (kl *Kubelet) runOnce(pods []Pod) (results []RunPodResult, err error) {
	if kl.dockerPuller == nil {
		kl.dockerPuller = dockertools.NewDockerPuller(kl.dockerClient, kl.pullQPS, kl.pullBurst)
	}
	pods = filterHostPortConflicts(pods)

	ch := make(chan RunPodResult)
	for i := range pods {
		pod := pods[i] // Make a copy
		go func() {
			info, err := kl.runPod(pod)
			ch <- RunPodResult{&pod, info, err}
		}()
	}

	glog.Infof("waiting for %d pods", len(pods))
	failedPods := []string{}
	for i := 0; i < len(pods); i++ {
		res := <-ch
		results = append(results, res)
		if res.Err != nil {
			// TODO(proppy): report which containers failed the pod.
			glog.Infof("failed to start pod %q: %v", res.Pod.Name, res.Err)
			failedPods = append(failedPods, res.Pod.Name)
		} else {
			glog.Infof("started pod %q: %#v", res.Pod.Name, res.Info)
		}
	}
	if len(failedPods) > 0 {
		return results, fmt.Errorf("error running pods: %v", failedPods)
	}
	glog.Infof("%d pods started", len(pods))
	return results, err
}

// Run a single pod and wait until all containers are running.
func (kl *Kubelet) runPod(pod Pod) (api.PodInfo, error) {
	dockerContainers, err := dockertools.GetKubeletDockerContainers(kl.dockerClient, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubelet docker containers: %v", err)
	}

	delay := RunOnceRetryDelay
	retry := 0
	for {
		running, info, err := kl.isPodRunning(pod)
		if err != nil {
			return nil, fmt.Errorf("error checking pod status: %v", err)
		}
		if running {
			glog.Infof("pod %q containers running", pod.Name)
			return info, nil
		}
		glog.Infof("pod %q containers not running: syncing", pod.Name)
		err = kl.syncPod(&pod, dockerContainers)
		if err != nil {
			return nil, fmt.Errorf("error syncing pod: %v", err)
		}
		if retry >= RunOnceMaxRetries {
			return info, fmt.Errorf("timeout error: pod %q containers not running after %d retries", pod.Name, RunOnceMaxRetries)
		}
		// TODO(proppy): health checking would be better than waiting + checking the state at the next iteration.
		glog.Infof("pod %q containers synced, waiting for %v", pod.Name, delay)
		<-time.After(delay)
		retry++
		delay *= RunOnceRetryDelayBackoff
	}
}

// Check if all containers of a manifest are running.
func (kl *Kubelet) isPodRunning(pod Pod) (bool, api.PodInfo, error) {
	info, err := kl.GetPodInfo(GetPodFullName(&pod), pod.Manifest.UUID)
	if err != nil {
		return false, nil, fmt.Errorf("error getting pod info: %v", err)
	}
	for _, container := range pod.Manifest.Containers {
		running := podInfo(info).isContainerRunning(container)
		glog.Infof("container %q running: %v", container.Name, running)
		if !running {
			return false, info, nil
		}
	}
	return true, info, nil
}

// Alias PodInfo for internal usage.
type podInfo api.PodInfo

func (info podInfo) isContainerRunning(container api.Container) bool {
	for name, status := range info {
		glog.Infof("container %q status: %#v", name, status)
		if name == container.Name && status.State.Running != nil {
			return true
		}
	}
	return false
}
