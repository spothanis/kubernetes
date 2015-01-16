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

package master

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/registrytest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
)

type podInfoCall struct {
	host      string
	namespace string
	name      string
}

type podInfoResponse struct {
	useCount int
	data     api.PodStatusResult
	err      error
}

type podInfoCalls map[podInfoCall]*podInfoResponse

type FakePodInfoGetter struct {
	calls podInfoCalls
	lock  sync.Mutex

	// default data/error to return, or you can add
	// responses to specific calls-- that will take precedence.
	data api.PodStatusResult
	err  error
}

func (f *FakePodInfoGetter) GetPodStatus(host, namespace, name string) (api.PodStatusResult, error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	if f.calls == nil {
		f.calls = podInfoCalls{}
	}

	key := podInfoCall{host, namespace, name}
	call, ok := f.calls[key]
	if !ok {
		f.calls[key] = &podInfoResponse{
			0, f.data, f.err,
		}
		call = f.calls[key]
	}
	call.useCount++
	return call.data, call.err
}

func TestPodCacheGetDifferentNamespace(t *testing.T) {
	cache := NewPodCache(nil, nil, nil, nil)

	expectedDefault := api.PodStatus{
		Info: api.PodInfo{
			"foo": api.ContainerStatus{},
		},
	}
	expectedOther := api.PodStatus{
		Info: api.PodInfo{
			"bar": api.ContainerStatus{},
		},
	}

	cache.podStatus[objKey{api.NamespaceDefault, "foo"}] = expectedDefault
	cache.podStatus[objKey{"other", "foo"}] = expectedOther

	info, err := cache.GetPodStatus(api.NamespaceDefault, "foo")
	if err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
	if !reflect.DeepEqual(info, &expectedDefault) {
		t.Errorf("Unexpected mismatch. Expected: %+v, Got: %+v", &expectedOther, info)
	}

	info, err = cache.GetPodStatus("other", "foo")
	if err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
	if !reflect.DeepEqual(info, &expectedOther) {
		t.Errorf("Unexpected mismatch. Expected: %+v, Got: %+v", &expectedOther, info)
	}
}

func TestPodCacheGet(t *testing.T) {
	cache := NewPodCache(nil, nil, nil, nil)

	expected := api.PodStatus{
		Info: api.PodInfo{
			"foo": api.ContainerStatus{},
		},
	}
	cache.podStatus[objKey{api.NamespaceDefault, "foo"}] = expected

	info, err := cache.GetPodStatus(api.NamespaceDefault, "foo")
	if err != nil {
		t.Errorf("Unexpected error: %+v", err)
	}
	if !reflect.DeepEqual(info, &expected) {
		t.Errorf("Unexpected mismatch. Expected: %+v, Got: %+v", &expected, info)
	}
}

func TestPodCacheGetMissing(t *testing.T) {
	cache := NewPodCache(nil, nil, nil, nil)

	status, err := cache.GetPodStatus(api.NamespaceDefault, "foo")
	if err == nil {
		t.Errorf("Unexpected non-error: %+v", err)
	}
	if status != nil {
		t.Errorf("Unexpected status: %+v", status)
	}
}

type fakeIPCache func(string) string

func (f fakeIPCache) GetInstanceIP(host string) (ip string) {
	return f(host)
}

type podCacheTestConfig struct {
	ipFunc               func(string) string // Construct will set a default if nil
	nodes                []api.Node
	pods                 []api.Pod
	kubeletContainerInfo api.PodStatus

	// Construct will fill in these fields
	fakePodInfo *FakePodInfoGetter
	fakeNodes   *client.Fake
	fakePods    *registrytest.PodRegistry
}

func (c *podCacheTestConfig) Construct() *PodCache {
	if c.ipFunc == nil {
		c.ipFunc = func(host string) string {
			return "ip of " + host
		}
	}
	c.fakePodInfo = &FakePodInfoGetter{
		data: api.PodStatusResult{
			Status: c.kubeletContainerInfo,
		},
	}
	c.fakeNodes = &client.Fake{
		MinionsList: api.NodeList{
			Items: c.nodes,
		},
	}
	c.fakePods = registrytest.NewPodRegistry(&api.PodList{Items: c.pods})
	return NewPodCache(
		fakeIPCache(c.ipFunc),
		c.fakePodInfo,
		c.fakeNodes.Nodes(),
		c.fakePods,
	)
}

