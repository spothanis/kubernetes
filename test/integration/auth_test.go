// +build integration,!no-etcd

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

package integration

// This file tests authentication and (soon) authorization of HTTP requests to a master object.
// It does not use the client in pkg/client/... because authentication and authorization needs
// to work for any client of the HTTP interface.

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/master"

	"github.com/golang/glog"
)

func init() {
	requireEtcd()
}

const (
	AliceToken   string = "abc123" // username: alice.  Present in token file.
	BobToken     string = "xyz987" // username: bob.  Present in token file.
	UnknownToken string = "qwerty" // Not present in token file.
	// Keep file in sync with above constants.
	TokenfileCSV string = `
abc123,alice,1
xyz987,bob,2
`
)

func writeTestTokenFile() string {
	// Write a token file.
	f, err := ioutil.TempFile("", "auth_integration_test")
	f.Close()
	if err != nil {
		glog.Fatalf("unexpected error: %v", err)
	}
	if err := ioutil.WriteFile(f.Name(), []byte(TokenfileCSV), 0700); err != nil {
		glog.Fatalf("unexpected error writing tokenfile: %v", err)
	}
	return f.Name()
}

// TestWhoAmI passes a known Bearer Token to the master's /_whoami endpoint and checks that
// the master authenticates the user.
func TestWhoAmI(t *testing.T) {
	deleteAllEtcdKeys()

	// Set up a master

	helper, err := master.NewEtcdHelper(newEtcdClient(), "v1beta1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tokenFilename := writeTestTokenFile()
	defer os.Remove(tokenFilename)
	m := master.New(&master.Config{
		EtcdHelper:        helper,
		EnableLogsSupport: false,
		EnableUISupport:   false,
		APIPrefix:         "/api",
		TokenAuthFile:     tokenFilename,
		AuthorizationMode: "AlwaysAllow",
	})

	s := httptest.NewServer(m.Handler)
	defer s.Close()

	// TODO: also test TLS, using e.g NewUnsafeTLSTransport() and NewClientCertTLSTransport() (see pkg/client/helper.go)
	transport := http.DefaultTransport

	testCases := []struct {
		name     string
		token    string
		expected string
		succeeds bool
	}{
		{"Valid token", AliceToken, "AUTHENTICATED AS alice", true},
		{"Unknown token", UnknownToken, "", false},
		{"No token", "", "", false},
	}
	for _, tc := range testCases {
		req, err := http.NewRequest("GET", s.URL+"/_whoami", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tc.token))
		{
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.succeeds {
				body, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				actual := string(body)
				if tc.expected != actual {
					t.Errorf("case: %s expected: %v got: %v", tc.name, tc.expected, actual)
				}
			} else {
				if resp.StatusCode != http.StatusUnauthorized {
					t.Errorf("case: %s expected Unauthorized, got: %v", tc.name, resp.StatusCode)
				}

			}
		}
	}
}

// Bodies for requests used in subsequent tests.
var aPod string = `
{
  "kind": "Pod",
  "apiVersion": "v1beta1",
  "id": "a",
  "desiredState": {
    "manifest": {
      "version": "v1beta1",
      "id": "a",
      "containers": [{ "name": "foo", "image": "bar/foo", }]
    }
  },
}
`
var aRC string = `
{
  "kind": "ReplicationController",
  "apiVersion": "v1beta1",
  "id": "a",
  "desiredState": {
    "replicas": 2,
    "replicaSelector": {"name": "a"},
    "podTemplate": {
      "desiredState": {
         "manifest": {
           "version": "v1beta1",
           "id": "a",
           "containers": [{
             "name": "foo",
             "image": "bar/foo",
           }]
         }
       },
       "labels": {"name": "a"}
      }},
  "labels": {"name": "a"}
}
`
var aService string = `
{
  "kind": "Service",
  "apiVersion": "v1beta1",
  "id": "a",
  "port": 8000,
  "labels": { "name": "a" },
  "selector": { "name": "a" }
}
`
var aMinion string = `
{
  "kind": "Minion",
  "apiVersion": "v1beta1",
  "id": "a",
  "hostIP": "10.10.10.10",
}
`

var aEvent string = `
{
  "kind": "Event",
  "apiVersion": "v1beta1",
  "id": "a",
  "involvedObject": {
    {
      "kind": "Minion",
      "name": "a",
      "apiVersion": "v1beta1",
    }
  }
}
`

