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

package scheduler

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

func matchesPredicate(pod api.Pod, existingPods []api.Pod, node string) (bool, error) {
	return pod.ID == node, nil
}

func evenPriority(pod api.Pod, podLister PodLister, minionLister MinionLister) (HostPriorityList, error) {
	nodes, err := minionLister.List()
	result := []HostPriority{}

	if err != nil {
		fmt.Errorf("failed to list nodes: %v", err)
		return []HostPriority{}, err
	}
	for _, minion := range nodes {
		result = append(result, HostPriority{
			host:  minion,
			score: 1,
		})
	}
	return result, nil
}

func numericPriority(pod api.Pod, podLister PodLister, minionLister MinionLister) (HostPriorityList, error) {
	nodes, err := minionLister.List()
	result := []HostPriority{}

	if err != nil {
		fmt.Errorf("failed to list nodes: %v", err)
		return nil, err
	}
	for _, minion := range nodes {
		score, err := strconv.Atoi(minion)
		if err != nil {
			return nil, err
		}
		result = append(result, HostPriority{
			host:  minion,
			score: score,
		})
	}
	return result, nil
}

func TestGenericScheduler(t *testing.T) {
	tests := []struct {
		predicates   []FitPredicate
		prioritizer  PriorityFunction
		nodes        []string
		pod          api.Pod
		expectedHost string
		expectsErr   bool
	}{
		{
			predicates:  []FitPredicate{falsePredicate},
			prioritizer: evenPriority,
			nodes:       []string{"machine1", "machine2"},
			expectsErr:  true,
		},
		{
			predicates:  []FitPredicate{truePredicate},
			prioritizer: evenPriority,
			nodes:       []string{"machine1", "machine2"},
			// Random choice between both, the rand seeded above with zero, chooses "machine2"
			expectedHost: "machine2",
		},
		{
			// Fits on a machine where the pod ID matches the machine name
			predicates:   []FitPredicate{matchesPredicate},
			prioritizer:  evenPriority,
			nodes:        []string{"machine1", "machine2"},
			pod:          api.Pod{JSONBase: api.JSONBase{ID: "machine2"}},
			expectedHost: "machine2",
		},
		{
			predicates:   []FitPredicate{truePredicate},
			prioritizer:  numericPriority,
			nodes:        []string{"3", "2", "1"},
			expectedHost: "1",
		},
		{
			predicates:   []FitPredicate{matchesPredicate},
			prioritizer:  numericPriority,
			nodes:        []string{"3", "2", "1"},
			pod:          api.Pod{JSONBase: api.JSONBase{ID: "2"}},
			expectedHost: "2",
		},
		{
			predicates:  []FitPredicate{truePredicate, falsePredicate},
			prioritizer: numericPriority,
			nodes:       []string{"3", "2", "1"},
			expectsErr:  true,
		},
	}

	for _, test := range tests {
		random := rand.New(rand.NewSource(0))
		scheduler := NewGenericScheduler(test.predicates, test.prioritizer, FakePodLister([]api.Pod{}), random)
		machine, err := scheduler.Schedule(test.pod, FakeMinionLister(test.nodes))
		if test.expectsErr {
			if err == nil {
				t.Error("Unexpected non-error")
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if test.expectedHost != machine {
				t.Errorf("Expected: %s, Saw: %s", test.expectedHost, machine)
			}
		}
	}
}
