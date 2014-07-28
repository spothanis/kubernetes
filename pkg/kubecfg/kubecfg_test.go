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

package kubecfg

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

type Action struct {
	action string
	value  interface{}
}

type FakeKubeClient struct {
	actions []Action
	pods    api.PodList
	ctrl    api.ReplicationController
}

func (client *FakeKubeClient) ListPods(selector labels.Selector) (api.PodList, error) {
	client.actions = append(client.actions, Action{action: "list-pods"})
	return client.pods, nil
}

func (client *FakeKubeClient) GetPod(name string) (api.Pod, error) {
	client.actions = append(client.actions, Action{action: "get-pod", value: name})
	return api.Pod{}, nil
}

func (client *FakeKubeClient) DeletePod(name string) error {
	client.actions = append(client.actions, Action{action: "delete-pod", value: name})
	return nil
}

func (client *FakeKubeClient) CreatePod(pod api.Pod) (api.Pod, error) {
	client.actions = append(client.actions, Action{action: "create-pod"})
	return api.Pod{}, nil
}

func (client *FakeKubeClient) UpdatePod(pod api.Pod) (api.Pod, error) {
	client.actions = append(client.actions, Action{action: "update-pod", value: pod.ID})
	return api.Pod{}, nil
}

func (client *FakeKubeClient) GetReplicationController(name string) (api.ReplicationController, error) {
	client.actions = append(client.actions, Action{action: "get-controller", value: name})
	return client.ctrl, nil
}

func (client *FakeKubeClient) CreateReplicationController(controller api.ReplicationController) (api.ReplicationController, error) {
	client.actions = append(client.actions, Action{action: "create-controller", value: controller})
	return api.ReplicationController{}, nil
}

func (client *FakeKubeClient) UpdateReplicationController(controller api.ReplicationController) (api.ReplicationController, error) {
	client.actions = append(client.actions, Action{action: "update-controller", value: controller})
	return api.ReplicationController{}, nil
}

func (client *FakeKubeClient) DeleteReplicationController(controller string) error {
	client.actions = append(client.actions, Action{action: "delete-controller", value: controller})
	return nil
}

func (client *FakeKubeClient) GetService(name string) (api.Service, error) {
	client.actions = append(client.actions, Action{action: "get-controller", value: name})
	return api.Service{}, nil
}

func (client *FakeKubeClient) CreateService(controller api.Service) (api.Service, error) {
	client.actions = append(client.actions, Action{action: "create-service", value: controller})
	return api.Service{}, nil
}

func (client *FakeKubeClient) UpdateService(controller api.Service) (api.Service, error) {
	client.actions = append(client.actions, Action{action: "update-service", value: controller})
	return api.Service{}, nil
}

func (client *FakeKubeClient) DeleteService(controller string) error {
	client.actions = append(client.actions, Action{action: "delete-service", value: controller})
	return nil
}

func validateAction(expectedAction, actualAction Action, t *testing.T) {
	if expectedAction != actualAction {
		t.Errorf("Unexpected action: %#v, expected: %#v", actualAction, expectedAction)
	}
}

func TestUpdateWithPods(t *testing.T) {
	client := FakeKubeClient{
		pods: api.PodList{
			Items: []api.Pod{
				{JSONBase: api.JSONBase{ID: "pod-1"}},
				{JSONBase: api.JSONBase{ID: "pod-2"}},
			},
		},
	}
	Update("foo", &client, 0)
	if len(client.actions) != 4 {
		t.Errorf("Unexpected action list %#v", client.actions)
	}
	validateAction(Action{action: "get-controller", value: "foo"}, client.actions[0], t)
	validateAction(Action{action: "list-pods"}, client.actions[1], t)
	// Update deletes the pods, it relies on the replication controller to replace them.
	validateAction(Action{action: "delete-pod", value: "pod-1"}, client.actions[2], t)
	validateAction(Action{action: "delete-pod", value: "pod-2"}, client.actions[3], t)
}

func TestUpdateNoPods(t *testing.T) {
	client := FakeKubeClient{}
	Update("foo", &client, 0)
	if len(client.actions) != 2 {
		t.Errorf("Unexpected action list %#v", client.actions)
	}
	validateAction(Action{action: "get-controller", value: "foo"}, client.actions[0], t)
	validateAction(Action{action: "list-pods"}, client.actions[1], t)
}

