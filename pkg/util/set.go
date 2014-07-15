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

package util

import (
	"sort"
)

type empty struct{}

// StringSet is a set of strings, implemented via map[string]struct{} for minimal memory consumption.
type StringSet map[string]empty

// NewStringSet creates a StringSet from a list of values.
func NewStringSet(items ...string) StringSet {
	ss := StringSet{}
	ss.Insert(items...)
	return ss
}

// Insert adds items to the set.
func (s StringSet) Insert(items ...string) {
	for _, item := range items {
		s[item] = empty{}
	}
}

// Delete removes item from the set.
func (s StringSet) Delete(item string) {
	delete(s, item)
}

// Has returns true iff item is contained in the set.
func (s StringSet) Has(item string) bool {
	_, contained := s[item]
	return contained
}

// List returns the contents as a sorted string slice.
func (s StringSet) List() []string {
	res := make([]string, 0, len(s))
	for key := range s {
		res = append(res, key)
	}
	sort.StringSlice(res).Sort()
	return res
}
