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

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/fsouza/go-dockerclient"
)

// TODO: This doesn't reduce typing enough to make it worth the less readable errors. Remove.
func expectNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Unexpected error: %#v", err)
	}
}

func TestHTTPContainerInfo(t *testing.T) {
	expectObj := &docker.Container{ID: "myID"}
	body, err := json.Marshal(expectObj)
	expectNoError(t, err)
	fakeHandler := util.FakeHandler{
		StatusCode:   200,
		ResponseBody: string(body),
	}
	testServer := httptest.NewServer(&fakeHandler)

	hostUrl, err := url.Parse(testServer.URL)
	expectNoError(t, err)
	parts := strings.Split(hostUrl.Host, ":")

	port, err := strconv.Atoi(parts[1])
	expectNoError(t, err)
	containerInfo := &HTTPContainerInfo{
		Client: http.DefaultClient,
		Port:   uint(port),
	}
	gotObj, err := containerInfo.GetContainerInfo(parts[0], "foo")
	expectNoError(t, err)

	// reflect.DeepEqual(expectObj, gotObj) doesn't handle blank times well
	if expectObj.ID != gotObj.ID {
		t.Errorf("Unexpected response.  Expected: %#v, received %#v", expectObj, gotObj)
	}
}
