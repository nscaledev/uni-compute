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

//nolint:testpackage,revive // test package in suites is standard for these tests, dot imports standard for Ginkgo
package suites

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var _ = Describe("Core Cluster Management", func() {
	Context("When creating a new compute cluster", func() {
		Describe("Given valid cluster configuration", func() {
			It("should successfully create the cluster", func() {
				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithRegionID(config.RegionID).
						BuildTyped())

				Expect(cluster.Metadata.Id).NotTo(BeEmpty())
				Expect(cluster.Metadata.Id).To(Equal(clusterID))
				Expect(cluster.Metadata.ProjectId).To(Equal(config.ProjectID))
				Expect(cluster.Metadata.OrganizationId).To(Equal(config.OrgID))
				Expect(cluster.Spec.RegionId).To(Equal(config.RegionID))
			})
		})

		Describe("Given invalid cluster configuration", func() {
			It("should reject cluster creation with missing required fields", func() {
				// Use typed struct with empty regionID to test missing required field validation
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(""). // Empty regionID to test missing required field
						BuildTyped())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
				Expect(err.Error()).To(ContainSubstring("region ID is invalid or cannot be resolved"))
			})
			It("should reject cluster creation with invalid flavor", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(config.RegionID).
						WithFlavorID("invalid-flavor-id").
						BuildTyped())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
			})

			It("should reject cluster creation with invalid image", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(config.RegionID).
						WithImageID("invalid-image-id").
						BuildTyped())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
			})
			It("should reject cluster creation with invalid region", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID("invalid-region-id").
						BuildTyped())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
			})
		})
	})

	Context("When listing compute clusters", func() {
		Describe("Given multiple clusters exist", func() {
			var fixture *api.MultiProjectClusterFixture

			BeforeEach(func() {
				fixture = api.CreateMultiProjectClusterFixture(client, ctx, config, []string{
					config.ProjectID,
					config.SecondaryProjectID,
				})
			})

			It("should return all clusters for the organization", func() {
				clusters, err := client.ListOrganizationClusters(ctx, config.OrgID)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(clusters)).To(BeNumerically(">=", 2))
				expectedClusterIDs := make([]string, len(fixture.Clusters))
				expectedProjectIDs := make([]string, len(fixture.Clusters))
				for i, cluster := range fixture.Clusters {
					expectedClusterIDs[i] = cluster.ClusterID
					expectedProjectIDs[i] = cluster.ProjectID
				}
				api.VerifyClusterPresence(clusters, expectedClusterIDs)
				api.VerifyProjectPresence(clusters, expectedProjectIDs)
			})
		})

	})

	Context("When retrieving a specific cluster", func() {
		Describe("Given the cluster exists", func() {
			It("should return complete cluster details", func() {
				_, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("get-cluster-test").
						WithRegionID(config.RegionID).
						BuildTyped())

				retrievedCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				Expect(retrievedCluster.Metadata.Id).To(Equal(clusterID))
				Expect(retrievedCluster.Metadata.Name).To(Equal("get-cluster-test"))
				Expect(retrievedCluster.Metadata.ProjectId).To(Equal(config.ProjectID))
				Expect(retrievedCluster.Metadata.OrganizationId).To(Equal(config.OrgID))
				Expect(retrievedCluster.Spec.RegionId).To(Equal(config.RegionID))
				Expect(retrievedCluster.Spec.WorkloadPools).ToNot(BeEmpty())
				Expect(retrievedCluster.Status).NotTo(BeNil())
			})
		})

		Describe("Given the cluster does not exist", func() {
			It("should return a not found error", func() {
				_, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, "non-existent-cluster-id")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("404"))
			})
		})
	})

	Context("When updating a cluster", func() {
		var fixture *api.ClusterUpdateFixture

		BeforeEach(func() {
			fixture = api.CreateClusterUpdateFixture(client, ctx, config, "update-test")
		})

		Describe("Given valid update parameters", func() {
			It("should successfully update workload pools", func() {
				updatedPayload := fixture.CreateUpdatePayload(config, fixture.OriginalReplicas+1)
				err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, fixture.ClusterID, updatedPayload)
				Expect(err).NotTo(HaveOccurred())

				updatedCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, fixture.ClusterID)
				Expect(err).NotTo(HaveOccurred())
				api.VerifyWorkloadPoolUpdate(updatedCluster, 1)
			})
		})
		Describe("Given invalid update parameters", func() {
			It("should reject updates to immutable fields", func() {
				invalidPayload := api.NewClusterPayload().
					WithName("immutable-test").
					WithRegionID(config.SecondaryRegionID).
					BuildTyped()

				err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, fixture.ClusterID, invalidPayload)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
				Expect(err.Error()).To(ContainSubstring("region ID is invalid or cannot be resolved"))
			})
		})
	})

	Context("When deleting a cluster", func() {
		Describe("Given the cluster exists and is deletable", func() {
			It("should successfully delete the cluster", func() {
				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("delete-cluster-test").
						WithRegionID(config.RegionID).
						BuildTyped())

				Expect(cluster.Metadata.Id).To(Equal(clusterID))

				err := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() error {
					_, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					return getErr
				}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(MatchError(ContainSubstring("404")))
			})
		})

	})

	Context("When repeating API operations", func() {
		Describe("Given idempotent operations", func() {

			It("should handle repeated delete operations", func() {
				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("repeated-delete-test").
						WithRegionID(config.RegionID).
						BuildTyped())

				Expect(cluster.Metadata).NotTo(BeNil())

				err := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() error {
					_, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					return getErr
				}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(MatchError(ContainSubstring("404")))

				// Repeated delete should be idempotent - no error (accepts 404)
				err = client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred(), "Repeated delete should be idempotent and not return an error")
			})
		})
	})
})
