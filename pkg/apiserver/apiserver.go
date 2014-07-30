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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"code.google.com/p/go.net/websocket"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/httplog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/golang/glog"
)

// errNotFound is an error which indicates that a specified resource is not found.
type errNotFound string

// Error returns a string representation of the err.
func (err errNotFound) Error() string {
	return string(err)
}

// IsNotFound determines if the err is an error which indicates that a specified resource was not found.
func IsNotFound(err error) bool {
	_, ok := err.(errNotFound)
	return ok
}

// NewNotFoundErr returns a new error which indicates that the resource of the kind and the name was not found.
func NewNotFoundErr(kind, name string) error {
	return errNotFound(fmt.Sprintf("%s %q not found", kind, name))
}

// APIServer is an HTTPHandler that delegates to RESTStorage objects.
// It handles URLs of the form:
// ${prefix}/${storage_key}[/${object_name}]
// Where 'prefix' is an arbitrary string, and 'storage_key' points to a RESTStorage object stored in storage.
//
// TODO: consider migrating this to go-restful which is a more full-featured version of the same thing.
type APIServer struct {
	prefix  string
	storage map[string]RESTStorage
	ops     *Operations
	mux     *http.ServeMux
}

// New creates a new APIServer object.
// 'storage' contains a map of handlers.
// 'prefix' is the hosting path prefix.
func New(storage map[string]RESTStorage, prefix string) *APIServer {
	s := &APIServer{
		storage: storage,
		prefix:  strings.TrimRight(prefix, "/"),
		ops:     NewOperations(),
		mux:     http.NewServeMux(),
	}

	s.mux.Handle("/logs/", http.StripPrefix("/logs/", http.FileServer(http.Dir("/var/log/"))))
	s.mux.HandleFunc(s.prefix+"/", s.handleREST)
	healthz.InstallHandler(s.mux)

	s.mux.HandleFunc("/version", s.handleVersionReq)
	s.mux.HandleFunc("/", handleIndex)

	// Handle both operations and operations/* with the same handler
	s.mux.HandleFunc(s.operationPrefix(), s.handleOperationRequest)
	s.mux.HandleFunc(s.operationPrefix()+"/", s.handleOperationRequest)

	s.mux.HandleFunc(s.watchPrefix()+"/", s.handleWatch)

	s.mux.HandleFunc("/proxy/minion/", s.handleProxyMinion)

	return s
}

// handleVersionReq writes the server's version information.
func (server *APIServer) handleVersionReq(w http.ResponseWriter, req *http.Request) {
	server.writeRawJSON(http.StatusOK, version.Get(), w)
}

// HTTP Handler interface
func (s *APIServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if x := recover(); x != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "apiserver panic. Look in log for details.")
			glog.Infof("APIServer panic'd on %v %v: %#v\n%s\n", req.Method, req.RequestURI, x, debug.Stack())
		}
	}()
	defer httplog.MakeLogged(req, &w).StacktraceWhen(
		httplog.StatusIsNot(
			http.StatusOK,
			http.StatusAccepted,
			http.StatusConflict,
			http.StatusNotFound,
		),
	).Log()

	// Dispatch via our mux.
	s.mux.ServeHTTP(w, req)
}

// handleREST handles requests to all our RESTStorage objects.
func (s *APIServer) handleREST(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, s.prefix) {
		notFound(w, req)
		return
	}
	requestParts := strings.Split(req.URL.Path[len(s.prefix):], "/")[1:]
	if len(requestParts) < 1 {
		notFound(w, req)
		return
	}
	storage := s.storage[requestParts[0]]
	if storage == nil {
		httplog.LogOf(w).Addf("'%v' has no storage object", requestParts[0])
		notFound(w, req)
		return
	}

	s.handleRESTStorage(requestParts, req, w, storage)
}