func makePod(namespace, name, host string, containers ...string) *api.Pod {
	pod := &api.Pod{
		ObjectMeta: api.ObjectMeta{Namespace: namespace, Name: name},
		Status:     api.PodStatus{Host: host},
	}
	for _, c := range containers {
		pod.Spec.Containers = append(pod.Spec.Containers, api.Container{
			Name: c,
		})
	}
	return pod
}

func makeNode(name string) *api.Node {
	return &api.Node{
		ObjectMeta: api.ObjectMeta{Name: name},
	}
}

func TestPodUpdateAllContainers(t *testing.T) {
	pod := makePod(api.NamespaceDefault, "foo", "machine", "bar")
	pod2 := makePod(api.NamespaceDefault, "baz", "machine", "qux")
	config := podCacheTestConfig{
		ipFunc: func(host string) string {
			if host == "machine" {
				return "1.2.3.5"
			}
			return ""
		},
		kubeletContainerInfo: api.PodStatus{
			Info: api.PodInfo{"bar": api.ContainerStatus{}}},
		nodes: []api.Node{*makeNode("machine")},
		pods:  []api.Pod{*pod, *pod2},
	}
	cache := config.Construct()

	cache.UpdateAllContainers()

	call1 := config.fakePodInfo.calls[podInfoCall{"machine", api.NamespaceDefault, "foo"}]
	call2 := config.fakePodInfo.calls[podInfoCall{"machine", api.NamespaceDefault, "baz"}]
	if call1 == nil || call1.useCount != 1 {
		t.Errorf("Expected 1 call for 'foo': %+v", config.fakePodInfo.calls)
	}
	if call2 == nil || call2.useCount != 1 {
		t.Errorf("Expected 1 call for 'baz': %+v", config.fakePodInfo.calls)
	}
	if len(config.fakePodInfo.calls) != 2 {
		t.Errorf("Expected 2 calls: %+v", config.fakePodInfo.calls)
	}

	status, err := cache.GetPodStatus(api.NamespaceDefault, "foo")
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}
	if e, a := config.kubeletContainerInfo.Info, status.Info; !reflect.DeepEqual(e, a) {
		t.Errorf("Unexpected mismatch. Expected: %+v, Got: %+v", e, a)
	}
	if e, a := "1.2.3.5", status.HostIP; e != a {
		t.Errorf("Unexpected mismatch. Expected: %+v, Got: %+v", e, a)
	}

	if e, a := 1, len(config.fakeNodes.Actions); e != a {
		t.Errorf("Expected: %v, Got: %v; %+v", e, a, config.fakeNodes.Actions)
	} else {
		if e, a := "get-minion", config.fakeNodes.Actions[0].Action; e != a {
			t.Errorf("Expected: %v, Got: %v; %+v", e, a, config.fakeNodes.Actions)
		}
	}
}

func TestFillPodStatusNoHost(t *testing.T) {
	pod := makePod(api.NamespaceDefault, "foo", "", "bar")
	config := podCacheTestConfig{
		kubeletContainerInfo: api.PodStatus{},
		nodes:                []api.Node{*makeNode("machine")},
		pods:                 []api.Pod{*pod},
	}
	cache := config.Construct()
	err := cache.updatePodStatus(&config.pods[0])
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	status, err := cache.GetPodStatus(pod.Namespace, pod.Name)
	if e, a := api.PodPending, status.Phase; e != a {
		t.Errorf("Expected: %+v, Got %+v", e, a)
	}
}

func TestFillPodStatusMissingMachine(t *testing.T) {
	pod := makePod(api.NamespaceDefault, "foo", "machine", "bar")
	config := podCacheTestConfig{
		kubeletContainerInfo: api.PodStatus{},
		nodes:                []api.Node{},
		pods:                 []api.Pod{*pod},
	}
	cache := config.Construct()
	err := cache.updatePodStatus(&config.pods[0])
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	status, err := cache.GetPodStatus(pod.Namespace, pod.Name)
	if e, a := api.PodFailed, status.Phase; e != a {
		t.Errorf("Expected: %+v, Got %+v", e, a)
	}
}

