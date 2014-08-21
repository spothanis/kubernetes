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

package tools

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/conversion"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/coreos/go-etcd/etcd"
)

type fakeClientGetSet struct {
	get func(key string, sort, recursive bool) (*etcd.Response, error)
	set func(key, value string, ttl uint64) (*etcd.Response, error)
}

type TestResource struct {
	api.JSONBase `json:",inline" yaml:",inline"`
	Value        int `json:"value" yaml:"value,omitempty"`
}

var scheme *conversion.Scheme
var codec = api.Codec
var versioner = api.ResourceVersioner

func init() {
	scheme = conversion.NewScheme()
	scheme.ExternalVersion = "v1beta1"
	scheme.AddKnownTypes("", TestResource{})
	scheme.AddKnownTypes("v1beta1", TestResource{})
}

func TestIsEtcdNotFound(t *testing.T) {
	try := func(err error, isNotFound bool) {
		if IsEtcdNotFound(err) != isNotFound {
			t.Errorf("Expected %#v to return %v, but it did not", err, isNotFound)
		}
	}
	try(EtcdErrorNotFound, true)
	try(&etcd.EtcdError{ErrorCode: 101}, false)
	try(nil, false)
	try(fmt.Errorf("some other kind of error"), false)
}

func TestExtractList(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value:         `{"id":"foo"}`,
						ModifiedIndex: 1,
					},
					{
						Value:         `{"id":"bar"}`,
						ModifiedIndex: 2,
					},
					{
						Value:         `{"id":"baz"}`,
						ModifiedIndex: 3,
					},
				},
			},
		},
	}
	expect := []api.Pod{
		{JSONBase: api.JSONBase{ID: "foo", ResourceVersion: 1}},
		{JSONBase: api.JSONBase{ID: "bar", ResourceVersion: 2}},
		{JSONBase: api.JSONBase{ID: "baz", ResourceVersion: 3}},
	}

	var got []api.Pod
	helper := EtcdHelper{fakeClient, codec, versioner}
	err := helper.ExtractList("/some/key", &got)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}

	for i := 0; i < len(expect); i++ {
		if !reflect.DeepEqual(got[i], expect[i]) {
			t.Errorf("\nWanted:\n%#v\nGot:\n%#v\n", expect[i], got[i])
		}
	}
}

func TestExtractObj(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	expect := api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	fakeClient.Set("/some/key", util.EncodeJSON(expect), 0)
	helper := EtcdHelper{fakeClient, codec, versioner}
	var got api.Pod
	err := helper.ExtractObj("/some/key", &got, false)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	if !reflect.DeepEqual(got, expect) {
		t.Errorf("Wanted %#v, got %#v", expect, got)
	}
}

func TestExtractObjNotFoundErr(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: &etcd.EtcdError{
			ErrorCode: 100,
		},
	}
	fakeClient.Data["/some/key2"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
	}
	fakeClient.Data["/some/key3"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value: "",
			},
		},
	}
	helper := EtcdHelper{fakeClient, codec, versioner}
	try := func(key string) {
		var got api.Pod
		err := helper.ExtractObj(key, &got, false)
		if err == nil {
			t.Errorf("%s: wanted error but didn't get one", key)
		}
		err = helper.ExtractObj(key, &got, true)
		if err != nil {
			t.Errorf("%s: didn't want error but got %#v", key, err)
		}
	}

	try("/some/key")
	try("/some/key2")
	try("/some/key3")
}

func TestSetObj(t *testing.T) {
	obj := api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	fakeClient := NewFakeEtcdClient(t)
	helper := EtcdHelper{fakeClient, codec, versioner}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := codec.Encode(obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}
}

func TestSetObjWithVersion(t *testing.T) {
	obj := api.Pod{JSONBase: api.JSONBase{ID: "foo", ResourceVersion: 1}}
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value:         api.EncodeOrDie(obj),
				ModifiedIndex: 1,
			},
		},
	}

	helper := EtcdHelper{fakeClient, codec, versioner}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Fatalf("Unexpected error %#v", err)
	}
	data, err := codec.Encode(obj)
	if err != nil {
		t.Fatalf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}
}