var aBinding string = `
{
  "kind": "Binding",
  "apiVersion": "v1beta1",
  "id": "a",
  "host": "10.10.10.10",
  "podID": "a"
}
`

var aEndpoints string = `
{
  "kind": "Endpoints",
  "apiVersion": "v1beta1",
  "id": "a",
  "endpoints": ["10.10.1.1:1909"],
}
`

// Requests to try.  Each one should be forbidden or not forbidden
// depending on the authentication and authorization setup of the master.

var code200or202 = map[int]bool{200: true, 202: true} // Unpredicatable which will be returned.
var code404 = map[int]bool{404: true}
var code409 = map[int]bool{409: true}
var code422 = map[int]bool{422: true}
var code500 = map[int]bool{500: true}

func getTestRequests() []struct {
	verb        string
	URL         string
	body        string
	statusCodes map[int]bool // allowed status codes.
} {
	requests := []struct {
		verb        string
		URL         string
		body        string
		statusCodes map[int]bool // Set of expected resp.StatusCode if all goes well.
	}{
		// Normal methods on pods
		{"GET", "/api/v1beta1/pods", "", code200or202},
		{"POST", "/api/v1beta1/pods", aPod, code200or202},
		{"PUT", "/api/v1beta1/pods/a", aPod, code500}, // See #2114 about why 500
		{"GET", "/api/v1beta1/pods", "", code200or202},
		{"GET", "/api/v1beta1/pods/a", "", code200or202},
		{"DELETE", "/api/v1beta1/pods/a", "", code200or202},

		// Non-standard methods (not expected to work,
		// but expected to pass/fail authorization prior to
		// failing validation.
		{"PATCH", "/api/v1beta1/pods/a", "", code404},
		{"OPTIONS", "/api/v1beta1/pods", "", code404},
		{"OPTIONS", "/api/v1beta1/pods/a", "", code404},
		{"HEAD", "/api/v1beta1/pods", "", code404},
		{"HEAD", "/api/v1beta1/pods/a", "", code404},
		{"TRACE", "/api/v1beta1/pods", "", code404},
		{"TRACE", "/api/v1beta1/pods/a", "", code404},
		{"NOSUCHVERB", "/api/v1beta1/pods", "", code404},

		// Normal methods on services
		{"GET", "/api/v1beta1/services", "", code200or202},
		{"POST", "/api/v1beta1/services", aService, code200or202},
		{"PUT", "/api/v1beta1/services/a", aService, code422}, // TODO: GET and put back server-provided fields to avoid a 422
		{"GET", "/api/v1beta1/services", "", code200or202},
		{"GET", "/api/v1beta1/services/a", "", code200or202},
		{"DELETE", "/api/v1beta1/services/a", "", code200or202},

		// Normal methods on replicationControllers
		{"GET", "/api/v1beta1/replicationControllers", "", code200or202},
		{"POST", "/api/v1beta1/replicationControllers", aRC, code200or202},
		{"PUT", "/api/v1beta1/replicationControllers/a", aRC, code409}, // See #2115 about why 409
		{"GET", "/api/v1beta1/replicationControllers", "", code200or202},
		{"GET", "/api/v1beta1/replicationControllers/a", "", code200or202},
		{"DELETE", "/api/v1beta1/replicationControllers/a", "", code200or202},

		// Normal methods on endpoints
		{"GET", "/api/v1beta1/endpoints", "", code200or202},
		{"POST", "/api/v1beta1/endpoints", aEndpoints, code200or202},
		{"PUT", "/api/v1beta1/endpoints/a", aEndpoints, code200or202},
		{"GET", "/api/v1beta1/endpoints", "", code200or202},
		{"GET", "/api/v1beta1/endpoints/a", "", code200or202},
		{"DELETE", "/api/v1beta1/endpoints/a", "", code500}, // Issue #2113.

		// Normal methods on minions
		{"GET", "/api/v1beta1/minions", "", code200or202},
		{"POST", "/api/v1beta1/minions", aMinion, code200or202},
		{"PUT", "/api/v1beta1/minions/a", aMinion, code500}, // See #2114 about why 500
		{"GET", "/api/v1beta1/minions", "", code200or202},
		{"GET", "/api/v1beta1/minions/a", "", code200or202},
		{"DELETE", "/api/v1beta1/minions/a", "", code200or202},

		// Normal methods on events
		{"GET", "/api/v1beta1/events", "", code200or202},
		{"POST", "/api/v1beta1/events", aEvent, code200or202},
		{"PUT", "/api/v1beta1/events/a", aEvent, code500}, // See #2114 about why 500
		{"GET", "/api/v1beta1/events", "", code200or202},
		{"GET", "/api/v1beta1/events", "", code200or202},
		{"GET", "/api/v1beta1/events/a", "", code200or202},
		{"DELETE", "/api/v1beta1/events/a", "", code200or202},

		// Normal methods on bindings
		{"GET", "/api/v1beta1/bindings", "", code404},     // Bindings are write-only, so 404
		{"POST", "/api/v1beta1/pods", aPod, code200or202}, // Need a pod to bind or you get a 404
		{"POST", "/api/v1beta1/bindings", aBinding, code200or202},
		{"PUT", "/api/v1beta1/bindings/a", aBinding, code500}, // See #2114 about why 500
		{"GET", "/api/v1beta1/bindings", "", code404},
		{"GET", "/api/v1beta1/bindings/a", "", code404},
		{"DELETE", "/api/v1beta1/bindings/a", "", code404},

		// Non-existent object type.
		{"GET", "/api/v1beta1/foo", "", code404},
		{"POST", "/api/v1beta1/foo", `{"foo": "foo"}`, code404},
		{"PUT", "/api/v1beta1/foo/a", `{"foo": "foo"}`, code404},
		{"GET", "/api/v1beta1/foo", "", code404},
		{"GET", "/api/v1beta1/foo/a", "", code404},
		{"DELETE", "/api/v1beta1/foo", "", code404},

		// Operations
		{"GET", "/api/v1beta1/operations", "", code200or202},
		{"GET", "/api/v1beta1/operations/1234567890", "", code404},

		// Special verbs on pods
		{"GET", "/api/v1beta1/proxy/minions/a", "", code404},
		{"GET", "/api/v1beta1/redirect/minions/a", "", code404},
		// TODO: test .../watch/..., which doesn't end before the test timeout.
		// TODO: figure out how to create a minion so that it can successfully proxy/redirect.

		// Non-object endpoints
		{"GET", "/", "", code200or202},
		{"GET", "/healthz", "", code200or202},
		{"GET", "/version", "", code200or202},
	}
	return requests
}

