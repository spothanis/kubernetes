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

package resttest

import (
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
)

type Tester struct {
	*testing.T
	storage apiserver.RESTStorage
}

func New(t *testing.T, storage apiserver.RESTStorage) *Tester {
	return &Tester{
		T:       t,
		storage: storage,
	}
}

func copyOrDie(obj runtime.Object) runtime.Object {
	out, err := api.Scheme.Copy(obj)
	if err != nil {
		panic(err)
	}
	return out
}

func (t *Tester) TestCreate(valid runtime.Object, invalid ...runtime.Object) {
	t.TestCreateHasMetadata(copyOrDie(valid))
	t.TestCreateGeneratesName(copyOrDie(valid))
	t.TestCreateRejectsMismatchedNamespace(copyOrDie(valid))
	t.TestCreateInvokesValidation(invalid...)
}

func (t *Tester) TestCreateHasMetadata(valid runtime.Object) {
	objectMeta, err := api.ObjectMetaFor(valid)
	if err != nil {
		t.Fatalf("object does not have ObjectMeta: %v\n%#v", err, valid)
	}

	objectMeta.Name = "test"
	objectMeta.Namespace = api.NamespaceDefault

	channel, err := t.storage.(apiserver.RESTCreater).Create(api.NewDefaultContext(), valid)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if obj := <-channel; obj.Object == nil {
		t.Fatalf("Unexpected object from channel: %#v", obj)
	}
	if !api.HasObjectMetaSystemFieldValues(objectMeta) {
		t.Errorf("storage did not populate object meta field values")
	}
}

func (t *Tester) TestCreateGeneratesName(valid runtime.Object) {
	objectMeta, err := api.ObjectMetaFor(valid)
	if err != nil {
		t.Fatalf("object does not have ObjectMeta: %v\n%#v", err, valid)
	}

	objectMeta.GenerateName = "test-"

	_, err = t.storage.(apiserver.RESTCreater).Create(api.NewDefaultContext(), valid)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if objectMeta.Name == "test-" || !strings.HasPrefix(objectMeta.Name, "test-") {
		t.Errorf("unexpected name: %#v", valid)
	}
}

func (t *Tester) TestCreateInvokesValidation(invalid ...runtime.Object) {
	for i, obj := range invalid {
		ctx := api.NewDefaultContext()
		_, err := t.storage.(apiserver.RESTCreater).Create(ctx, obj)
		if !errors.IsInvalid(err) {
			t.Errorf("%d: Expected to get an invalid resource error, got %v", i, err)
		}
	}
}

func (t *Tester) TestCreateRejectsMismatchedNamespace(valid runtime.Object) {
	objectMeta, err := api.ObjectMetaFor(valid)
	if err != nil {
		t.Fatalf("object does not have ObjectMeta: %v\n%#v", err, valid)
	}

	objectMeta.Namespace = "not-default"

	_, err = t.storage.(apiserver.RESTCreater).Create(api.NewDefaultContext(), valid)
	if err == nil {
		t.Errorf("Expected an error, but we didn't get one")
	} else if strings.Contains(err.Error(), "Controller.Namespace does not match the provided context") {
		t.Errorf("Expected 'Controller.Namespace does not match the provided context' error, got '%v'", err.Error())
	}
}
