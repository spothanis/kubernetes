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

package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
)

func TestGetTCPAddrParts(t *testing.T) {
	testCases := []struct {
		probe *api.TCPSocketProbe
		ok    bool
		host  string
		port  int
	}{
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromInt(-1)}, false, "", -1},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromString("")}, false, "", -1},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromString("-1")}, false, "", -1},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromString("not-found")}, false, "", -1},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromString("found")}, true, "1.2.3.4", 93},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromInt(76)}, true, "1.2.3.4", 76},
		{&api.TCPSocketProbe{Port: util.MakeIntOrStringFromString("118")}, true, "1.2.3.4", 118},
	}

	for _, test := range testCases {
		state := api.PodState{PodIP: "1.2.3.4"}
		container := api.Container{
			Ports: []api.Port{{Name: "found", HostPort: 93}},
			LivenessProbe: &api.LivenessProbe{
				TCPSocket: test.probe,
				Type:      "tcp",
			},
		}
		host, port, err := getTCPAddrParts(state, container)
		if !test.ok && err == nil {
			t.Errorf("Expected error for %+v, got %s:%d", test, host, port)
		}
		if test.ok && err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if test.ok {
			if host != test.host || port != test.port {
				t.Errorf("Expected %s:%d, got %s:%d", test.host, test.port, host, port)
			}
		}
	}
}

func TestTcpHealthChecker(t *testing.T) {
	tests := []struct {
		probe          *api.TCPSocketProbe
		expectedStatus Status
		expectError    bool
	}{
		// The probe will be filled in below.  This is primarily testing that a connection is made.
		{&api.TCPSocketProbe{}, Healthy, false},
		{&api.TCPSocketProbe{}, Unhealthy, false},
		{nil, Unknown, true},
	}

	checker := &TCPHealthChecker{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	for _, test := range tests {
		container := api.Container{
			LivenessProbe: &api.LivenessProbe{
				TCPSocket: test.probe,
				Type:      "tcp",
			},
		}
		params := container.LivenessProbe.TCPSocket
		if params != nil && test.expectedStatus == Healthy {
			params.Port = util.MakeIntOrStringFromString(port)
		}
		status, err := checker.HealthCheck(api.PodState{PodIP: host}, container)
		if status != test.expectedStatus {
			t.Errorf("expected: %v, got: %v", test.expectedStatus, status)
		}
		if err != nil && !test.expectError {
			t.Errorf("unexpected error: %#v", err)
		}
		if err == nil && test.expectError {
			t.Errorf("unexpected non-error.")
		}
	}
}
