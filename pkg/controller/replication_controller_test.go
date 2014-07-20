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
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/tools"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
)

// TODO: Move this to a common place, it's needed in multiple tests.
var apiPath = "/api/v1beta1"

func makeURL(suffix string) string {
	return apiPath + suffix
}

type FakePodControl struct {
	controllerSpec []api.ReplicationController
	deletePodID    []string
}

func (f *FakePodControl) createReplica(spec api.ReplicationController) {
	f.controllerSpec = append(f.controllerSpec, spec)
}

func (f *FakePodControl) deletePod(podID string) error {
	f.deletePodID = append(f.deletePodID, podID)
	return nil
}

func makeReplicationController(replicas int) api.ReplicationController {
	return api.ReplicationController{
		DesiredState: api.ReplicationControllerState{
			Replicas: replicas,
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						Containers: []api.Container{
							{
								Image: "foo/bar",
							},
						},
					},
				},
				Labels: map[string]string{
					"name": "foo",
					"type": "production",
				},
			},
		},
	}
}

func makePodList(count int) api.PodList {
	pods := []api.Pod{}
	for i := 0; i < count; i++ {
		pods = append(pods, api.Pod{
			JSONBase: api.JSONBase{
				ID: fmt.Sprintf("pod%d", i),
			},
		})
	}
	return api.PodList{
		Items: pods,
	}
}

func validateSyncReplication(t *testing.T, fakePodControl *FakePodControl, expectedCreates, expectedDeletes int) {
	if len(fakePodControl.controllerSpec) != expectedCreates {
		t.Errorf("Unexpected number of creates.  Expected %d, saw %d\n", expectedCreates, len(fakePodControl.controllerSpec))
	}
	if len(fakePodControl.deletePodID) != expectedDeletes {
		t.Errorf("Unexpected number of deletes.  Expected %d, saw %d\n", expectedDeletes, len(fakePodControl.deletePodID))
	}
}

func TestSyncReplicationControllerDoesNothing(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl

	controllerSpec := makeReplicationController(2)

	manager.syncReplicationController(controllerSpec)
	validateSyncReplication(t, &fakePodControl, 0, 0)
}

func TestSyncReplicationControllerDeletes(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl

	controllerSpec := makeReplicationController(1)

	manager.syncReplicationController(controllerSpec)
	validateSyncReplication(t, &fakePodControl, 0, 1)
}

func TestSyncReplicationControllerCreates(t *testing.T) {
	body := "{ \"items\": [] }"
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl

	controllerSpec := makeReplicationController(2)

	manager.syncReplicationController(controllerSpec)
	validateSyncReplication(t, &fakePodControl, 2, 0)
}

func TestCreateReplica(t *testing.T) {
	body := "{}"
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	podControl := RealPodControl{
		kubeClient: client,
	}

	controllerSpec := api.ReplicationController{
		DesiredState: api.ReplicationControllerState{
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						Containers: []api.Container{
							{
								Image: "foo/bar",
							},
						},
					},
				},
				Labels: map[string]string{
					"name": "foo",
					"type": "production",
				},
			},
		},
	}

	podControl.createReplica(controllerSpec)

	expectedPod := api.Pod{
		JSONBase: api.JSONBase{
			Kind: "Pod",
		},
		Labels:       controllerSpec.DesiredState.PodTemplate.Labels,
		DesiredState: controllerSpec.DesiredState.PodTemplate.DesiredState,
	}
	fakeHandler.ValidateRequest(t, makeURL("/pods"), "POST", nil)
	actualPod := api.Pod{}
	if err := json.Unmarshal([]byte(fakeHandler.RequestBody), &actualPod); err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
	if !reflect.DeepEqual(expectedPod, actualPod) {
		t.Logf("Body: %s", fakeHandler.RequestBody)
		t.Errorf("Unexpected mismatch.  Expected %#v, Got: %#v", expectedPod, actualPod)
	}
}

func TestHandleWatchResponseNotSet(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl
	_, err := manager.handleWatchResponse(&etcd.Response{
		Action: "update",
	})
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
}

func TestHandleWatchResponseNoNode(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl
	_, err := manager.handleWatchResponse(&etcd.Response{
		Action: "set",
	})
	if err == nil {
		t.Error("Unexpected non-error")
	}
}

func TestHandleWatchResponseBadData(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl
	_, err := manager.handleWatchResponse(&etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: "foobar",
		},
	})
	if err == nil {
		t.Error("Unexpected non-error")
	}
}

