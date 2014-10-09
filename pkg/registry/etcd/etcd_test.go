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

package etcd

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/latest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/pod"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/registrytest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/tools"

	"github.com/coreos/go-etcd/etcd"
)

func NewTestEtcdRegistry(client tools.EtcdClient) *Registry {
	registry := NewRegistry(tools.EtcdHelper{client, latest.Codec, tools.RuntimeVersionAdapter{latest.ResourceVersioner}},
		&pod.BasicManifestFactory{
			ServiceRegistry: &registrytest.ServiceRegistry{},
		})
	return registry
}

func TestEtcdParseWatchResourceVersion(t *testing.T) {
	testCases := []struct {
		Version       string
		Kind          string
		ExpectVersion uint64
		Err           bool
	}{
		{Version: "", ExpectVersion: 0},
		{Version: "a", Err: true},
		{Version: " ", Err: true},
		{Version: "1", ExpectVersion: 2},
		{Version: "10", ExpectVersion: 11},
	}
	for _, testCase := range testCases {
		version, err := parseWatchResourceVersion(testCase.Version, testCase.Kind)
		switch {
		case testCase.Err:
			if err == nil {
				t.Errorf("%s: unexpected non-error", testCase.Version)
				continue
			}
			if !errors.IsInvalid(err) {
				t.Errorf("%s: unexpected error: %v", testCase.Version, err)
				continue
			}
		case !testCase.Err && err != nil:
			t.Errorf("%s: unexpected error: %v", testCase.Version, err)
			continue
		}
		if version != testCase.ExpectVersion {
			t.Errorf("%s: expected version %d but was %d", testCase.Version, testCase.ExpectVersion, version)
		}
	}
}

func TestEtcdGetPod(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/pods/foo", runtime.EncodeOrDie(latest.Codec, &api.Pod{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	pod, err := registry.GetPod(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if pod.ID != "foo" {
		t.Errorf("Unexpected pod: %#v", pod)
	}
}

func TestEtcdGetPodNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	_, err := registry.GetPod(ctx, "foo")
	if !errors.IsNotFound(err) {
		t.Errorf("Unexpected error returned: %#v", err)
	}
}