func TestFillPodStatus(t *testing.T) {
	pod := makePod(api.NamespaceDefault, "foo", "machine", "bar")
	expectedIP := "1.2.3.4"
	expectedTime, _ := time.Parse("2013-Feb-03", "2013-Feb-03")
	config := podCacheTestConfig{
		kubeletContainerInfo: api.PodStatus{
			Phase:  api.PodPending,
			Host:   "machine",
			HostIP: "ip of machine",
			PodIP:  expectedIP,
			Info: api.PodInfo{
				"net": {
					State: api.ContainerState{
						Running: &api.ContainerStateRunning{
							StartedAt: util.NewTime(expectedTime),
						},
					},
					RestartCount: 1,
					PodIP:        expectedIP,
				},
			},
		},
		nodes: []api.Node{*makeNode("machine")},
		pods:  []api.Pod{*pod},
	}
	cache := config.Construct()
	err := cache.updatePodStatus(&config.pods[0])
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	status, err := cache.GetPodStatus(pod.Namespace, pod.Name)
	if e, a := &config.kubeletContainerInfo, status; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected: %+v, Got %+v", e, a)
	}
}

func TestFillPodInfoNoData(t *testing.T) {
	pod := makePod(api.NamespaceDefault, "foo", "machine", "bar")
	expectedIP := ""
	config := podCacheTestConfig{
		kubeletContainerInfo: api.PodStatus{
			Phase:  api.PodPending,
			Host:   "machine",
			HostIP: "ip of machine",
			Info: api.PodInfo{
				"net": {},
			},
		},
		nodes: []api.Node{*makeNode("machine")},
		pods:  []api.Pod{*pod},
	}
	cache := config.Construct()
	err := cache.updatePodStatus(&config.pods[0])
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	status, err := cache.GetPodStatus(pod.Namespace, pod.Name)
	if e, a := &config.kubeletContainerInfo, status; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected: %+v, Got %+v", e, a)
	}
	if status.PodIP != expectedIP {
		t.Errorf("Expected %s, Got %s", expectedIP, status.PodIP)
	}
}

func TestPodPhaseWithBadNode(t *testing.T) {
	desiredState := api.PodSpec{
		Containers: []api.Container{
			{Name: "containerA"},
			{Name: "containerB"},
		},
		RestartPolicy: api.RestartPolicy{Always: &api.RestartPolicyAlways{}},
	}
	runningState := api.ContainerStatus{
		State: api.ContainerState{
			Running: &api.ContainerStateRunning{},
		},
	}
	stoppedState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{},
		},
	}

	tests := []struct {
		pod    *api.Pod
		status api.PodPhase
		test   string
	}{
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Host: "machine-2",
				},
			},
			api.PodFailed,
			"no info, but bad machine",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": runningState,
					},
					Host: "machine-two",
				},
			},
			api.PodFailed,
			"all running but minion is missing",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": stoppedState,
						"containerB": stoppedState,
					},
					Host: "machine-two",
				},
			},
			api.PodFailed,
			"all stopped but minion missing",
		},
	}
	for _, test := range tests {
		config := podCacheTestConfig{
			kubeletContainerInfo: test.pod.Status,
			nodes:                []api.Node{},
			pods:                 []api.Pod{*test.pod},
		}
		cache := config.Construct()
		cache.UpdateAllContainers()
		status, err := cache.GetPodStatus(test.pod.Namespace, test.pod.Name)
		if err != nil {
			t.Errorf("%v: Unexpected error %v", test.test, err)
			continue
		}
		if e, a := test.status, status.Phase; e != a {
			t.Errorf("In test %s, expected %v, got %v", test.test, e, a)
		}
	}
}

