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

package labels

import (
	"sort"
	"strings"
)

// Labels allows you to present labels independently from their storage.
type Labels interface {
	Get(label string) (value string)
}

// A map of label:value. Implements Labels.
type Set map[string]string

// All labels listed as a human readable string. Conveiently, exactly the format
// that ParseQuery takes.
func (ls Set) String() string {
	query := make([]string, 0, len(ls))
	for key, value := range ls {
		query = append(query, key+"="+value)
	}
	// Sort for determinism.
	sort.StringSlice(query).Sort()
	return strings.Join(query, ",")
}

// Implement Labels interface.
func (ls Set) Get(label string) string {
	return ls[label]
}
