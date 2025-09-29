package suites

import (
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
						Build())

				Expect(cluster).To(HaveKey("metadata"))
				metadata := cluster["metadata"].(map[string]interface{})
				spec := cluster["spec"].(map[string]interface{})
				Expect(metadata).To(HaveKey("id"))
				Expect(metadata["id"]).NotTo(BeEmpty())
				Expect(metadata["id"]).To(Equal(clusterID))
				Expect(metadata["projectId"]).To(Equal(config.ProjectID))
				Expect(metadata["organizationId"]).To(Equal(config.OrgID))
				Expect(spec["regionId"]).To(Equal(config.RegionID))
			})
		})

		Describe("Given invalid cluster configuration", func() {
			It("should reject cluster creation with missing required fields", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(""). // Empty regionID to test missing required field
						Build())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("400"))
				Expect(err.Error()).To(ContainSubstring("invalid_request"))
			})
			//TODO: this is currently returning an ungraceful error, should be handled better, will update this test when that is fixed
			It("should reject cluster creation with invalid flavor", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(config.RegionID).
						WithFlavorID("invalid-flavor-id").
						Build())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("500"))
				Expect(err.Error()).To(ContainSubstring("unhandled error"))
			})

			It("should reject cluster creation with invalid image", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID(config.RegionID).
						WithImageID("invalid-image-id").
						Build())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("500"))
				Expect(err.Error()).To(ContainSubstring("unable to select an image"))
			})
			//TODO: this is currently returning an ungraceful error, should be handled better, will update this test when that is fixed
			It("should reject cluster creation with invalid region", func() {
				_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
					api.NewClusterPayload().
						WithRegionID("invalid-region-id").
						Build())

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("500"))
				Expect(err.Error()).To(ContainSubstring("unhandled error"))
			})
		})
	})

	Context("When listing compute clusters", func() {
		Describe("Given multiple clusters exist", func() {
			It("should return all clusters for the organization", func() {
				// Given: Multiple clusters exist across different projects
				// When: I request the cluster list for an organization
				// Then: All clusters for that organization should be returned
			})

			It("should include project information in cluster listings", func() {
				// Given: Clusters exist in multiple projects
				// When: I request the organization cluster list
				// Then: Each cluster should include its project ID in the response
			})
		})

		Describe("Given no clusters exist", func() {
			It("should return an empty list", func() {
				// Given: No clusters exist for the organization
				// When: I request the cluster list
				// Then: An empty list should be returned
				// And: No error should occur
			})
		})

		Describe("Given filtering and sorting", func() {
			It("should filter clusters by status", func() {
				// Given: Clusters in various states
				// When: I request clusters filtered by status
				// Then: Only clusters matching the filter should be returned
			})

			It("should filter clusters by project", func() {
				// Given: Clusters across multiple projects
				// When: I filter by specific project ID
				// Then: Only clusters from that project should be returned
			})
		})
	})

	Context("When retrieving a specific cluster", func() {
		Describe("Given the cluster exists", func() {
			It("should return complete cluster details", func() {
				// Given: A cluster exists with full configuration
				// When: I request the cluster details by ID
				// Then: The complete cluster configuration should be returned
				// And: All workload pools should be included
				// And: The current status should be accurate
			})
		})

		Describe("Given the cluster does not exist", func() {
			It("should return a not found error", func() {
				// Given: A cluster ID that does not exist
				// When: I request the cluster details
				// Then: A 404 not found error should be returned
			})
		})
	})

	Context("When updating a cluster", func() {
		Describe("Given valid update parameters", func() {
			It("should successfully update workload pools", func() {
				// Given: An existing cluster with workload pools
				// When: I update the workload pool configuration
				// Then: The cluster should be updated successfully
				// And: The new workload pool configuration should be applied
			})

			It("should successfully update authorized keys", func() {
				// Given: An existing cluster
				// When: I update the authorized SSH keys
				// Then: The keys should be updated successfully
				// And: New keys should be deployed to all machines
			})
		})

		Describe("Given invalid update parameters", func() {
			It("should reject updates to immutable fields", func() {
				// Given: An existing cluster
				// When: I attempt to update immutable configuration
				// Then: The update should be rejected
				// And: An appropriate error message should be returned
			})
		})
	})

	Context("When deleting a cluster", func() {
		Describe("Given the cluster exists and is deletable", func() {
			It("should successfully delete the cluster", func() {
				// Given: An existing cluster in a deletable state
				// When: I request cluster deletion
				// Then: The cluster should be marked for deletion
				// And: All associated resources should be cleaned up
			})
		})

		Describe("Given the cluster is in use", func() {
			It("should prevent deletion of clusters with running workloads", func() {
				// Given: A cluster with active workloads
				// When: I attempt to delete the cluster
				// Then: The deletion should be prevented
				// And: An error indicating active workloads should be returned
			})
		})
	})

	Context("When repeating API operations", func() {
		Describe("Given idempotent operations", func() {
			It("should handle duplicate cluster creation requests", func() {
				// Given: Identical cluster creation requests
				// When: I submit the same request multiple times
				// Then: Only one cluster should be created
				// And: Subsequent requests should return the existing cluster
			})

			It("should handle repeated delete operations", func() {
				// Given: A cluster that has been deleted
				// When: I attempt to delete it again
				// Then: The operation should be idempotent
				// And: No error should occur for already-deleted clusters
			})

			It("should handle update operations with same data", func() {
				// Given: A cluster with specific configuration
				// When: I update it with identical configuration
				// Then: No unnecessary changes should be made
				// And: The operation should complete successfully
			})
		})

		Describe("Given consistency requirements", func() {
			It("should maintain data consistency across repeated reads", func() {
				// Given: A cluster in stable state
				// When: I read cluster details multiple times
				// Then: Consistent data should be returned within reason
			})

			It("should handle concurrent identical requests", func() {
				// Given: Multiple identical requests submitted simultaneously
				// When: The system processes these requests
				// Then: Results should be consistent and correct
			})
		})
	})
})
