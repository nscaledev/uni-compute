/*
Copyright 2024-2025 the Unikorn Authors.

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

//nolint:gochecknoglobals,revive,paralleltest,testpackage // global vars and dot imports standard for Ginkgo
package suites

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var (
	client *api.APIClient
	ctx    context.Context
	config *api.TestConfig
)

var _ = BeforeEach(func() {
	config = api.LoadTestConfig()
	client = api.NewAPIClientWithConfig(config)
	ctx = context.Background()
})

func TestSuites(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Test Suites")
}
