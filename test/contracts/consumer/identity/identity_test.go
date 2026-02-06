/*
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

package identity_test

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

	coreclient "github.com/unikorn-cloud/core/pkg/openapi"
	contract "github.com/unikorn-cloud/core/pkg/testing/contract"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
)

var testingT *testing.T //nolint:gochecknoglobals

func TestContracts(t *testing.T) { //nolint:paralleltest
	testingT = t

	RegisterFailHandler(Fail)
	RunSpecs(t, "Identity Consumer Contract Suite")
}

// createIdentityClient creates an identity client for the mock server.
func createIdentityClient(config consumer.MockServerConfig) (*identityapi.ClientWithResponses, error) {
	url := fmt.Sprintf("http://%s", net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port)))

	return identityapi.NewClientWithResponses(url)
}

var _ = Describe("Identity Service Contract", func() {
	var (
		pact *consumer.V4HTTPMockProvider
		ctx  context.Context
	)

	BeforeEach(func() {
		var err error
		pact, err = contract.NewV4Pact(contract.PactConfig{
			Consumer: "uni-compute",
			Provider: "uni-identity",
			PactDir:  "../pacts",
		})
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	Describe("ResourceAllocations", func() {
		Context("when creating a cluster allocation", func() {
			It("creates allocation for compute resources", func() {
				organizationID := "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f"
				projectID := "d4e5f6a7-b8c9-4d0e-1f2a-3b4c5d6e7f8a"
				allocationID := "e5f6a7b8-c9d0-4e1f-2a3b-4c5d6e7f8a9b"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "project exists",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"projectID":      projectID,
						},
					}).
					UponReceiving("a request to create cluster allocation").
					WithRequest("POST", fmt.Sprintf("/api/v1/organizations/%s/projects/%s/allocations", organizationID, projectID), func(b *consumer.V4RequestBuilder) {
						b.JSONBody(map[string]interface{}{
							"metadata": map[string]interface{}{
								"name": matchers.String("test-cluster"),
							},
							"spec": map[string]interface{}{
								"id":   matchers.String(allocationID),
								"kind": matchers.String("cluster"),
								"allocations": []map[string]interface{}{
									{
										"kind":      matchers.String("clusters"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
									{
										"kind":      matchers.String("servers"),
										"committed": matchers.Integer(3),
										"reserved":  matchers.Integer(3),
									},
									{
										"kind":      matchers.String("gpus"),
										"committed": matchers.Integer(2),
										"reserved":  matchers.Integer(2),
									},
								},
							},
						})
					}).
					WillRespondWith(201, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody(map[string]interface{}{
							"metadata": map[string]interface{}{
								"id":           matchers.UUID(),
								"name":         matchers.String("test-cluster"),
								"creationTime": matchers.Timestamp(),
							},
							"spec": map[string]interface{}{
								"id":   matchers.String(allocationID),
								"kind": matchers.String("cluster"),
								"allocations": []map[string]interface{}{
									{
										"kind":      matchers.String("clusters"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
									{
										"kind":      matchers.String("servers"),
										"committed": matchers.Integer(3),
										"reserved":  matchers.Integer(3),
									},
									{
										"kind":      matchers.String("gpus"),
										"committed": matchers.Integer(2),
										"reserved":  matchers.Integer(2),
									},
								},
							},
						})
					})

				test := func(config consumer.MockServerConfig) error {
					identityClient, err := createIdentityClient(config)
					if err != nil {
						return fmt.Errorf("creating identity client: %w", err)
					}

					// Create allocation request
					allocationReq := identityapi.AllocationWrite{
						Metadata: coreclient.ResourceWriteMetadata{
							Name: "test-cluster",
						},
						Spec: identityapi.AllocationSpec{
							Id:   allocationID,
							Kind: "cluster",
							Allocations: identityapi.ResourceAllocationList{
								{
									Kind:      "clusters",
									Committed: 1,
									Reserved:  1,
								},
								{
									Kind:      "servers",
									Committed: 3,
									Reserved:  3,
								},
								{
									Kind:      "gpus",
									Committed: 2,
									Reserved:  2,
								},
							},
						},
					}

					resp, err := identityClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsWithResponse(
						ctx, organizationID, projectID, allocationReq)
					if err != nil {
						return fmt.Errorf("creating allocation: %w", err)
					}

					// Verify the response
					Expect(resp.StatusCode()).To(Equal(201))
					Expect(resp.JSON201).NotTo(BeNil())
					Expect(resp.JSON201.Spec.Id).To(Equal(allocationID))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when updating an allocation", func() {
			It("updates allocation with new resource counts", func() {
				organizationID := "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f"
				projectID := "d4e5f6a7-b8c9-4d0e-1f2a-3b4c5d6e7f8a"
				allocationID := "e5f6a7b8-c9d0-4e1f-2a3b-4c5d6e7f8a9b"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "allocation exists",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"projectID":      projectID,
							"allocationID":   allocationID,
						},
					}).
					UponReceiving("a request to update cluster allocation").
					WithRequest("PUT",
						fmt.Sprintf("/api/v1/organizations/%s/projects/%s/allocations/%s",
							organizationID, projectID, allocationID), func(b *consumer.V4RequestBuilder) {
							b.JSONBody(map[string]interface{}{
								"metadata": map[string]interface{}{
									"name": matchers.String("test-cluster"),
								},
								"spec": map[string]interface{}{
									"id":   matchers.String(allocationID),
									"kind": matchers.String("cluster"),
									"allocations": []map[string]interface{}{
										{
											"kind":      matchers.String("clusters"),
											"committed": matchers.Integer(1),
											"reserved":  matchers.Integer(1),
										},
										{
											"kind":      matchers.String("servers"),
											"committed": matchers.Integer(5),
											"reserved":  matchers.Integer(5),
										},
										{
											"kind":      matchers.String("gpus"),
											"committed": matchers.Integer(4),
											"reserved":  matchers.Integer(4),
										},
									},
								},
							})
						}).
					WillRespondWith(200, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody(map[string]interface{}{
							"metadata": map[string]interface{}{
								"id":           matchers.UUID(),
								"name":         matchers.String("test-cluster"),
								"creationTime": matchers.Timestamp(),
							},
							"spec": map[string]interface{}{
								"id":   matchers.String(allocationID),
								"kind": matchers.String("cluster"),
								"allocations": []map[string]interface{}{
									{
										"kind":      matchers.String("clusters"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
									{
										"kind":      matchers.String("servers"),
										"committed": matchers.Integer(5),
										"reserved":  matchers.Integer(5),
									},
									{
										"kind":      matchers.String("gpus"),
										"committed": matchers.Integer(4),
										"reserved":  matchers.Integer(4),
									},
								},
							},
						})
					})

				test := func(config consumer.MockServerConfig) error {
					identityClient, err := createIdentityClient(config)
					if err != nil {
						return fmt.Errorf("creating identity client: %w", err)
					}

					// Update allocation request with scaled resources
					allocationReq := identityapi.AllocationWrite{
						Metadata: coreclient.ResourceWriteMetadata{
							Name: "test-cluster",
						},
						Spec: identityapi.AllocationSpec{
							Id:   allocationID,
							Kind: "cluster",
							Allocations: identityapi.ResourceAllocationList{
								{
									Kind:      "clusters",
									Committed: 1,
									Reserved:  1,
								},
								{
									Kind:      "servers",
									Committed: 5,
									Reserved:  5,
								},
								{
									Kind:      "gpus",
									Committed: 4,
									Reserved:  4,
								},
							},
						},
					}

					resp, err := identityClient.PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(
						ctx, organizationID, projectID, allocationID, allocationReq)
					if err != nil {
						return fmt.Errorf("updating allocation: %w", err)
					}

					// Verify the response
					Expect(resp.StatusCode()).To(Equal(200))
					Expect(resp.JSON200).NotTo(BeNil())
					Expect(resp.JSON200.Spec.Id).To(Equal(allocationID))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when deleting an allocation", func() {
			It("removes allocation successfully", func() {
				organizationID := "c3d4e5f6-a7b8-4c9d-0e1f-2a3b4c5d6e7f"
				projectID := "d4e5f6a7-b8c9-4d0e-1f2a-3b4c5d6e7f8a"
				allocationID := "e5f6a7b8-c9d0-4e1f-2a3b-4c5d6e7f8a9b"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "allocation exists",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"projectID":      projectID,
							"allocationID":   allocationID,
						},
					}).
					UponReceiving("a request to delete allocation").
					WithRequest("DELETE",
						fmt.Sprintf("/api/v1/organizations/%s/projects/%s/allocations/%s",
							organizationID, projectID, allocationID)).
					WillRespondWith(202)

				test := func(config consumer.MockServerConfig) error {
					identityClient, err := createIdentityClient(config)
					if err != nil {
						return fmt.Errorf("creating identity client: %w", err)
					}

					resp, err := identityClient.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(
						ctx, organizationID, projectID, allocationID)
					if err != nil {
						return fmt.Errorf("deleting allocation: %w", err)
					}

					// Verify the response
					Expect(resp.StatusCode()).To(Equal(202))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})

		Context("when creating an instance allocation", func() {
			It("creates allocation for instance with GPU", func() {
				organizationID := "f6a7b8c9-d0e1-4f2a-3b4c-5d6e7f8a9b0c"
				projectID := "a7b8c9d0-e1f2-4a3b-4c5d-6e7f8a9b0c1d"
				allocationID := "b8c9d0e1-f2a3-4b4c-5d6e-7f8a9b0c1d2e"

				pact.AddInteraction().
					GivenWithParameter(models.ProviderState{
						Name: "project exists",
						Parameters: map[string]interface{}{
							"organizationID": organizationID,
							"projectID":      projectID,
						},
					}).
					UponReceiving("a request to create instance allocation with GPU").
					WithRequest("POST", fmt.Sprintf("/api/v1/organizations/%s/projects/%s/allocations", organizationID, projectID), func(b *consumer.V4RequestBuilder) {
						b.JSONBody(map[string]interface{}{
							"metadata": map[string]interface{}{
								"name": matchers.String("test-instance"),
							},
							"spec": map[string]interface{}{
								"id":   matchers.String(allocationID),
								"kind": matchers.String("instance"),
								"allocations": []map[string]interface{}{
									{
										"kind":      matchers.String("servers"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
									{
										"kind":      matchers.String("gpus"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
								},
							},
						})
					}).
					WillRespondWith(201, func(b *consumer.V4ResponseBuilder) {
						b.JSONBody(map[string]interface{}{
							"metadata": map[string]interface{}{
								"id":           matchers.UUID(),
								"name":         matchers.String("test-instance"),
								"creationTime": matchers.Timestamp(),
							},
							"spec": map[string]interface{}{
								"id":   matchers.String(allocationID),
								"kind": matchers.String("instance"),
								"allocations": []map[string]interface{}{
									{
										"kind":      matchers.String("servers"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
									{
										"kind":      matchers.String("gpus"),
										"committed": matchers.Integer(1),
										"reserved":  matchers.Integer(1),
									},
								},
							},
						})
					})

				test := func(config consumer.MockServerConfig) error {
					identityClient, err := createIdentityClient(config)
					if err != nil {
						return fmt.Errorf("creating identity client: %w", err)
					}

					// Create instance allocation
					allocationReq := identityapi.AllocationWrite{
						Metadata: coreclient.ResourceWriteMetadata{
							Name: "test-instance",
						},
						Spec: identityapi.AllocationSpec{
							Id:   allocationID,
							Kind: "instance",
							Allocations: identityapi.ResourceAllocationList{
								{
									Kind:      "servers",
									Committed: 1,
									Reserved:  1,
								},
								{
									Kind:      "gpus",
									Committed: 1,
									Reserved:  1,
								},
							},
						},
					}

					resp, err := identityClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsWithResponse(
						ctx, organizationID, projectID, allocationReq)
					if err != nil {
						return fmt.Errorf("creating allocation: %w", err)
					}

					// Verify the response
					Expect(resp.StatusCode()).To(Equal(201))
					Expect(resp.JSON201).NotTo(BeNil())
					Expect(resp.JSON201.Spec.Id).To(Equal(allocationID))

					return nil
				}

				Expect(pact.ExecuteTest(testingT, test)).To(Succeed())
			})
		})
	})
})
