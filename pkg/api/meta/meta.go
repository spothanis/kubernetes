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

package meta

import (
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/conversion"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
)

// Interface lets you work with object metadata from any of the versioned or
// internal API objects.
type Interface interface {
	Name() string
	SetName(name string)
	UID() string
	SetUID(uid string)
	APIVersion() string
	SetAPIVersion(version string)
	Kind() string
	SetKind(kind string)
	ResourceVersion() string
	SetResourceVersion(version string)
	SelfLink() string
	SetSelfLink(selfLink string)
}

// Accessor takes an arbitary object pointer and returns meta.Interface.
// obj must be a pointer to an API type. An error is returned if the minimum
// required fields are missing. Fields that are not required return the default
// value and are a no-op if set.
func Accessor(obj interface{}) (Interface, error) {
	v, err := conversion.EnforcePtr(obj)
	if err != nil {
		return nil, err
	}
	t := v.Type()
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, but got %v: %v (%#v)", v.Kind(), t, v.Interface())
	}

	typeMeta := v.FieldByName("TypeMeta")
	if !typeMeta.IsValid() {
		return nil, fmt.Errorf("struct %v lacks embedded TypeMeta type", t)
	}

	a := &genericAccessor{}
	if err := extractFromTypeMeta(typeMeta, a); err != nil {
		return nil, fmt.Errorf("unable to find type fields on %#v", typeMeta)
	}

	objectMeta := v.FieldByName("ObjectMeta")
	if objectMeta.IsValid() {
		// look for the ObjectMeta fields
		if err := extractFromObjectMeta(objectMeta, a); err != nil {
			return nil, fmt.Errorf("unable to find object fields on %#v", objectMeta)
		}
	} else {
		listMeta := v.FieldByName("ListMeta")
		if listMeta.IsValid() {
			// look for the ListMeta fields
			if err := extractFromListMeta(listMeta, a); err != nil {
				return nil, fmt.Errorf("unable to find list fields on %#v", listMeta)
			}
		} else {
			// look for the older TypeMeta with all metadata
			if err := extractFromObjectMeta(typeMeta, a); err != nil {
				return nil, fmt.Errorf("unable to find object fields on %#v", typeMeta)
			}
		}
	}

	return a, nil
}

// NewResourceVersioner returns a ResourceVersioner that can set or
// retrieve ResourceVersion on objects derived from TypeMeta.
func NewResourceVersioner() runtime.ResourceVersioner {
	return resourceAccessor{}
}

// resourceAccessor implements ResourceVersioner and SelfLinker.
type resourceAccessor struct{}

func (v resourceAccessor) ResourceVersion(obj runtime.Object) (string, error) {
	accessor, err := Accessor(obj)
	if err != nil {
		return "", err
	}
	return accessor.ResourceVersion(), nil
}

func (v resourceAccessor) SetResourceVersion(obj runtime.Object, version string) error {
	accessor, err := Accessor(obj)
	if err != nil {
		return err
	}
	accessor.SetResourceVersion(version)
	return nil
}

func (v resourceAccessor) Name(obj runtime.Object) (string, error) {
	accessor, err := Accessor(obj)
	if err != nil {
		return "", err
	}
	return accessor.Name(), nil
}

func (v resourceAccessor) SelfLink(obj runtime.Object) (string, error) {
	accessor, err := Accessor(obj)
	if err != nil {
		return "", err
	}
	return accessor.SelfLink(), nil
}

func (v resourceAccessor) SetSelfLink(obj runtime.Object, selfLink string) error {
	accessor, err := Accessor(obj)
	if err != nil {
		return err
	}
	accessor.SetSelfLink(selfLink)
	return nil
}

// NewSelfLinker returns a SelfLinker that works on all TypeMeta SelfLink fields.
func NewSelfLinker() runtime.SelfLinker {
	return resourceAccessor{}
}

// genericAccessor contains pointers to strings that can modify an arbitrary
// struct and implements the Accessor interface.
type genericAccessor struct {
	name            *string
	uid             *string
	apiVersion      *string
	kind            *string
	resourceVersion *string
	selfLink        *string
}

func (a genericAccessor) Name() string {
	if a.name == nil {
		return ""
	}
	return *a.name
}

func (a genericAccessor) SetName(name string) {
	if a.name == nil {
		return
	}
	*a.name = name
}

func (a genericAccessor) UID() string {
	if a.uid == nil {
		return ""
	}
	return *a.uid
}

func (a genericAccessor) SetUID(uid string) {
	if a.uid == nil {
		return
	}
	*a.uid = uid
}

func (a genericAccessor) APIVersion() string {
	return *a.apiVersion
}

func (a genericAccessor) SetAPIVersion(version string) {
	*a.apiVersion = version
}

func (a genericAccessor) Kind() string {
	return *a.kind
}

func (a genericAccessor) SetKind(kind string) {
	*a.kind = kind
}

func (a genericAccessor) ResourceVersion() string {
	return *a.resourceVersion
}

func (a genericAccessor) SetResourceVersion(version string) {
	*a.resourceVersion = version
}

func (a genericAccessor) SelfLink() string {
	return *a.selfLink
}

func (a genericAccessor) SetSelfLink(selfLink string) {
	*a.selfLink = selfLink
}

// fieldPtr puts the address of fieldName, which must be a member of v,
// into dest, which must be an address of a variable to which this field's
// address can be assigned.
func fieldPtr(v reflect.Value, fieldName string, dest interface{}) error {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return fmt.Errorf("Couldn't find %v field in %#v", fieldName, v.Interface())
	}
	v = reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("dest should be ptr")
	}
	v = v.Elem()
	field = field.Addr()
	if field.Type().AssignableTo(v.Type()) {
		v.Set(field)
		return nil
	}
	if field.Type().ConvertibleTo(v.Type()) {
		v.Set(field.Convert(v.Type()))
		return nil
	}
	return fmt.Errorf("Couldn't assign/convert %v to %v", field.Type(), v.Type())
}

// extractFromTypeMeta extracts pointers to version and kind fields from an object
func extractFromTypeMeta(v reflect.Value, a *genericAccessor) error {
	if err := fieldPtr(v, "APIVersion", &a.apiVersion); err != nil {
		return err
	}
	if err := fieldPtr(v, "Kind", &a.kind); err != nil {
		return err
	}
	return nil
}

// extractFromObjectMeta extracts pointers to metadata fields from an object
func extractFromObjectMeta(v reflect.Value, a *genericAccessor) error {
	if err := fieldPtr(v, "Name", &a.name); err != nil {
		return err
	}
	if err := fieldPtr(v, "UID", &a.uid); err != nil {
		return err
	}
	if err := fieldPtr(v, "ResourceVersion", &a.resourceVersion); err != nil {
		return err
	}
	if err := fieldPtr(v, "SelfLink", &a.selfLink); err != nil {
		return err
	}
	return nil
}

// extractFromObjectMeta extracts pointers to metadata fields from a list object
func extractFromListMeta(v reflect.Value, a *genericAccessor) error {
	if err := fieldPtr(v, "ResourceVersion", &a.resourceVersion); err != nil {
		return err
	}
	if err := fieldPtr(v, "SelfLink", &a.selfLink); err != nil {
		return err
	}
	return nil
}