func TestSetObjWithoutResourceVersioner(t *testing.T) {
	obj := api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	fakeClient := NewFakeEtcdClient(t)
	helper := EtcdHelper{fakeClient, codec, nil}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := codec.Encode(obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}
}

func TestAtomicUpdate(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	codec := scheme
	helper := EtcdHelper{fakeClient, codec, api.NewJSONBaseResourceVersioner()}

	// Create a new node.
	fakeClient.ExpectNotFoundGet("/some/key")
	obj := &TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: 1}
	err := helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
		return obj, nil
	})
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := codec.Encode(obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}

	// Update an existing node.
	callbackCalled := false
	objUpdate := &TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: 2}
	err = helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
		callbackCalled = true

		if in.(*TestResource).Value != 1 {
			t.Errorf("Callback input was not current set value")
		}

		return objUpdate, nil
	})
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err = codec.Encode(objUpdate)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	expect = string(data)
	got = fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}

	if !callbackCalled {
		t.Errorf("tryUpdate callback should have been called.")
	}
}

func TestAtomicUpdateNoChange(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	helper := EtcdHelper{fakeClient, scheme, api.NewJSONBaseResourceVersioner()}

	// Create a new node.
	fakeClient.ExpectNotFoundGet("/some/key")
	obj := &TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: 1}
	err := helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
		return obj, nil
	})
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}

	// Update an existing node with the same data
	callbackCalled := false
	objUpdate := &TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: 1}
	fakeClient.Err = errors.New("should not be called")
	err = helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
		callbackCalled = true
		return objUpdate, nil
	})
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	if !callbackCalled {
		t.Errorf("tryUpdate callback should have been called.")
	}
}

func TestAtomicUpdate_CreateCollision(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.TestIndex = true
	codec := scheme
	helper := EtcdHelper{fakeClient, codec, api.NewJSONBaseResourceVersioner()}

	fakeClient.ExpectNotFoundGet("/some/key")

	const concurrency = 10
	var wgDone sync.WaitGroup
	var wgForceCollision sync.WaitGroup
	wgDone.Add(concurrency)
	wgForceCollision.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		// Increment TestResource.Value by 1
		go func() {
			defer wgDone.Done()

			firstCall := true
			err := helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
				defer func() { firstCall = false }()

				if firstCall {
					// Force collision by joining all concurrent AtomicUpdate operations here.
					wgForceCollision.Done()
					wgForceCollision.Wait()
				}

				currValue := in.(*TestResource).Value
				obj := TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: currValue + 1}
				return obj, nil
			})
			if err != nil {
				t.Errorf("Unexpected error %#v", err)
			}
		}()
	}
	wgDone.Wait()

	// Check that stored TestResource has received all updates.
	body := fakeClient.Data["/some/key"].R.Node.Value
	stored := &TestResource{}
	if err := codec.DecodeInto([]byte(body), stored); err != nil {
		t.Errorf("Error decoding stored value: %v", body)
	}
	if stored.Value != concurrency {
		t.Errorf("Some of the writes were lost. Stored value: %d", stored.Value)
	}
}

func TestWatchInterpretation_ListCreate(t *testing.T) {
	w := newEtcdWatcher(true, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := codec.Encode(pod)

	go w.sendResult(&etcd.Response{
		Action: "create",
		Node: &etcd.Node{
			Value: string(podBytes),
		},
	})

	got := <-w.outgoing
	if e, a := watch.Added, got.Type; e != a {
		t.Errorf("Expected %v, got %v", e, a)
	}
	if e, a := pod, got.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected %v, got %v", e, a)
	}
}

func TestWatchInterpretation_ListAdd(t *testing.T) {
	w := newEtcdWatcher(true, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := codec.Encode(pod)

	go w.sendResult(&etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: string(podBytes),
		},
	})

	got := <-w.outgoing
	if e, a := watch.Modified, got.Type; e != a {
		t.Errorf("Expected %v, got %v", e, a)
	}
	if e, a := pod, got.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected %v, got %v", e, a)
	}
}

