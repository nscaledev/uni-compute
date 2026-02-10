/*
Copyright 2025 the Unikorn Authors.
Copyright 2026 Nscale.

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

package region_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive
	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/matchers"
	"github.com/pact-foundation/pact-go/v2/models"

	regionclient "github.com/unikorn-cloud/compute/pkg/server/handler/region"
	contract "github.com/unikorn-cloud/core/pkg/testing/contract"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
)

var testingT *testing.T //nolint:gochecknoglobals

func TestContracts(t *testing.T) { //nolint:paralleltest
	testingT = t

	RegisterFailHandler(Fail)
	RunSpecs(t, "Region Consumer Contract Suite")
}

// createRegionClient creates a region client for the mock server.
func createRegionClient(config consumer.MockServerConfig) (*regionapi.ClientWithResponses, error) {
	url := fmt.Sprintf("http://%s", net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port)))

	return regionapi.NewClientWithResponses(url)
}

// testEmptyRegionsList is a helper for testing empty regions list responses.
func testEmptyRegionsList(ctx context.Context, config consumer.MockServerConfig, organizationID string) error {
	regionClient, err := createRegionClient(config)

	if err != nil {
		return fmt.Errorf("creating region client: %w", err)
	}

	client := regionclient.New(regionClient)
	regions, err := client.List(ctx, organizationID)

	if err != nil {
		return fmt.Errorf("listing regions: %w", err)
	}

	Expect(regions).To(BeEmpty())

	return nil
}

var _ = Describe("Region Service Contract", func() {
	var (
		pact *consumer.V4HTTPMockProvider
		ctx  context.Context
	)

	BeforeEach(func() {
		var err error
		pact, err = contract.NewV4Pact(contract.PactConfig{
			Consumer: "uni-compute",
			Provider: "uni-region",
			PactDir:  "../pacts",
		})
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	Describe("GetRegions", func() {
		Context("when organization exists with regions", func() {
			It("returns list of regions", func() {
				organizationID := "test-org-123"

				// Define the expected interaction
				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "organization has regions",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"regionType":     "openstack",
						},
					}).
					UponReceiving("a request for regions (webhook test v2)").
					WithRequest("GET", fmt.Sprintf("/api/v1/organizations/%s/regions", organizationID)).
					WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody(matchers.EachLike(map[string]interface{}{
							"metadata": map[string]interface{}{
								"id":           matchers.UUID(),
								"name":         matchers.String("us-west-1"),
								"creationTime": matchers.Timestamp(),
							},
							"spec": map[string]interface{}{
								"type": matchers.String("openstack"),
							},
						}, 1))
					})

				// Execute the test
				test := func(config consumer.MockServerConfig) error {
					regionClient, err := createRegionClient(config)
					if err != nil {
						return fmt.Errorf("creating region client: %w", err)
					}

					client := regionclient.New(regionClient)
					regions, err := client.List(ctx, organizationID)
					if err != nil {
						return fmt.Errorf("listing regions: %w", err)
					}

					// Verify the response
					Expect(regions).NotTo(BeEmpty(), "Expected at least one region")
					Expect(regions[0].Metadata.Name).To(Equal("us-west-1"))
					Expect(regions[0].Spec.Type).To(Equal(regionapi.RegionTypeOpenstack))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when organization exists but has no regions", func() {
			It("returns empty list", func() {
				organizationID := "test-org-empty"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "organization has no regions",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
						},
					}).
					UponReceiving("a request for regions from organization with no regions").
					WithRequest("GET", fmt.Sprintf("/api/v1/organizations/%s/regions", organizationID)).
					WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody([]interface{}{})
					})

				test := func(config consumer.MockServerConfig) error {
					return testEmptyRegionsList(ctx, config, organizationID)
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when organization does not exist", func() {
			It("returns empty list (regions are global)", func() {
				organizationID := "nonexistent-org"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "organization does not exist",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
						},
					}).
					UponReceiving("a request for regions from nonexistent organization").
					WithRequest("GET", fmt.Sprintf("/api/v1/organizations/%s/regions", organizationID)).
					WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody([]interface{}{})
					})

				test := func(config consumer.MockServerConfig) error {
					return testEmptyRegionsList(ctx, config, organizationID)
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when client filters out Kubernetes regions", func() {
			It("only returns non-Kubernetes regions", func() {
				organizationID := "test-org-mixed"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "organization has mixed regions",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"regionType":     "mixed",
						},
					}).
					UponReceiving("a request for regions with mixed types").
					WithRequest("GET", fmt.Sprintf("/api/v1/organizations/%s/regions", organizationID)).
					WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
						// Note: The OpenAPI spec does not specify ordering for the regions list endpoint.
						// The provider may return regions in any order (e.g., as Kubernetes provides them).
						// This pact specifies a particular order for the mock server, but provider verification
						// should allow regions to be returned in any order as long as both types are present.
						// The consumer test verifies that the client correctly filters out Kubernetes regions
						// regardless of the order they are returned.
						b.JSONBody([]map[string]interface{}{
							{
								"metadata": map[string]interface{}{
									"id":           matchers.UUID(),
									"name":         matchers.String("openstack-region"),
									"creationTime": matchers.Timestamp(),
								},
								"spec": map[string]interface{}{
									"type": matchers.String("openstack"),
								},
							},
							{
								"metadata": map[string]interface{}{
									"id":           matchers.UUID(),
									"name":         matchers.String("k8s-region"),
									"creationTime": matchers.Timestamp(),
								},
								"spec": map[string]interface{}{
									"type": matchers.String("kubernetes"),
								},
							},
						})
					})

				test := func(config consumer.MockServerConfig) error {
					regionClient, err := createRegionClient(config)
					if err != nil {
						return fmt.Errorf("creating region client: %w", err)
					}

					client := regionclient.New(regionClient)
					regions, err := client.List(ctx, organizationID)
					if err != nil {
						return fmt.Errorf("listing regions: %w", err)
					}

					// The client should filter out kubernetes regions
					Expect(regions).To(HaveLen(1), "Should only have one region after filtering")
					Expect(regions[0].Metadata.Name).To(Equal("openstack-region"))
					Expect(regions[0].Spec.Type).To(Equal(regionapi.RegionTypeOpenstack))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})
	})
})