func TestPodPhaseWithRestartAlways(t *testing.T) {
	desiredState := api.PodSpec{
		Containers: []api.Container{
			{Name: "containerA"},
			{Name: "containerB"},
		},
		RestartPolicy: api.RestartPolicy{Always: &api.RestartPolicyAlways{}},
	}
	currentState := api.PodStatus{
		Host: "machine",
	}
	runningState := api.ContainerStatus{
		State: api.ContainerState{
			Running: &api.ContainerStateRunning{},
		},
	}
	stoppedState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{},
		},
	}

	tests := []struct {
		pod    *api.Pod
		status api.PodPhase
		test   string
	}{
		{&api.Pod{Spec: desiredState, Status: currentState}, api.PodPending, "waiting"},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": runningState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"all running",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": stoppedState,
						"containerB": stoppedState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"all stopped with restart always",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": stoppedState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"mixed state #1 with restart always",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
					},
					Host: "machine",
				},
			},
			api.PodPending,
			"mixed state #2 with restart always",
		},
	}
	for _, test := range tests {
		if status := getPhase(&test.pod.Spec, test.pod.Status.Info); status != test.status {
			t.Errorf("In test %s, expected %v, got %v", test.test, test.status, status)
		}
	}
}

func TestPodPhaseWithRestartNever(t *testing.T) {
	desiredState := api.PodSpec{
		Containers: []api.Container{
			{Name: "containerA"},
			{Name: "containerB"},
		},
		RestartPolicy: api.RestartPolicy{Never: &api.RestartPolicyNever{}},
	}
	currentState := api.PodStatus{
		Host: "machine",
	}
	runningState := api.ContainerStatus{
		State: api.ContainerState{
			Running: &api.ContainerStateRunning{},
		},
	}
	succeededState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{
				ExitCode: 0,
			},
		},
	}
	failedState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{
				ExitCode: -1,
			},
		},
	}

	tests := []struct {
		pod    *api.Pod
		status api.PodPhase
		test   string
	}{
		{&api.Pod{Spec: desiredState, Status: currentState}, api.PodPending, "waiting"},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": runningState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"all running with restart never",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": succeededState,
						"containerB": succeededState,
					},
					Host: "machine",
				},
			},
			api.PodSucceeded,
			"all succeeded with restart never",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": failedState,
						"containerB": failedState,
					},
					Host: "machine",
				},
			},
			api.PodFailed,
			"all failed with restart never",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": succeededState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"mixed state #1 with restart never",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
					},
					Host: "machine",
				},
			},
			api.PodPending,
			"mixed state #2 with restart never",
		},
	}
	for _, test := range tests {
		if status := getPhase(&test.pod.Spec, test.pod.Status.Info); status != test.status {
			t.Errorf("In test %s, expected %v, got %v", test.test, test.status, status)
		}
	}
}

func TestPodPhaseWithRestartOnFailure(t *testing.T) {
	desiredState := api.PodSpec{
		Containers: []api.Container{
			{Name: "containerA"},
			{Name: "containerB"},
		},
		RestartPolicy: api.RestartPolicy{OnFailure: &api.RestartPolicyOnFailure{}},
	}
	currentState := api.PodStatus{
		Host: "machine",
	}
	runningState := api.ContainerStatus{
		State: api.ContainerState{
			Running: &api.ContainerStateRunning{},
		},
	}
	succeededState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{
				ExitCode: 0,
			},
		},
	}
	failedState := api.ContainerStatus{
		State: api.ContainerState{
			Termination: &api.ContainerStateTerminated{
				ExitCode: -1,
			},
		},
	}

	tests := []struct {
		pod    *api.Pod
		status api.PodPhase
		test   string
	}{
		{&api.Pod{Spec: desiredState, Status: currentState}, api.PodPending, "waiting"},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": runningState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"all running with restart onfailure",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": succeededState,
						"containerB": succeededState,
					},
					Host: "machine",
				},
			},
			api.PodSucceeded,
			"all succeeded with restart onfailure",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": failedState,
						"containerB": failedState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"all failed with restart never",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
						"containerB": succeededState,
					},
					Host: "machine",
				},
			},
			api.PodRunning,
			"mixed state #1 with restart onfailure",
		},
		{
			&api.Pod{
				Spec: desiredState,
				Status: api.PodStatus{
					Info: map[string]api.ContainerStatus{
						"containerA": runningState,
					},
					Host: "machine",
				},
			},
			api.PodPending,
			"mixed state #2 with restart onfailure",
		},
	}
	for _, test := range tests {
		if status := getPhase(&test.pod.Spec, test.pod.Status.Info); status != test.status {
			t.Errorf("In test %s, expected %v, got %v", test.test, test.status, status)
		}
	}
}