func TestWatchInterpretation_Delete(t *testing.T) {
	w := newEtcdWatcher(true, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := codec.Encode(pod)

	go w.sendResult(&etcd.Response{
		Action: "delete",
		Node: &etcd.Node{
			ModifiedIndex: 2,
		},
		PrevNode: &etcd.Node{
			Value:         string(podBytes),
			ModifiedIndex: 1,
		},
	})

	got := <-w.outgoing
	if e, a := watch.Deleted, got.Type; e != a {
		t.Errorf("Expected %v, got %v", e, a)
	}
	pod.ResourceVersion = 2
	if e, a := pod, got.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected %v, got %v", e, a)
	}
}

func TestWatchInterpretation_ResponseNotSet(t *testing.T) {
	w := newEtcdWatcher(false, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	w.emit = func(e watch.Event) {
		t.Errorf("Unexpected emit: %v", e)
	}

	w.sendResult(&etcd.Response{
		Action: "update",
	})
}

func TestWatchInterpretation_ResponseNoNode(t *testing.T) {
	w := newEtcdWatcher(false, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	w.emit = func(e watch.Event) {
		t.Errorf("Unexpected emit: %v", e)
	}
	w.sendResult(&etcd.Response{
		Action: "set",
	})
}

func TestWatchInterpretation_ResponseBadData(t *testing.T) {
	w := newEtcdWatcher(false, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, codec, versioner, nil)
	w.emit = func(e watch.Event) {
		t.Errorf("Unexpected emit: %v", e)
	}
	w.sendResult(&etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: "foobar",
		},
	})
}

func TestWatch(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.expectNotFoundGetSet["/some/key"] = struct{}{}
	h := EtcdHelper{fakeClient, codec, versioner}

	watching, err := h.Watch("/some/key", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	fakeClient.WaitForWatchCompletion()
	// when server returns not found, the watch index starts at the next value (1)
	if fakeClient.WatchIndex != 1 {
		t.Errorf("Expected client to be at index %d, got %#v", 1, fakeClient)
	}

	// Test normal case
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := codec.Encode(pod)
	fakeClient.WatchResponse <- &etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: string(podBytes),
		},
	}

	event := <-watching.ResultChan()
	if e, a := watch.Modified, event.Type; e != a {
		t.Errorf("Expected %v, got %v", e, a)
	}
	if e, a := pod, event.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected %v, got %v", e, a)
	}

	// Test error case
	fakeClient.WatchInjectError <- fmt.Errorf("Injected error")

	// Did everything shut down?
	if _, open := <-fakeClient.WatchResponse; open {
		t.Errorf("An injected error did not cause a graceful shutdown")
	}
	if _, open := <-watching.ResultChan(); open {
		t.Errorf("An injected error did not cause a graceful shutdown")
	}
}

func TestWatchFromZeroIndex(t *testing.T) {
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}

	testCases := map[string]struct {
		Response        EtcdResponseWithError
		ExpectedVersion uint64
		ExpectedType    watch.EventType
	}{
		"get value created": {
			EtcdResponseWithError{
				R: &etcd.Response{
					Node: &etcd.Node{
						Value:         api.EncodeOrDie(pod),
						CreatedIndex:  1,
						ModifiedIndex: 1,
					},
					Action:    "get",
					EtcdIndex: 2,
				},
			},
			1,
			watch.Added,
		},
		"get value modified": {
			EtcdResponseWithError{
				R: &etcd.Response{
					Node: &etcd.Node{
						Value:         api.EncodeOrDie(pod),
						CreatedIndex:  1,
						ModifiedIndex: 2,
					},
					Action:    "get",
					EtcdIndex: 3,
				},
			},
			2,
			watch.Modified,
		},
	}

	for k, testCase := range testCases {
		fakeClient := NewFakeEtcdClient(t)
		fakeClient.Data["/some/key"] = testCase.Response
		h := EtcdHelper{fakeClient, codec, versioner}

		watching, err := h.Watch("/some/key", 0)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", k, err)
		}

		fakeClient.WaitForWatchCompletion()
		if e, a := testCase.Response.R.EtcdIndex+1, fakeClient.WatchIndex; e != a {
			t.Errorf("%s: expected watch index to be %d, got %d", k, e, a)
		}

		// the existing node is detected and the index set
		event := <-watching.ResultChan()
		if e, a := testCase.ExpectedType, event.Type; e != a {
			t.Errorf("%s: expected %v, got %v", k, e, a)
		}
		actualPod, ok := event.Object.(*api.Pod)
		if !ok {
			t.Fatalf("%s: expected a pod, got %#v", k, event.Object)
		}
		if actualPod.ResourceVersion != testCase.ExpectedVersion {
			t.Errorf("%s: expected pod with resource version %d, Got %#v", k, testCase.ExpectedVersion, actualPod)
		}
		pod.ResourceVersion = testCase.ExpectedVersion
		if e, a := pod, event.Object; !reflect.DeepEqual(e, a) {
			t.Errorf("%s: expected %v, got %v", k, e, a)
		}
		watching.Stop()
	}
}

