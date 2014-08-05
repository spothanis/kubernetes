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
	"fmt"
	"reflect"
	"testing"

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
var encoding = api.Encoding
var versioning = api.Versioning

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
	fakeClient := MakeFakeEtcdClient(t)
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Nodes: []*etcd.Node{
					{
						Value: `{"id":"foo"}`,
					},
					{
						Value: `{"id":"bar"}`,
					},
					{
						Value: `{"id":"baz"}`,
					},
				},
			},
		},
	}
	expect := []api.Pod{
		{JSONBase: api.JSONBase{ID: "foo"}},
		{JSONBase: api.JSONBase{ID: "bar"}},
		{JSONBase: api.JSONBase{ID: "baz"}},
	}
	var got []api.Pod
	helper := EtcdHelper{fakeClient, encoding, versioning}
	err := helper.ExtractList("/some/key", &got)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	if !reflect.DeepEqual(got, expect) {
		t.Errorf("Wanted %#v, got %#v", expect, got)
	}
}

func TestExtractObj(t *testing.T) {
	fakeClient := MakeFakeEtcdClient(t)
	expect := api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	fakeClient.Set("/some/key", util.MakeJSONString(expect), 0)
	helper := EtcdHelper{fakeClient, encoding, versioning}
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
	fakeClient := MakeFakeEtcdClient(t)
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
	helper := EtcdHelper{fakeClient, encoding, versioning}
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
	fakeClient := MakeFakeEtcdClient(t)
	helper := EtcdHelper{fakeClient, encoding, versioning}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := encoding.Encode(obj)
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
	fakeClient := MakeFakeEtcdClient(t)
	fakeClient.TestIndex = true
	fakeClient.Data["/some/key"] = EtcdResponseWithError{
		R: &etcd.Response{
			Node: &etcd.Node{
				Value:         api.EncodeOrDie(obj),
				ModifiedIndex: 1,
			},
		},
	}

	helper := EtcdHelper{fakeClient, encoding, versioning}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Fatalf("Unexpected error %#v", err)
	}
	data, err := encoding.Encode(obj)
	if err != nil {
		t.Fatalf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}
}

func TestSetObjWithoutVersioning(t *testing.T) {
	obj := api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	fakeClient := MakeFakeEtcdClient(t)
	helper := EtcdHelper{fakeClient, encoding, nil}
	err := helper.SetObj("/some/key", obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := encoding.Encode(obj)
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
	fakeClient := MakeFakeEtcdClient(t)
	fakeClient.TestIndex = true
	encoding := scheme
	helper := EtcdHelper{fakeClient, encoding, api.JSONBaseVersioning{}}

	// Create a new node.
	fakeClient.ExpectNotFoundGet("/some/key")
	obj := TestResource{JSONBase: api.JSONBase{ID: "foo"}, Value: 1}
	err := helper.AtomicUpdate("/some/key", &TestResource{}, func(in interface{}) (interface{}, error) {
		return obj, nil
	})
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	data, err := encoding.Encode(obj)
	if err != nil {
		t.Errorf("Unexpected error %#v", err)
	}
	expect := string(data)
	got := fakeClient.Data["/some/key"].R.Node.Value
	if expect != got {
		t.Errorf("Wanted %v, got %v", expect, got)
	}
	return

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
	data, err = encoding.Encode(objUpdate)
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

func TestWatchInterpretation_ListAdd(t *testing.T) {
	w := newEtcdWatcher(true, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, encoding)
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := encoding.Encode(pod)

	go w.sendResult(&etcd.Response{
		Action: "set",
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

func TestWatchInterpretation_Delete(t *testing.T) {
	w := newEtcdWatcher(true, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, encoding)
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := encoding.Encode(pod)

	go w.sendResult(&etcd.Response{
		Action: "delete",
		PrevNode: &etcd.Node{
			Value: string(podBytes),
		},
	})

	got := <-w.outgoing
	if e, a := watch.Deleted, got.Type; e != a {
		t.Errorf("Expected %v, got %v", e, a)
	}
	if e, a := pod, got.Object; !reflect.DeepEqual(e, a) {
		t.Errorf("Expected %v, got %v", e, a)
	}
}

func TestWatchInterpretation_ResponseNotSet(t *testing.T) {
	w := newEtcdWatcher(false, func(interface{}) bool {
		t.Errorf("unexpected filter call")
		return true
	}, encoding)
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
	}, encoding)
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
	}, encoding)
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
	fakeClient := MakeFakeEtcdClient(t)
	h := EtcdHelper{fakeClient, encoding, versioning}

	watching, err := h.Watch("/some/key")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	fakeClient.WaitForWatchCompletion()

	// Test normal case
	pod := &api.Pod{JSONBase: api.JSONBase{ID: "foo"}}
	podBytes, _ := encoding.Encode(pod)
	fakeClient.WatchResponse <- &etcd.Response{
		Action: "set",
		Node: &etcd.Node{
			Value: string(podBytes),
		},
	}

	event := <-watching.ResultChan()
	if e, a := watch.Added, event.Type; e != a {
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

func TestWatchPurposefulShutdown(t *testing.T) {
	fakeClient := MakeFakeEtcdClient(t)
	h := EtcdHelper{fakeClient, encoding, versioning}

	// Test purposeful shutdown
	watching, err := h.Watch("/some/key")
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