func TestHandleWatchResponse(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl

	controller := makeReplicationController(2)

	data, err := json.Marshal(controller)
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
	controllerOut, err := manager.handleWatchResponse(&etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: string(data),
		},
	})
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
	if !reflect.DeepEqual(controller, *controllerOut) {
		t.Errorf("Unexpected mismatch.  Expected %#v, Saw: %#v", controller, controllerOut)
	}
}

func TestHandleWatchResponseDelete(t *testing.T) {
	body, _ := json.Marshal(makePodList(2))
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)

	fakePodControl := FakePodControl{}

	manager := MakeReplicationManager(nil, client)
	manager.podControl = &fakePodControl

	controller := makeReplicationController(2)

	data, err := json.Marshal(controller)
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
	controllerOut, err := manager.handleWatchResponse(&etcd.Response{
		Action: "delete",
		PrevNode: &etcd.Node{
			Value: string(data),
		},
	})
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
	if !reflect.DeepEqual(controller, *controllerOut) {
		t.Errorf("Unexpected mismatch.  Expected %#v, Saw: %#v", controller, controllerOut)
	}
}

func TestSyncronize(t *testing.T) {
	controllerSpec1 := api.ReplicationController{
		DesiredState: api.ReplicationControllerState{
			Replicas: 4,
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						Containers: []api.Container{
							{
								Image: "foo/bar",
							},
						},
					},
				},
				Labels: map[string]string{
					"name": "foo",
					"type": "production",
				},
			},
		},
	}
	controllerSpec2 := api.ReplicationController{
		DesiredState: api.ReplicationControllerState{
			Replicas: 3,
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						Containers: []api.Container{
							{
								Image: "bar/baz",
							},
						},
					},
				},
				Labels: map[string]string{
					"name": "bar",
					"type": "production",
				},
			},
		},
	}

	fakeEtcd := tools.MakeFakeEtcdClient(t)
	fakeEtcd.Data["/registry/controllers"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: util.MakeJSONString(controllerSpec1),
					},
					{
						Value: util.MakeJSONString(controllerSpec2),
					},
				},
			},
		},
	}

	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: "{}",
		T:            t,
	}
	testServer := httptest.NewTLSServer(&fakeHandler)
	client := client.New(testServer.URL, nil)
	manager := MakeReplicationManager(fakeEtcd, client)
	fakePodControl := FakePodControl{}
	manager.podControl = &fakePodControl

	manager.synchronize()

	validateSyncReplication(t, &fakePodControl, 7, 0)
}

type asyncTimeout struct {
	doneChan chan bool
}

func beginTimeout(d time.Duration) *asyncTimeout {
	a := &asyncTimeout{doneChan: make(chan bool)}
	go func() {
		select {
		case <-a.doneChan:
			return
		case <-time.After(d):
			panic("Timeout expired!")
		}
	}()
	return a
}

func (a *asyncTimeout) done() {
	close(a.doneChan)
}

func TestWatchControllers(t *testing.T) {
	defer beginTimeout(20 * time.Second).done()
	fakeEtcd := tools.MakeFakeEtcdClient(t)
	manager := MakeReplicationManager(fakeEtcd, nil)
	var testControllerSpec api.ReplicationController
	received := make(chan bool)
	manager.syncHandler = func(controllerSpec api.ReplicationController) error {
		if !reflect.DeepEqual(controllerSpec, testControllerSpec) {
			t.Errorf("Expected %#v, but got %#v", testControllerSpec, controllerSpec)
		}
		close(received)
		return nil
	}

	go manager.watchControllers()

	fakeEtcd.WaitForWatchCompletion()

	// Test normal case
	testControllerSpec.ID = "foo"
	fakeEtcd.WatchResponse <- &etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: util.MakeJSONString(testControllerSpec),
		},
	}

	select {
	case <-received:
	case <-time.After(10 * time.Millisecond):
		t.Errorf("Expected 1 call but got 0")
	}

	// Test error case
	fakeEtcd.WatchInjectError <- fmt.Errorf("Injected error")

	// Did everything shut down?
	if _, open := <-fakeEtcd.WatchResponse; open {
		t.Errorf("An injected error did not cause a graceful shutdown")
	}

	// Test purposeful shutdown
	go manager.watchControllers()
	fakeEtcd.WaitForWatchCompletion()
	fakeEtcd.WatchStop <- true

	// Did everything shut down?
	if _, open := <-fakeEtcd.WatchResponse; open {
		t.Errorf("A stop did not cause a graceful shutdown")
	}
}
