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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/admission"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/latest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/meta"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"

	"github.com/emicklei/go-restful"
	"github.com/golang/glog"
)

// mux is an object that can register http handlers.
type Mux interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// defaultAPIServer exposes nested objects for testability.
type defaultAPIServer struct {
	http.Handler
	group *APIGroupVersion
}

// Handle returns a Handler function that exposes the provided storage interfaces
// as RESTful resources at prefix, serialized by codec, and also includes the support
// http resources.
// Note: This method is used only in tests.
func Handle(storage map[string]RESTStorage, codec runtime.Codec, root string, version string, selfLinker runtime.SelfLinker, admissionControl admission.Interface, mapper meta.RESTMapper) http.Handler {
	prefix := root + "/" + version
	group := NewAPIGroupVersion(storage, codec, root, prefix, selfLinker, admissionControl, mapper)
	container := restful.NewContainer()
	container.Router(restful.CurlyRouter{})
	mux := container.ServeMux
	group.InstallREST(container, root, version)
	ws := new(restful.WebService)
	InstallSupport(mux, ws)
	container.Add(ws)
	return &defaultAPIServer{mux, group}
}

// TODO: This is a whole API version right now. Maybe should rename it.
// APIGroupVersion is a http.Handler that exposes multiple RESTStorage objects
// It handles URLs of the form:
// /${storage_key}[/${object_name}]
// Where 'storage_key' points to a RESTStorage object stored in storage.
//
// TODO: consider migrating this to go-restful which is a more full-featured version of the same thing.
type APIGroupVersion struct {
	handler RESTHandler
	mapper  meta.RESTMapper
}

// NewAPIGroupVersion returns an object that will serve a set of REST resources and their
// associated operations.  The provided codec controls serialization and deserialization.
// This is a helper method for registering multiple sets of REST handlers under different
// prefixes onto a server.
// TODO: add multitype codec serialization
func NewAPIGroupVersion(storage map[string]RESTStorage, codec runtime.Codec, apiRoot, canonicalPrefix string, selfLinker runtime.SelfLinker, admissionControl admission.Interface, mapper meta.RESTMapper) *APIGroupVersion {
	return &APIGroupVersion{
		handler: RESTHandler{
			storage:                storage,
			codec:                  codec,
			canonicalPrefix:        canonicalPrefix,
			selfLinker:             selfLinker,
			ops:                    NewOperations(),
			admissionControl:       admissionControl,
			apiRequestInfoResolver: &APIRequestInfoResolver{util.NewStringSet(apiRoot), latest.RESTMapper},
		},
		mapper: mapper,
	}
}

// InstallREST registers the REST handlers (storage, watch, proxy and redirect) into a restful Container.
// It is expected that the provided path root prefix will serve all operations. Root MUST NOT end
// in a slash. A restful WebService is created for the group and version.
func (g *APIGroupVersion) InstallREST(container *restful.Container, root string, version string) error {
	prefix := path.Join(root, version)
	ws, registrationErrors := (&APIInstaller{prefix, version, &g.handler, g.mapper}).Install()
	container.Add(ws)
	return errors.NewAggregate(registrationErrors)
}

// TODO: Convert to go-restful
func InstallValidator(mux Mux, servers func() map[string]Server) {
	validator, err := NewValidator(servers)
	if err != nil {
		glog.Errorf("failed to set up validator: %v", err)
		return
	}
	if validator != nil {
		mux.Handle("/validate", validator)
	}
}

// TODO: document all handlers
// InstallSupport registers the APIServer support functions
func InstallSupport(mux Mux, ws *restful.WebService) {
	// TODO: convert healthz to restful and remove container arg
	healthz.InstallHandler(mux)

	// Set up a service to return the git code version.
	ws.Path("/version")
	ws.Doc("git code version from which this is built")
	ws.Route(
		ws.GET("/").To(handleVersion).
			Doc("get the code version").
			Operation("getCodeVersion").
			Produces(restful.MIME_JSON).
			Consumes(restful.MIME_JSON))
}

// InstallLogsSupport registers the APIServer log support function into a mux.
func InstallLogsSupport(mux Mux) {
	// TODO: use restful: ws.Route(ws.GET("/logs/{logpath:*}").To(fileHandler))
	// See github.com/emicklei/go-restful/blob/master/examples/restful-serve-static.go
	mux.Handle("/logs/", http.StripPrefix("/logs/", http.FileServer(http.Dir("/var/log/"))))
}

// Adds a service to return the supported api versions.
func AddApiWebService(container *restful.Container, apiPrefix string, versions []string) {
	// TODO: InstallREST should register each version automatically

	versionHandler := APIVersionHandler(versions[:]...)
	getApiVersionsWebService := new(restful.WebService)
	getApiVersionsWebService.Path(apiPrefix)
	getApiVersionsWebService.Doc("get available api versions")
	getApiVersionsWebService.Route(getApiVersionsWebService.GET("/").To(versionHandler).
		Doc("get available api versions").
		Operation("getApiVersions").
		Produces(restful.MIME_JSON).
		Consumes(restful.MIME_JSON))
	container.Add(getApiVersionsWebService)
}

// handleVersion writes the server's version information.
func handleVersion(req *restful.Request, resp *restful.Response) {
	// TODO: use restful's Response methods
	writeRawJSON(http.StatusOK, version.Get(), resp.ResponseWriter)
}

// APIVersionHandler returns a handler which will list the provided versions as available.
func APIVersionHandler(versions ...string) restful.RouteFunction {
	return func(req *restful.Request, resp *restful.Response) {
		// TODO: use restful's Response methods
		writeRawJSON(http.StatusOK, api.APIVersions{Versions: versions}, resp.ResponseWriter)
	}
}

// writeJSON renders an object as JSON to the response.
func writeJSON(statusCode int, codec runtime.Codec, object runtime.Object, w http.ResponseWriter) {
	output, err := codec.Encode(object)
	if err != nil {
		errorJSONFatal(err, codec, w)
		return
	}
	// PR #2243: Pretty-print JSON by default.
	formatted := &bytes.Buffer{}
	err = json.Indent(formatted, output, "", "  ")
	if err != nil {
		errorJSONFatal(err, codec, w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(formatted.Bytes())
}

// errorJSON renders an error to the response.
func errorJSON(err error, codec runtime.Codec, w http.ResponseWriter) {
	status := errToAPIStatus(err)
	writeJSON(status.Code, codec, status, w)
}

// errorJSONFatal renders an error to the response, and if codec fails will render plaintext
func errorJSONFatal(err error, codec runtime.Codec, w http.ResponseWriter) {
	util.HandleError(fmt.Errorf("apiserver was unable to write a JSON response: %v", err))
	status := errToAPIStatus(err)
	output, err := codec.Encode(status)
	if err != nil {
		w.WriteHeader(status.Code)
		fmt.Fprintf(w, "%s: %s", status.Reason, status.Message)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status.Code)
	w.Write(output)
}

// writeRawJSON writes a non-API object in JSON.
func writeRawJSON(statusCode int, object interface{}, w http.ResponseWriter) {
	output, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(output)
}

func parseTimeout(str string) time.Duration {
	if str != "" {
		timeout, err := time.ParseDuration(str)
		if err == nil {
			return timeout
		}
		glog.Errorf("Failed to parse %q: %v", str, err)
	}
	return 30 * time.Second
}

func readBody(req *http.Request) ([]byte, error) {
	defer req.Body.Close()
	return ioutil.ReadAll(req.Body)
}

// splitPath returns the segments for a URL path.
func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}
