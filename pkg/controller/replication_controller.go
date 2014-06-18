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

package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
)

// ReplicationManager is responsible for synchronizing ReplicationController objects stored in etcd
// with actual running pods.
// TODO: Remove the etcd dependency and re-factor in terms of a generic watch interface
type ReplicationManager struct {
	etcdClient util.EtcdClient
	kubeClient client.ClientInterface
	podControl PodControlInterface
	syncTime   <-chan time.Time
}

// An interface that knows how to add or delete pods
// created as an interface to allow testing.
type PodControlInterface interface {
	createReplica(controllerSpec api.ReplicationController)
	deletePod(podID string) error
}

type RealPodControl struct {
	kubeClient client.ClientInterface
}

func (r RealPodControl) createReplica(controllerSpec api.ReplicationController) {
	labels := controllerSpec.DesiredState.PodTemplate.Labels
	if labels != nil {
		labels["replicationController"] = controllerSpec.ID
	}
	pod := api.Pod{
		JSONBase: api.JSONBase{
			ID: fmt.Sprintf("%x", rand.Int()),
		},
		DesiredState: controllerSpec.DesiredState.PodTemplate.DesiredState,
		Labels:       controllerSpec.DesiredState.PodTemplate.Labels,
	}
	_, err := r.kubeClient.CreatePod(pod)
	if err != nil {
		log.Printf("%#v\n", err)
	}
}

func (r RealPodControl) deletePod(podID string) error {
	return r.kubeClient.DeletePod(podID)
}

func MakeReplicationManager(etcdClient util.EtcdClient, kubeClient client.ClientInterface) *ReplicationManager {
	return &ReplicationManager{
		kubeClient: kubeClient,
		etcdClient: etcdClient,
		podControl: RealPodControl{
			kubeClient: kubeClient,
		},
	}
}

// Begin watching and syncing.
func (rm *ReplicationManager) Run(period time.Duration) {
	rm.syncTime = time.Tick(period)
	go util.Forever(func() { rm.watchControllers() }, period)
}

func (rm *ReplicationManager) watchControllers() {
	watchChannel := make(chan *etcd.Response)
	go func() {
		defer util.HandleCrash()
		defer func() {
			close(watchChannel)
		}()
		rm.etcdClient.Watch("/registry/controllers", 0, true, watchChannel, nil)
	}()

	for {
		select {
		case <-rm.syncTime:
			rm.synchronize()
		case watchResponse, ok := <-watchChannel:
			if !ok {
				// watchChannel has been closed. Let the util.Forever() that
				// called us call us again.
				return
			}
			if watchResponse == nil {
				time.Sleep(time.Second * 10)
				continue
			}
			log.Printf("Got watch: %#v", watchResponse)
			controller, err := rm.handleWatchResponse(watchResponse)
			if err != nil {
				log.Printf("Error handling data: %#v, %#v", err, watchResponse)
				continue
			}
			rm.syncReplicationController(*controller)
		}
	}
}

func (rm *ReplicationManager) handleWatchResponse(response *etcd.Response) (*api.ReplicationController, error) {
	if response.Action == "set" {
		if response.Node != nil {
			var controllerSpec api.ReplicationController
			err := json.Unmarshal([]byte(response.Node.Value), &controllerSpec)
			if err != nil {
				return nil, err
			}
			return &controllerSpec, nil
		} else {
			return nil, fmt.Errorf("response node is null %#v", response)
		}
	}
	return nil, nil
}

func (rm *ReplicationManager) filterActivePods(pods []api.Pod) []api.Pod {
	var result []api.Pod
	for _, value := range pods {
		if strings.Index(value.CurrentState.Status, "Exit") == -1 {
			result = append(result, value)
		}
	}
	return result
}

func (rm *ReplicationManager) syncReplicationController(controllerSpec api.ReplicationController) error {
	podList, err := rm.kubeClient.ListPods(controllerSpec.DesiredState.ReplicasInSet)
	if err != nil {
		return err
	}
	filteredList := rm.filterActivePods(podList.Items)
	diff := len(filteredList) - controllerSpec.DesiredState.Replicas
	log.Printf("%#v", filteredList)
	if diff < 0 {
		diff *= -1
		log.Printf("Too few replicas, creating %d\n", diff)
		for i := 0; i < diff; i++ {
			rm.podControl.createReplica(controllerSpec)
		}
	} else if diff > 0 {
		log.Print("Too many replicas, deleting")
		for i := 0; i < diff; i++ {
			rm.podControl.deletePod(filteredList[i].ID)
		}
	}
	return nil
}

func (rm *ReplicationManager) synchronize() {
	var controllerSpecs []api.ReplicationController
	helper := util.EtcdHelper{rm.etcdClient}
	err := helper.ExtractList("/registry/controllers", &controllerSpecs)
	if err != nil {
		log.Printf("Synchronization error: %v (%#v)", err, err)
	}
	for _, controllerSpec := range controllerSpecs {
		err = rm.syncReplicationController(controllerSpec)
		if err != nil {
			log.Printf("Error synchronizing: %#v", err)
		}
	}
}
