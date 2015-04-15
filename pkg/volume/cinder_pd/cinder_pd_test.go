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

package cinder_pd

import (
	"os"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/types"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util/mount"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/volume"
)

func TestCanSupport(t *testing.T) {
	plugMgr := volume.VolumePluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), volume.NewFakeVolumeHost("/tmp/fake", nil, nil))

	plug, err := plugMgr.FindPluginByName("kubernetes.io/cinder-pd")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	if plug.Name() != "kubernetes.io/cinder-pd" {
		t.Errorf("Wrong name: %s", plug.Name())
	}
	if !plug.CanSupport(&api.Volume{VolumeSource: api.VolumeSource{CinderPersistentDisk: &api.CinderPersistentDiskVolumeSource{}}}) {
		t.Errorf("Expected true")
	}
}

type fakePDManager struct{}

func (fake *fakePDManager) AttachDisk(pd *cinderPersistentDisk, globalPDPath string) error {
	globalPath := makeGlobalPDName(pd.plugin.host, pd.pdName)
	err := os.MkdirAll(globalPath, 0750)
	if err != nil {
		return err
	}
	return nil
}

func (fake *fakePDManager) DetachDisk(pd *cinderPersistentDisk) error {
	globalPath := makeGlobalPDName(pd.plugin.host, pd.pdName)
	err := os.RemoveAll(globalPath)
	if err != nil {
		return err
	}
	return nil
}

func TestPlugin(t *testing.T) {
	plugMgr := volume.VolumePluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), volume.NewFakeVolumeHost("/tmp/fake", nil, nil))

	plug, err := plugMgr.FindPluginByName("kubernetes.io/cinder-pd")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	spec := &api.Volume{
		Name: "vol1",
		VolumeSource: api.VolumeSource{
			CinderPersistentDisk: &api.CinderPersistentDiskVolumeSource{
				PDName: "pd",
				FSType: "ext4",
			},
		},
	}
	builder, err := plug.(*cinderPersistentDiskPlugin).newBuilderInternal(spec, types.UID("poduid"), &fakePDManager{}, &mount.FakeMounter{})
	if err != nil {
		t.Errorf("Failed to make a new Builder: %v", err)
	}
	if builder == nil {
		t.Errorf("Got a nil Builder: %v")
	}

	path := builder.GetPath()
	if path != "/tmp/fake/pods/poduid/volumes/kubernetes.io~cinder-pd/vol1" {
		t.Errorf("Got unexpected path: %s", path)
	}

	if err := builder.SetUp(); err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Errorf("SetUp() failed, volume path not created: %s", path)
		} else {
			t.Errorf("SetUp() failed: %v", err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Errorf("SetUp() failed, volume path not created: %s", path)
		} else {
			t.Errorf("SetUp() failed: %v", err)
		}
	}

	cleaner, err := plug.(*cinderPersistentDiskPlugin).newCleanerInternal("vol1", types.UID("poduid"), &fakePDManager{}, &mount.FakeMounter{})
	if err != nil {
		t.Errorf("Failed to make a new Cleaner: %v", err)
	}
	if cleaner == nil {
		t.Errorf("Got a nil Cleaner: %v")
	}

	if err := cleaner.TearDown(); err != nil {
		t.Errorf("Expected success, got: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("TearDown() failed, volume path still exists: %s", path)
	} else if !os.IsNotExist(err) {
		t.Errorf("SetUp() failed: %v", err)
	}
}

func TestPluginLegacy(t *testing.T) {
	plugMgr := volume.VolumePluginMgr{}
	plugMgr.InitPlugins(ProbeVolumePlugins(), volume.NewFakeVolumeHost("/tmp/fake", nil, nil))

	plug, err := plugMgr.FindPluginByName("cinder-pd")
	if err != nil {
		t.Errorf("Can't find the plugin by name")
	}
	if plug.Name() != "cinder-pd" {
		t.Errorf("Wrong name: %s", plug.Name())
	}
	if plug.CanSupport(&api.Volume{VolumeSource: api.VolumeSource{CinderPersistentDisk: &api.CinderPersistentDiskVolumeSource{}}}) {
		t.Errorf("Expected false")
	}

	if _, err := plug.NewBuilder(&api.Volume{VolumeSource: api.VolumeSource{CinderPersistentDisk: &api.CinderPersistentDiskVolumeSource{}}}, &api.ObjectReference{UID: types.UID("poduid")}); err == nil {
		t.Errorf("Expected failiure")
	}

	cleaner, err := plug.NewCleaner("vol1", types.UID("poduid"))
	if err != nil {
		t.Errorf("Failed to make a new Cleaner: %v", err)
	}
	if cleaner == nil {
		t.Errorf("Got a nil Cleaner: %v")
	}
}