func TestRunController(t *testing.T) {
	fakeClient := FakeKubeClient{}
	name := "name"
	image := "foo/bar"
	replicas := 3
	RunController(image, name, replicas, &fakeClient, "8080:80", -1)
	if len(fakeClient.actions) != 1 || fakeClient.actions[0].action != "create-controller" {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	controller := fakeClient.actions[0].value.(api.ReplicationController)
	if controller.ID != name ||
		controller.DesiredState.Replicas != replicas ||
		controller.DesiredState.PodTemplate.DesiredState.Manifest.Containers[0].Image != image {
		t.Errorf("Unexpected controller: %#v", controller)
	}
}

func TestRunControllerWithService(t *testing.T) {
	fakeClient := FakeKubeClient{}
	name := "name"
	image := "foo/bar"
	replicas := 3
	RunController(image, name, replicas, &fakeClient, "", 8000)
	if len(fakeClient.actions) != 2 ||
		fakeClient.actions[0].action != "create-controller" ||
		fakeClient.actions[1].action != "create-service" {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	controller := fakeClient.actions[0].value.(api.ReplicationController)
	if controller.ID != name ||
		controller.DesiredState.Replicas != replicas ||
		controller.DesiredState.PodTemplate.DesiredState.Manifest.Containers[0].Image != image {
		t.Errorf("Unexpected controller: %#v", controller)
	}
}

func TestStopController(t *testing.T) {
	fakeClient := FakeKubeClient{}
	name := "name"
	StopController(name, &fakeClient)
	if len(fakeClient.actions) != 2 {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	if fakeClient.actions[0].action != "get-controller" ||
		fakeClient.actions[0].value.(string) != name {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[0])
	}
	controller := fakeClient.actions[1].value.(api.ReplicationController)
	if fakeClient.actions[1].action != "update-controller" ||
		controller.DesiredState.Replicas != 0 {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[1])
	}
}

func TestResizeController(t *testing.T) {
	fakeClient := FakeKubeClient{}
	name := "name"
	replicas := 17
	ResizeController(name, replicas, &fakeClient)
	if len(fakeClient.actions) != 2 {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	if fakeClient.actions[0].action != "get-controller" ||
		fakeClient.actions[0].value.(string) != name {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[0])
	}
	controller := fakeClient.actions[1].value.(api.ReplicationController)
	if fakeClient.actions[1].action != "update-controller" ||
		controller.DesiredState.Replicas != 17 {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[1])
	}
}

func TestCloudCfgDeleteController(t *testing.T) {
	fakeClient := FakeKubeClient{}
	name := "name"
	err := DeleteController(name, &fakeClient)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(fakeClient.actions) != 2 {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	if fakeClient.actions[0].action != "get-controller" ||
		fakeClient.actions[0].value.(string) != name {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[0])
	}
	if fakeClient.actions[1].action != "delete-controller" ||
		fakeClient.actions[1].value.(string) != name {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[1])
	}
}

func TestCloudCfgDeleteControllerWithReplicas(t *testing.T) {
	fakeClient := FakeKubeClient{
		ctrl: api.ReplicationController{
			DesiredState: api.ReplicationControllerState{
				Replicas: 2,
			},
		},
	}
	name := "name"
	err := DeleteController(name, &fakeClient)
	if len(fakeClient.actions) != 1 {
		t.Errorf("Unexpected actions: %#v", fakeClient.actions)
	}
	if fakeClient.actions[0].action != "get-controller" ||
		fakeClient.actions[0].value.(string) != name {
		t.Errorf("Unexpected action: %#v", fakeClient.actions[0])
	}
	if err == nil {
		t.Errorf("Unexpected non-error.")
	}
}

func TestLoadAuthInfo(t *testing.T) {
	loadAuthInfoTests := []struct {
		authData string
		authInfo *client.AuthInfo
		r        io.Reader
	}{
		{
			`{"user": "user", "password": "pass"}`,
			&client.AuthInfo{User: "user", Password: "pass"},
			nil,
		},
		{
			"", nil, nil,
		},
		{
			"missing",
			&client.AuthInfo{User: "user", Password: "pass"},
			bytes.NewBufferString("user\npass"),
		},
	}
	for _, loadAuthInfoTest := range loadAuthInfoTests {
		tt := loadAuthInfoTest
		aifile, err := ioutil.TempFile("", "testAuthInfo")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if tt.authData != "missing" {
			defer os.Remove(aifile.Name())
			defer aifile.Close()
			_, err = aifile.WriteString(tt.authData)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		} else {
			aifile.Close()
			os.Remove(aifile.Name())
		}
		authInfo, err := LoadAuthInfo(aifile.Name(), tt.r)
		if len(tt.authData) == 0 && tt.authData != "missing" {
			if err == nil {
				t.Error("LoadAuthInfo didn't fail on empty file")
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !reflect.DeepEqual(authInfo, tt.authInfo) {
			t.Errorf("Expected %v, got %v", tt.authInfo, authInfo)
		}
	}
}

func TestMakePorts(t *testing.T) {
	var makePortsTests = []struct {
		spec  string
		ports []api.Port
	}{
		{
			"8080:80,8081:8081,443:444",
			[]api.Port{
				api.Port{HostPort: 8080, ContainerPort: 80},
				api.Port{HostPort: 8081, ContainerPort: 8081},
				api.Port{HostPort: 443, ContainerPort: 444},
			},
		},
	}
	for _, tt := range makePortsTests {
		ports := makePorts(tt.spec)
		if !reflect.DeepEqual(ports, tt.ports) {
			t.Errorf("Expected %#v, got %#v", tt.ports, ports)
		}
	}
}