// write writes an API object in wire format.
func (s *APIServer) write(statusCode int, object interface{}, w http.ResponseWriter) {
	output, err := api.Encode(object)
	if err != nil {
		internalError(err, w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(output)
}

// writeRawJSON writes a non-API object in JSON.
func (s *APIServer) writeRawJSON(statusCode int, object interface{}, w http.ResponseWriter) {
	output, err := json.Marshal(object)
	if err != nil {
		internalError(err, w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(output)
}

// finishReq finishes up a request, waiting until the operation finishes or, after a timeout, creating an
// Operation to receive the result and returning its ID down the writer.
func (s *APIServer) finishReq(out <-chan interface{}, sync bool, timeout time.Duration, w http.ResponseWriter) {
	op := s.ops.NewOperation(out)
	if sync {
		op.WaitFor(timeout)
	}
	obj, complete := op.StatusOrResult()
	if complete {
		status := http.StatusOK
		switch stat := obj.(type) {
		case api.Status:
			httplog.LogOf(w).Addf("programmer error: use *api.Status as a result, not api.Status.")
			if stat.Code != 0 {
				status = stat.Code
			}
		case *api.Status:
			if stat.Code != 0 {
				status = stat.Code
			}
		}
		s.write(status, obj, w)
	} else {
		s.write(http.StatusAccepted, obj, w)
	}
}

// handleRESTStorage is the main dispatcher for a storage object.  It switches on the HTTP method, and then
// on path length, according to the following table:
//   Method     Path          Action
//   GET        /foo          list
//   GET        /foo/bar      get 'bar'
//   POST       /foo          create
//   PUT        /foo/bar      update 'bar'
//   DELETE     /foo/bar      delete 'bar'
// Returns 404 if the method/pattern doesn't match one of these entries
// The s accepts several query parameters:
//    sync=[false|true] Synchronous request (only applies to create, update, delete operations)
//    timeout=<duration> Timeout for synchronous requests, only applies if sync=true
//    labels=<label-selector> Used for filtering list operations
func (s *APIServer) handleRESTStorage(parts []string, req *http.Request, w http.ResponseWriter, storage RESTStorage) {
	sync := req.URL.Query().Get("sync") == "true"
	timeout := parseTimeout(req.URL.Query().Get("timeout"))
	switch req.Method {
	case "GET":
		switch len(parts) {
		case 1:
			selector, err := labels.ParseSelector(req.URL.Query().Get("labels"))
			if err != nil {
				internalError(err, w)
				return
			}
			list, err := storage.List(selector)
			if err != nil {
				internalError(err, w)
				return
			}
			s.write(http.StatusOK, list, w)
		case 2:
			item, err := storage.Get(parts[1])
			if IsNotFound(err) {
				notFound(w, req)
				return
			}
			if err != nil {
				internalError(err, w)
				return
			}
			s.write(http.StatusOK, item, w)
		default:
			notFound(w, req)
		}
	case "POST":
		if len(parts) != 1 {
			notFound(w, req)
			return
		}
		body, err := readBody(req)
		if err != nil {
			internalError(err, w)
			return
		}
		obj, err := storage.Extract(body)
		if IsNotFound(err) {
			notFound(w, req)
			return
		}
		if err != nil {
			internalError(err, w)
			return
		}
		out, err := storage.Create(obj)
		if IsNotFound(err) {
			notFound(w, req)
			return
		}
		if err != nil {
			internalError(err, w)
			return
		}
		s.finishReq(out, sync, timeout, w)
	case "DELETE":
		if len(parts) != 2 {
			notFound(w, req)
			return
		}
		out, err := storage.Delete(parts[1])
		if IsNotFound(err) {
			notFound(w, req)
			return
		}
		if err != nil {
			internalError(err, w)
			return
		}
		s.finishReq(out, sync, timeout, w)
	case "PUT":
		if len(parts) != 2 {
			notFound(w, req)
			return
		}
		body, err := readBody(req)
		if err != nil {
			internalError(err, w)
			return
		}
		obj, err := storage.Extract(body)
		if IsNotFound(err) {
			notFound(w, req)
			return
		}
		if err != nil {
			internalError(err, w)
			return
		}
		out, err := storage.Update(obj)
		if IsNotFound(err) {
			notFound(w, req)
			return
		}
		if err != nil {
			internalError(err, w)
			return
		}
		s.finishReq(out, sync, timeout, w)
	default:
		notFound(w, req)
	}
}

func (s *APIServer) operationPrefix() string {
	return path.Join(s.prefix, "operations")
}

func (s *APIServer) handleOperationRequest(w http.ResponseWriter, req *http.Request) {
	opPrefix := s.operationPrefix()
	if !strings.HasPrefix(req.URL.Path, opPrefix) {
		notFound(w, req)
		return
	}
	trimmed := strings.TrimLeft(req.URL.Path[len(opPrefix):], "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) > 1 {
		notFound(w, req)
		return
	}
	if req.Method != "GET" {
		notFound(w, req)
		return
	}
	if len(parts) == 0 {
		// List outstanding operations.
		list := s.ops.List()
		s.write(http.StatusOK, list, w)
		return
	}

	op := s.ops.Get(parts[0])
	if op == nil {
		notFound(w, req)
		return
	}

	obj, complete := op.StatusOrResult()
	if complete {
		s.write(http.StatusOK, obj, w)
	} else {
		s.write(http.StatusAccepted, obj, w)
	}
}

func (s *APIServer) watchPrefix() string {
	return path.Join(s.prefix, "watch")
}

func (s *APIServer) handleWatch(w http.ResponseWriter, req *http.Request) {
	prefix := s.watchPrefix()
	if !strings.HasPrefix(req.URL.Path, prefix) {
		notFound(w, req)
		return
	}
	parts := strings.Split(req.URL.Path[len(prefix):], "/")[1:]
	if req.Method != "GET" || len(parts) < 1 {
		notFound(w, req)
	}
	storage := s.storage[parts[0]]
	if storage == nil {
		notFound(w, req)
	}
	if watcher, ok := storage.(ResourceWatcher); ok {
		var watching watch.Interface
		var err error
		if id := req.URL.Query().Get("id"); id != "" {
			watching, err = watcher.WatchSingle(id)
		} else {
			watching, err = watcher.WatchAll()
		}
		if err != nil {
			internalError(err, w)
			return
		}

		// TODO: This is one watch per connection. We want to multiplex, so that
		// multiple watches of the same thing don't create two watches downstream.
		watchServer := &WatchServer{watching}
		if req.Header.Get("Connection") == "Upgrade" && req.Header.Get("Upgrade") == "websocket" {
			websocket.Handler(watchServer.HandleWS).ServeHTTP(httplog.Unlogged(w), req)
		} else {
			watchServer.ServeHTTP(w, req)
		}
		return
	}

	notFound(w, req)
}

func parseTimeout(str string) time.Duration {
	if str != "" {
		timeout, err := time.ParseDuration(str)
		if err == nil {
			return timeout
		}
		glog.Errorf("Failed to parse: %#v '%s'", err, str)
	}
	return 30 * time.Second
}

func readBody(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	return ioutil.ReadAll(req.Body)
}