// The TestAuthMode* tests tests a large number of URLs and checks that they
// are FORBIDDEN or not, depending on the mode.  They do not attempt to do
// detailed verification of behaviour beyond authorization.  They are not
// fuzz tests.
//
// TODO(etune): write a fuzz test of the REST API.
func TestAuthModeAlwaysAllow(t *testing.T) {
	deleteAllEtcdKeys()

	// Set up a master

	helper, err := master.NewEtcdHelper(newEtcdClient(), "v1beta1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := master.New(&master.Config{
		EtcdHelper:        helper,
		EnableLogsSupport: false,
		EnableUISupport:   false,
		APIPrefix:         "/api",
		AuthorizationMode: "AlwaysAllow",
	})

	s := httptest.NewServer(m.Handler)
	defer s.Close()
	transport := http.DefaultTransport

	for _, r := range getTestRequests() {
		t.Logf("case %v", r)
		bodyBytes := bytes.NewReader([]byte(r.body))
		req, err := http.NewRequest(r.verb, s.URL+r.URL, bodyBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		{
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := r.statusCodes[resp.StatusCode]; !ok {
				t.Errorf("Expected status one of %v, but got %v", r.statusCodes, resp.StatusCode)
				b, _ := ioutil.ReadAll(resp.Body)
				t.Errorf("Body: %v", string(b))
			}
		}
	}
}

func TestAuthModeAlwaysDeny(t *testing.T) {
	deleteAllEtcdKeys()

	// Set up a master

	helper, err := master.NewEtcdHelper(newEtcdClient(), "v1beta1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := master.New(&master.Config{
		EtcdHelper:        helper,
		EnableLogsSupport: false,
		EnableUISupport:   false,
		APIPrefix:         "/api",
		AuthorizationMode: "AlwaysDeny",
	})

	s := httptest.NewServer(m.Handler)
	defer s.Close()
	transport := http.DefaultTransport

	for _, r := range getTestRequests() {
		t.Logf("case %v", r)
		bodyBytes := bytes.NewReader([]byte(r.body))
		req, err := http.NewRequest(r.verb, s.URL+r.URL, bodyBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		{
			resp, err := transport.RoundTrip(req)
			defer resp.Body.Close()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("Expected status Forbidden but got status %v", resp.Status)
			}
		}
	}
}
