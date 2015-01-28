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

package e2e

import (
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/gomega"
)

type testResult bool

func (t *testResult) Fail() { *t = false }

// Run each Go end-to-end-test. This function assumes the
// creation of a test cluster.
func RunE2ETests(authConfig, certDir, host, repoRoot, provider string, orderseed int64, times int, testList []string) {
	testContext = testContextType{authConfig, certDir, host, repoRoot, provider}
	util.ReallyCrash = true
	util.InitLogs()
	defer util.FlushLogs()

	// TODO: Associate a timeout with each test individually.
	go func() {
		defer util.FlushLogs()
		// TODO: We should modify testSpec to include an estimated running time
		//       for each test and use that information to estimate a timeout
		//       value. Until then, as we add more tests (and before we move to
		//       parallel testing) we need to adjust this value as we add more tests.
		time.Sleep(15 * time.Minute)
		glog.Fatalf("This test has timed out. Cleanup not guaranteed.")
	}()

	// TODO: Make -t TestName work again.
	// TODO: Make "times" work again.
	// TODO: Make orderseed work again.

	var passed testResult = true
	gomega.RegisterFailHandler(ginkgo.Fail)
	// Turn of colors for now to make it easier to collect console output in Jenkins
	config.DefaultReporterConfig.NoColor = true
	ginkgo.RunSpecs(&passed, "Kubernetes e2e Suite")

	if !passed {
		glog.Fatalf("At least one test failed")
	} else {
		glog.Infof("All tests pass")
	}
}