func TestEtcdCreatePod(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	fakeClient.Set("/registry/hosts/machine/kubelet", runtime.EncodeOrDie(latest.Codec, &api.ContainerManifestList{}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreatePod(ctx, &api.Pod{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
		DesiredState: api.PodState{
			Manifest: api.ContainerManifest{
				Containers: []api.Container{
					{
						Name: "foo",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Suddenly, a wild scheduler appears:
	err = registry.ApplyBinding(ctx, &api.Binding{PodID: "foo", Host: "machine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/pods/foo", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var pod api.Pod
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &pod)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if pod.ID != "foo" {
		t.Errorf("Unexpected pod: %#v %s", pod, resp.Node.Value)
	}
	var manifests api.ContainerManifestList
	resp, err = fakeClient.Get("/registry/hosts/machine/kubelet", false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &manifests)
	if len(manifests.Items) != 1 || manifests.Items[0].ID != "foo" {
		t.Errorf("Unexpected manifest list: %#v", manifests)
	}
}

func TestEtcdCreatePodAlreadyExisting(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value: runtime.EncodeOrDie(latest.Codec, &api.Pod{TypeMeta: api.TypeMeta{ID: "foo"}}),
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreatePod(ctx, &api.Pod{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
	})
	if !errors.IsAlreadyExists(err) {
		t.Errorf("Unexpected error returned: %#v", err)
	}
}

func TestEtcdCreatePodWithContainersError(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	fakeClient.Data["/registry/hosts/machine/kubelet"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNodeExist, // validate that ApplyBinding is translating Create errors
	}
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreatePod(ctx, &api.Pod{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Suddenly, a wild scheduler appears:
	err = registry.ApplyBinding(ctx, &api.Binding{PodID: "foo", Host: "machine"})
	if !errors.IsAlreadyExists(err) {
		t.Fatalf("Unexpected error returned: %#v", err)
	}

	existingPod, err := registry.GetPod(ctx, "foo")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if existingPod.DesiredState.Host == "machine" {
		t.Fatal("Pod's host changed in response to an non-apply-able binding.")
	}
}

func TestEtcdCreatePodWithContainersNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	fakeClient.Data["/registry/hosts/machine/kubelet"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreatePod(ctx, &api.Pod{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
		DesiredState: api.PodState{
			Manifest: api.ContainerManifest{
				ID: "foo",
				Containers: []api.Container{
					{
						Name: "foo",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Suddenly, a wild scheduler appears:
	err = registry.ApplyBinding(ctx, &api.Binding{PodID: "foo", Host: "machine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/pods/foo", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var pod api.Pod
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &pod)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if pod.ID != "foo" {
		t.Errorf("Unexpected pod: %#v %s", pod, resp.Node.Value)
	}
	var manifests api.ContainerManifestList
	resp, err = fakeClient.Get("/registry/hosts/machine/kubelet", false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &manifests)
	if len(manifests.Items) != 1 || manifests.Items[0].ID != "foo" {
		t.Errorf("Unexpected manifest list: %#v", manifests)
	}
}

func TestEtcdCreatePodWithExistingContainers(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/registry/pods/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	fakeClient.Set("/registry/hosts/machine/kubelet", runtime.EncodeOrDie(latest.Codec, &api.ContainerManifestList{
		Items: []api.ContainerManifest{
			{ID: "bar"},
		},
	}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreatePod(ctx, &api.Pod{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
		DesiredState: api.PodState{
			Manifest: api.ContainerManifest{
				ID: "foo",
				Containers: []api.Container{
					{
						Name: "foo",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Suddenly, a wild scheduler appears:
	err = registry.ApplyBinding(ctx, &api.Binding{PodID: "foo", Host: "machine"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/pods/foo", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var pod api.Pod
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &pod)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if pod.ID != "foo" {
		t.Errorf("Unexpected pod: %#v %s", pod, resp.Node.Value)
	}
	var manifests api.ContainerManifestList
	resp, err = fakeClient.Get("/registry/hosts/machine/kubelet", false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &manifests)
	if len(manifests.Items) != 2 || manifests.Items[1].ID != "foo" {
		t.Errorf("Unexpected manifest list: %#v", manifests)
	}
}

func TestEtcdDeletePod(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true

	key := "/registry/pods/foo"
	fakeClient.Set(key, runtime.EncodeOrDie(latest.Codec, &api.Pod{
		TypeMeta:     api.TypeMeta{ID: "foo"},
		DesiredState: api.PodState{Host: "machine"},
	}), 0)
	fakeClient.Set("/registry/hosts/machine/kubelet", runtime.EncodeOrDie(latest.Codec, &api.ContainerManifestList{
		Items: []api.ContainerManifest{
			{ID: "foo"},
		},
	}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.DeletePod(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(fakeClient.DeletedKeys) != 1 {
		t.Errorf("Expected 1 delete, found %#v", fakeClient.DeletedKeys)
	} else if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
	response, err := fakeClient.Get("/registry/hosts/machine/kubelet", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var manifests api.ContainerManifestList
	latest.Codec.DecodeInto([]byte(response.Node.Value), &manifests)
	if len(manifests.Items) != 0 {
		t.Errorf("Unexpected container set: %s, expected empty", response.Node.Value)
	}
}

func TestEtcdDeletePodMultipleContainers(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true

	key := "/registry/pods/foo"
	fakeClient.Set(key, runtime.EncodeOrDie(latest.Codec, &api.Pod{
		TypeMeta:     api.TypeMeta{ID: "foo"},
		DesiredState: api.PodState{Host: "machine"},
	}), 0)
	fakeClient.Set("/registry/hosts/machine/kubelet", runtime.EncodeOrDie(latest.Codec, &api.ContainerManifestList{
		Items: []api.ContainerManifest{
			{ID: "foo"},
			{ID: "bar"},
		},
	}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.DeletePod(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(fakeClient.DeletedKeys) != 1 {
		t.Errorf("Expected 1 delete, found %#v", fakeClient.DeletedKeys)
	}
	if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
	response, err := fakeClient.Get("/registry/hosts/machine/kubelet", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var manifests api.ContainerManifestList
	latest.Codec.DecodeInto([]byte(response.Node.Value), &manifests)
	if len(manifests.Items) != 1 {
		t.Fatalf("Unexpected manifest set: %#v, expected empty", manifests)
	}
	if manifests.Items[0].ID != "bar" {
		t.Errorf("Deleted wrong manifest: %#v", manifests)
	}
}

func TestEtcdEmptyListPods(t *testing.T) {
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/pods"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	ctx := api.NewContext()
	pods, err := registry.ListPods(ctx, labels.Everything())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(pods.Items) != 0 {
		t.Errorf("Unexpected pod list: %#v", pods)
	}
}

func TestEtcdListPodsNotFound(t *testing.T) {
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/pods"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	ctx := api.NewContext()
	pods, err := registry.ListPods(ctx, labels.Everything())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(pods.Items) != 0 {
		t.Errorf("Unexpected pod list: %#v", pods)
	}
}

func TestEtcdListPods(t *testing.T) {
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/pods"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Pod{
							TypeMeta:     api.TypeMeta{ID: "foo"},
							DesiredState: api.PodState{Host: "machine"},
						}),
					},
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Pod{
							TypeMeta:     api.TypeMeta{ID: "bar"},
							DesiredState: api.PodState{Host: "machine"},
						}),
					},
				},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	ctx := api.NewContext()
	pods, err := registry.ListPods(ctx, labels.Everything())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(pods.Items) != 2 || pods.Items[0].ID != "foo" || pods.Items[1].ID != "bar" {
		t.Errorf("Unexpected pod list: %#v", pods)
	}
	if pods.Items[0].CurrentState.Host != "machine" ||
		pods.Items[1].CurrentState.Host != "machine" {
		t.Errorf("Failed to populate host name.")
	}
}

func TestEtcdListControllersNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/controllers"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	controllers, err := registry.ListControllers(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(controllers.Items) != 0 {
		t.Errorf("Unexpected controller list: %#v", controllers)
	}
}

func TestEtcdListServicesNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/services/specs"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	services, err := registry.ListServices(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(services.Items) != 0 {
		t.Errorf("Unexpected controller list: %#v", services)
	}
}

func TestEtcdListControllers(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/controllers"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.ReplicationController{TypeMeta: api.TypeMeta{ID: "foo"}}),
					},
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.ReplicationController{TypeMeta: api.TypeMeta{ID: "bar"}}),
					},
				},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	controllers, err := registry.ListControllers(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(controllers.Items) != 2 || controllers.Items[0].ID != "foo" || controllers.Items[1].ID != "bar" {
		t.Errorf("Unexpected controller list: %#v", controllers)
	}
}

func TestEtcdGetController(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/controllers/foo", runtime.EncodeOrDie(latest.Codec, &api.ReplicationController{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	ctrl, err := registry.GetController(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if ctrl.ID != "foo" {
		t.Errorf("Unexpected controller: %#v", ctrl)
	}
}

func TestEtcdGetControllerNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Data["/registry/controllers/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	ctrl, err := registry.GetController(ctx, "foo")
	if ctrl != nil {
		t.Errorf("Unexpected non-nil controller: %#v", ctrl)
	}
	if !errors.IsNotFound(err) {
		t.Errorf("Unexpected error returned: %#v", err)
	}
}

func TestEtcdDeleteController(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.DeleteController(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(fakeClient.DeletedKeys) != 1 {
		t.Errorf("Expected 1 delete, found %#v", fakeClient.DeletedKeys)
	}
	key := "/registry/controllers/foo"
	if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
}

func TestEtcdCreateController(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreateController(ctx, &api.ReplicationController{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/controllers/foo", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var ctrl api.ReplicationController
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &ctrl)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if ctrl.ID != "foo" {
		t.Errorf("Unexpected pod: %#v %s", ctrl, resp.Node.Value)
	}
}

func TestEtcdCreateControllerAlreadyExisting(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/controllers/foo", runtime.EncodeOrDie(latest.Codec, &api.ReplicationController{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)

	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreateController(ctx, &api.ReplicationController{
		TypeMeta: api.TypeMeta{
			ID: "foo",
		},
	})
	if !errors.IsAlreadyExists(err) {
		t.Errorf("expected already exists err, got %#v", err)
	}
}

func TestEtcdUpdateController(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true

	resp, _ := fakeClient.Set("/registry/controllers/foo", runtime.EncodeOrDie(latest.Codec, &api.ReplicationController{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.UpdateController(ctx, &api.ReplicationController{
		TypeMeta: api.TypeMeta{ID: "foo", ResourceVersion: strconv.FormatUint(resp.Node.ModifiedIndex, 10)},
		DesiredState: api.ReplicationControllerState{
			Replicas: 2,
		},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	ctrl, err := registry.GetController(ctx, "foo")
	if ctrl.DesiredState.Replicas != 2 {
		t.Errorf("Unexpected controller: %#v", ctrl)
	}
}

func TestEtcdListServices(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/services/specs"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Service{TypeMeta: api.TypeMeta{ID: "foo"}}),
					},
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Service{TypeMeta: api.TypeMeta{ID: "bar"}}),
					},
				},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	services, err := registry.ListServices(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(services.Items) != 2 || services.Items[0].ID != "foo" || services.Items[1].ID != "bar" {
		t.Errorf("Unexpected service list: %#v", services)
	}
}

func TestEtcdCreateService(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreateService(ctx, &api.Service{
		TypeMeta: api.TypeMeta{ID: "foo"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/services/specs/foo", false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	var service api.Service
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &service)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if service.ID != "foo" {
		t.Errorf("Unexpected service: %#v %s", service, resp.Node.Value)
	}
}

func TestEtcdCreateServiceAlreadyExisting(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/services/specs/foo", runtime.EncodeOrDie(latest.Codec, &api.Service{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreateService(ctx, &api.Service{
		TypeMeta: api.TypeMeta{ID: "foo"},
	})
	if !errors.IsAlreadyExists(err) {
		t.Errorf("expected already exists err, got %#v", err)
	}
}

func TestEtcdGetService(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/services/specs/foo", runtime.EncodeOrDie(latest.Codec, &api.Service{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	service, err := registry.GetService(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if service.ID != "foo" {
		t.Errorf("Unexpected service: %#v", service)
	}
}

func TestEtcdGetServiceNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Data["/registry/services/specs/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	_, err := registry.GetService(ctx, "foo")
	if !errors.IsNotFound(err) {
		t.Errorf("Unexpected error returned: %#v", err)
	}
}

func TestEtcdDeleteService(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.DeleteService(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(fakeClient.DeletedKeys) != 2 {
		t.Errorf("Expected 2 delete, found %#v", fakeClient.DeletedKeys)
	}
	key := "/registry/services/specs/foo"
	if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
	key = "/registry/services/endpoints/foo"
	if fakeClient.DeletedKeys[1] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[1], key)
	}
}

func TestEtcdUpdateService(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true

	resp, _ := fakeClient.Set("/registry/services/specs/foo", runtime.EncodeOrDie(latest.Codec, &api.Service{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	testService := api.Service{
		TypeMeta: api.TypeMeta{ID: "foo", ResourceVersion: strconv.FormatUint(resp.Node.ModifiedIndex, 10)},
		Labels: map[string]string{
			"baz": "bar",
		},
		Selector: map[string]string{
			"baz": "bar",
		},
	}
	err := registry.UpdateService(ctx, &testService)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	svc, err := registry.GetService(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Clear modified indices before the equality test.
	svc.ResourceVersion = ""
	testService.ResourceVersion = ""
	if !reflect.DeepEqual(*svc, testService) {
		t.Errorf("Unexpected service: got\n %#v\n, wanted\n %#v", svc, testService)
	}
}

func TestEtcdListEndpoints(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/services/endpoints"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Endpoints{TypeMeta: api.TypeMeta{ID: "foo"}, Endpoints: []string{"127.0.0.1:8345"}}),
					},
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Endpoints{TypeMeta: api.TypeMeta{ID: "bar"}}),
					},
				},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	services, err := registry.ListEndpoints(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(services.Items) != 2 || services.Items[0].ID != "foo" || services.Items[1].ID != "bar" {
		t.Errorf("Unexpected endpoints list: %#v", services)
	}
}

func TestEtcdGetEndpoints(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	endpoints := &api.Endpoints{
		TypeMeta:  api.TypeMeta{ID: "foo"},
		Endpoints: []string{"127.0.0.1:34855"},
	}

	fakeClient.Set("/registry/services/endpoints/foo", runtime.EncodeOrDie(latest.Codec, endpoints), 0)

	got, err := registry.GetEndpoints(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if e, a := endpoints, got; !reflect.DeepEqual(e, a) {
		t.Errorf("Unexpected endpoints: %#v, expected %#v", e, a)
	}
}

func TestEtcdUpdateEndpoints(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	registry := NewTestEtcdRegistry(fakeClient)
	endpoints := api.Endpoints{
		TypeMeta:  api.TypeMeta{ID: "foo"},
		Endpoints: []string{"baz", "bar"},
	}

	fakeClient.Set("/registry/services/endpoints/foo", runtime.EncodeOrDie(latest.Codec, &api.Endpoints{}), 0)

	err := registry.UpdateEndpoints(ctx, &endpoints)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	response, err := fakeClient.Get("/registry/services/endpoints/foo", false, false)
	if err != nil {
		t.Fatalf("Unexpected error %v", err)
	}
	var endpointsOut api.Endpoints
	err = latest.Codec.DecodeInto([]byte(response.Node.Value), &endpointsOut)
	if !reflect.DeepEqual(endpoints, endpointsOut) {
		t.Errorf("Unexpected endpoints: %#v, expected %#v", endpointsOut, endpoints)
	}
}

func TestEtcdWatchServices(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	watching, err := registry.WatchServices(ctx,
		labels.Everything(),
		labels.SelectorFromSet(labels.Set{"ID": "foo"}),
		"1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeClient.WaitForWatchCompletion()

	select {
	case _, ok := <-watching.ResultChan():
		if !ok {
			t.Errorf("watching channel should be open")
		}
	default:
	}
	fakeClient.WatchInjectError <- nil
	if _, ok := <-watching.ResultChan(); ok {
		t.Errorf("watching channel should be closed")
	}
	watching.Stop()
}

func TestEtcdWatchServicesBadSelector(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	_, err := registry.WatchServices(
		ctx,
		labels.Everything(),
		labels.SelectorFromSet(labels.Set{"Field.Selector": "foo"}),
		"",
	)
	if err == nil {
		t.Errorf("unexpected non-error: %v", err)
	}

	_, err = registry.WatchServices(
		ctx,
		labels.SelectorFromSet(labels.Set{"Label.Selector": "foo"}),
		labels.Everything(),
		"",
	)
	if err == nil {
		t.Errorf("unexpected non-error: %v", err)
	}
}

func TestEtcdWatchEndpoints(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	watching, err := registry.WatchEndpoints(
		ctx,
		labels.Everything(),
		labels.SelectorFromSet(labels.Set{"ID": "foo"}),
		"1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fakeClient.WaitForWatchCompletion()

	select {
	case _, ok := <-watching.ResultChan():
		if !ok {
			t.Errorf("watching channel should be open")
		}
	default:
	}
	fakeClient.WatchInjectError <- nil
	if _, ok := <-watching.ResultChan(); ok {
		t.Errorf("watching channel should be closed")
	}
	watching.Stop()
}

func TestEtcdWatchEndpointsBadSelector(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	_, err := registry.WatchEndpoints(
		ctx,
		labels.Everything(),
		labels.SelectorFromSet(labels.Set{"Field.Selector": "foo"}),
		"",
	)
	if err == nil {
		t.Errorf("unexpected non-error: %v", err)
	}

	_, err = registry.WatchEndpoints(
		ctx,
		labels.SelectorFromSet(labels.Set{"Label.Selector": "foo"}),
		labels.Everything(),
		"",
	)
	if err == nil {
		t.Errorf("unexpected non-error: %v", err)
	}
}

func TestEtcdListMinions(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	key := "/registry/minions"
	fakeClient.Data[key] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Minion{
							TypeMeta: api.TypeMeta{ID: "foo"},
						}),
					},
					{
						Value: runtime.EncodeOrDie(latest.Codec, &api.Minion{
							TypeMeta: api.TypeMeta{ID: "bar"},
						}),
					},
				},
			},
		},
		E: nil,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	minions, err := registry.ListMinions(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(minions.Items) != 2 || minions.Items[0].ID != "foo" || minions.Items[1].ID != "bar" {
		t.Errorf("Unexpected minion list: %#v", minions)
	}
}

func TestEtcdCreateMinion(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.CreateMinion(ctx, &api.Minion{
		TypeMeta: api.TypeMeta{ID: "foo"},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	resp, err := fakeClient.Get("/registry/minions/foo", false, false)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	var minion api.Minion
	err = latest.Codec.DecodeInto([]byte(resp.Node.Value), &minion)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if minion.ID != "foo" {
		t.Errorf("Unexpected minion: %#v %s", minion, resp.Node.Value)
	}
}

func TestEtcdGetMinion(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Set("/registry/minions/foo", runtime.EncodeOrDie(latest.Codec, &api.Minion{TypeMeta: api.TypeMeta{ID: "foo"}}), 0)
	registry := NewTestEtcdRegistry(fakeClient)
	minion, err := registry.GetMinion(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if minion.ID != "foo" {
		t.Errorf("Unexpected minion: %#v", minion)
	}
}

func TestEtcdGetMinionNotFound(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	fakeClient.Data["/registry/minions/foo"] = tools.EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: tools.EtcdErrorNotFound,
	}
	registry := NewTestEtcdRegistry(fakeClient)
	_, err := registry.GetMinion(ctx, "foo")

	if !errors.IsNotFound(err) {
		t.Errorf("Unexpected error returned: %#v", err)
	}
}

func TestEtcdDeleteMinion(t *testing.T) {
	ctx := api.NewContext()
	fakeClient := tools.NewFakeEtcdClient(t)
	registry := NewTestEtcdRegistry(fakeClient)
	err := registry.DeleteMinion(ctx, "foo")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(fakeClient.DeletedKeys) != 1 {
		t.Errorf("Expected 1 delete, found %#v", fakeClient.DeletedKeys)
	}
	key := "/registry/minions/foo"
	if fakeClient.DeletedKeys[0] != key {
		t.Errorf("Unexpected key: %s, expected %s", fakeClient.DeletedKeys[0], key)
	}
}

// TODO We need a test for the compare and swap behavior.  This basically requires two things:
//   1) Add a per-operation synchronization channel to the fake etcd client, such that any operation waits on that
//      channel, this will enable us to orchestrate the flow of etcd requests in the test.
//   2) We need to make the map from key to (response, error) actually be a [](response, error) and pop
//      our way through the responses.  That will enable us to hand back multiple different responses for
//      the same key.
//   Once that infrastructure is in place, the test looks something like:
//      Routine #1                               Routine #2
//         Read
//         Wait for sync on update               Read
//                                               Update
//         Update
//   In the buggy case, this will result in lost data.  In the correct case, the second update should fail
//   and be retried.
