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
	"fmt"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/httplog"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
)

// RecoverPanics wraps an http Handler to recover and log panics.
func RecoverPanics(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if x := recover(); x != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "apis panic. Look in log for details.")
				glog.Infof("APIServer panic'd on %v %v: %#v\n%s\n", req.Method, req.RequestURI, x, debug.Stack())
			}
		}()
		defer httplog.NewLogged(req, &w).StacktraceWhen(
			httplog.StatusIsNot(
				http.StatusOK,
				http.StatusAccepted,
				http.StatusMovedPermanently,
				http.StatusTemporaryRedirect,
				http.StatusConflict,
				http.StatusNotFound,
				StatusUnprocessableEntity,
			),
		).Log()

		// Dispatch to the internal handler
		handler.ServeHTTP(w, req)
	})
}

// Simple CORS implementation that wraps an http Handler
// For a more detailed implementation use https://github.com/martini-contrib/cors
// or implement CORS at your proxy layer
// Pass nil for allowedMethods and allowedHeaders to use the defaults
func CORS(handler http.Handler, allowedOriginPatterns util.StringList, allowedMethods []string, allowedHeaders []string, allowCredentials string) http.Handler {
	// Compile the regular expressions once upfront
	allowedOriginRegexps := []*regexp.Regexp{}
	for _, allowedOrigin := range allowedOriginPatterns {
		allowedOriginRegexp, err := regexp.Compile(allowedOrigin)
		if err != nil {
			glog.Fatalf("Invalid CORS allowed origin regexp: %v", err)
		}
		allowedOriginRegexps = append(allowedOriginRegexps, allowedOriginRegexp)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		if origin != "" {
			allowed := false
			for _, pattern := range allowedOriginRegexps {
				if allowed = pattern.MatchString(origin); allowed {
					break
				}
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// Set defaults for methods and headers if nothing was passed
				if allowedMethods == nil {
					allowedMethods = []string{"POST", "GET", "OPTIONS", "PUT", "DELETE"}
				}
				if allowedHeaders == nil {
					allowedHeaders = []string{"Accept", "Content-Type", "Content-Length", "Accept-Encoding", "X-CSRF-Token", "Authorization"}
				}
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
				w.Header().Set("Access-Control-Allow-Credentials", allowCredentials)

				// Stop here if its a preflight OPTIONS request
				if req.Method == "OPTIONS" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
		}
		// Dispatch to the next handler
		handler.ServeHTTP(w, req)
	})
}
