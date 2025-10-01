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
						Build())

				retrievedCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())
				Expect(retrievedCluster).To(HaveKey("metadata"))
				Expect(retrievedCluster).To(HaveKey("spec"))
				Expect(retrievedCluster).To(HaveKey("status"))

				metadata := retrievedCluster["metadata"].(map[string]interface{})
				spec := retrievedCluster["spec"].(map[string]interface{})

				Expect(metadata["id"]).To(Equal(clusterID))
				Expect(metadata["name"]).To(Equal("get-cluster-test"))
				Expect(metadata["projectId"]).To(Equal(config.ProjectID))
				Expect(metadata["organizationId"]).To(Equal(config.OrgID))
				Expect(spec["regionId"]).To(Equal(config.RegionID))
				Expect(spec).To(HaveKey("workloadPools"))

				workloadPools := spec["workloadPools"].([]interface{})
				Expect(len(workloadPools)).To(BeNumerically(">", 0))
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
		//TODO: this is currently returning an ungraceful error, should be handled better, will update this test when that is fixed
		Describe("Given invalid update parameters", func() {
			It("should reject updates to immutable fields", func() {
				invalidPayload := api.NewClusterPayload().
					WithName("immutable-test").
					WithRegionID(config.SecondaryRegionID). //todo: change this from a hardcoded value to a variable
					Build()

				err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, fixture.ClusterID, invalidPayload)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("server_error"))
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
						Build())

				Expect(cluster).To(HaveKey("metadata"))
				metadata := cluster["metadata"].(map[string]interface{})
				Expect(metadata["id"]).To(Equal(clusterID))

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
						Build())

				Expect(cluster).To(HaveKey("metadata"))

				err := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() error {
					_, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					return getErr
				}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(MatchError(ContainSubstring("404")))

				err = client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("500"))
				Expect(err.Error()).To(ContainSubstring("failed to get cluster"))
			})
		})
	})
})