func TestWatchListFromZeroIndex(t *testing.T) {
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}

	fakeClient := NewFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Dir: true,
				Nodes: etcd.Nodes{
					&etcd.Node{
						Value:         api.EncodeOrDie(pod),
						ModifiedIndex: 1,
						Nodes:         etcd.Nodes{},
					},
					&etcd.Node{
						Value:         api.EncodeOrDie(pod),
						ModifiedIndex: 2,
						Nodes:         etcd.Nodes{},
					},
				},
			},
			Action:    "get",
			EtcdIndex: 3,
		},
	}
	h := EtcdHelper{fakeClient, codec, versioner}

	watching, err := h.WatchList("/some/key", 0, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// the existing node is detected and the index set
	event := <-watching.ResultChan()
	for i := 0; i < 2; i++ {
		if e, a := watch.Modified, event.Type; e != a {
			t.Errorf("Expected %v, got %v", e, a)
		}
		actualPod, ok := event.Object.(*api.Pod)
		if !ok {
			t.Fatalf("expected a pod, got %#v", event.Object)
		}
		if actualPod.ResourceVersion != 1 {
			t.Errorf("Expected pod with resource version %d, Got %#v", 1, actualPod)
		}
		pod.ResourceVersion = 1
		if e, a := pod, event.Object; !reflect.DeepEqual(e, a) {
			t.Errorf("Expected %v, got %v", e, a)
		}
	}

	fakeClient.WaitForWatchCompletion()
	watching.Stop()
}

func TestWatchFromNotFound(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: &etcd.EtcdError{
			Index:     2,
			ErrorCode: 100,
		},
	}
	h := EtcdHelper{fakeClient, codec, versioner}

	watching, err := h.Watch("/some/key", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	fakeClient.WaitForWatchCompletion()
	if fakeClient.WatchIndex != 3 {
		t.Errorf("Expected client to wait for %d, got %#v", 3, fakeClient)
	}

	watching.Stop()
}

func TestWatchFromOtherError(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: nil,
		},
		E: &etcd.EtcdError{
			Index:     2,
			ErrorCode: 101,
		},
	}
	h := EtcdHelper{fakeClient, codec, versioner}

	watching, err := h.Watch("/some/key", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	select {
	case _, ok := <-watching.ResultChan():
		if ok {
			t.Fatalf("expected result channel to be closed")
		}
	case <-time.After(1 * time.Millisecond):
		t.Fatalf("watch should have closed channel: %#v", watching)
	}

	if fakeClient.WatchResponse != nil || fakeClient.WatchIndex != 0 {
		t.Fatalf("Watch should not have been invoked: %#v", fakeClient)
	}
}

func TestWatchPurposefulShutdown(t *testing.T) {
	fakeClient := NewFakeEtcdClient(t)
	h := EtcdHelper{fakeClient, codec, versioner}
	fakeClient.expectNotFoundGetSet["/some/key"] = struct{}{}

	// Test purposeful shutdown
	watching, err := h.Watch("/some/key", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	fakeClient.WaitForWatchCompletion()
	watching.Stop()

	// Did everything shut down?
	if _, open := <-fakeClient.WatchResponse; open {
		t.Errorf("A stop did not cause a graceful shutdown")
	}
	if _, open := <-watching.ResultChan(); open {
		t.Errorf("An injected error did not cause a graceful shutdown")
	}
}
