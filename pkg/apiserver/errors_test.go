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

package apiserver

import (
	"errors"
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

func TestErrorNew(t *testing.T) {
	err := NewAlreadyExistsErr("test", "1")
	if !IsAlreadyExists(err) {
		t.Errorf("expected to be already_exists")
	}
	if IsConflict(err) {
		t.Errorf("expected to not be confict")
	}
	if IsNotFound(err) {
		t.Errorf(fmt.Sprintf("expected to not be %s", api.ReasonTypeNotFound))
	}

	if !IsConflict(NewConflictErr("test", "2", errors.New("message"))) {
		t.Errorf("expected to be confict")
	}
	if !IsNotFound(NewNotFoundErr("test", "3")) {
		t.Errorf("expected to be not found")
	}
}
