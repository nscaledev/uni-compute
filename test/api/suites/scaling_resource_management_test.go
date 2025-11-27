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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var _ = Describe("Scaling and Resource Management", func() {
	Context("When scaling workload pools", func() {
		Describe("Given a cluster with existing workload pools", func() {
			DescribeTable("should successfully scale workload pools",
				func(testName, poolName string, initialReplicas, targetReplicas int) {
					cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
						api.NewClusterPayload().
							WithName(testName).
							WithRegionID(config.RegionID).
							ClearWorkloadPools().
							WithWorkloadPool(poolName, config.FlavorID, config.ImageID, initialReplicas).
							BuildTyped())

					Expect(cluster.Metadata.Id).To(Equal(clusterID))
					api.VerifyPoolReplicas(&cluster, poolName, initialReplicas)

					updatedPayload := api.NewClusterPayload().
						WithName(testName).
						WithRegionID(config.RegionID).
						ClearWorkloadPools().
						WithWorkloadPool(poolName, config.FlavorID, config.ImageID, targetReplicas).
						BuildTyped()

					err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, clusterID, updatedPayload)
					Expect(err).NotTo(HaveOccurred())

					api.WaitForPoolReplicas(client, ctx, config, clusterID, poolName, targetReplicas)
					api.WaitForPoolMachinesActive(client, ctx, config, clusterID, poolName, targetReplicas)
				},
				Entry("scale up a workload pool", "scale-up-test", "scale-up-pool", 1, 2),
				Entry("scale down a workload pool", "scale-down-test", "scale-down-pool", 2, 1),
			)

			It("should handle scaling to zero machines", func() {
				poolName := "scale-to-zero-pool"
				initialReplicas := 1
				targetReplicas := 0

				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("scale-to-zero-test").
						WithRegionID(config.RegionID).
						ClearWorkloadPools().
						WithWorkloadPool(poolName, config.FlavorID, config.ImageID, initialReplicas).
						BuildTyped())

				Expect(cluster.Metadata.Id).To(Equal(clusterID))
				api.VerifyPoolReplicas(&cluster, poolName, initialReplicas)

				updatedPayload := api.NewClusterPayload().
					WithName("scale-to-zero-test").
					WithRegionID(config.RegionID).
					ClearWorkloadPools().
					WithWorkloadPool(poolName, config.FlavorID, config.ImageID, targetReplicas).
					BuildTyped()

				err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, clusterID, updatedPayload)
				Expect(err).NotTo(HaveOccurred())

				api.WaitForPoolReplicas(client, ctx, config, clusterID, poolName, targetReplicas)

				finalCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				pool := api.FindPoolByName(finalCluster.Spec.WorkloadPools, poolName)
				Expect(pool).NotTo(BeNil(), "Pool should still exist in spec after scaling to zero")
				Expect(pool.Machine.Replicas).To(Equal(0), "Pool should have 0 replicas configured")
			})
		})

		Describe("Given multiple workload pools", func() {
			DescribeTable("should scale multiple pools",
				func(testName, clusterName, pool1Name, pool2Name string, pool1Target, pool2Target int) {
					cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
						api.NewClusterPayload().
							WithName(clusterName).
							WithRegionID(config.RegionID).
							ClearWorkloadPools().
							WithWorkloadPool(pool1Name, config.FlavorID, config.ImageID, 1).
							WithWorkloadPool(pool2Name, config.FlavorID, config.ImageID, 1).
							BuildTyped())

					Expect(cluster.Metadata.Id).To(Equal(clusterID))

					updatedPayload := api.NewClusterPayload().
						WithName(clusterName).
						WithRegionID(config.RegionID).
						ClearWorkloadPools().
						WithWorkloadPool(pool1Name, config.FlavorID, config.ImageID, pool1Target).
						WithWorkloadPool(pool2Name, config.FlavorID, config.ImageID, pool2Target).
						BuildTyped()

					err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, clusterID, updatedPayload)
					Expect(err).NotTo(HaveOccurred())

					api.WaitForPoolReplicas(client, ctx, config, clusterID, pool1Name, pool1Target)
					api.WaitForPoolReplicas(client, ctx, config, clusterID, pool2Name, pool2Target)

					finalCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					Expect(err).NotTo(HaveOccurred())

					api.VerifyMultiplePoolsReplicas(&finalCluster, map[string]int{
						pool1Name: pool1Target,
						pool2Name: pool2Target,
					})
				},
				Entry("independently", "multi-pool-test", "multi-pool-test", "pool-1", "pool-2", 3, 2),
				Entry("concurrently with different pools", "concurrent-scale-test", "concurrent-scale-test", "concurrent-pool-1", "concurrent-pool-2", 2, 3),
			)
		})

		Describe("Given concurrent updates to the same cluster", func() {
			It("should handle concurrent workload pool updates", func() {
				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("concurrent-update-test").
						WithRegionID(config.RegionID).
						ClearWorkloadPools().
						WithWorkloadPool("pool-1", config.FlavorID, config.ImageID, 1).
						WithWorkloadPool("pool-2", config.FlavorID, config.ImageID, 1).
						BuildTyped())

				Expect(cluster.Metadata.Id).To(Equal(clusterID))

				updatedPayload := api.NewClusterPayload().
					WithName("concurrent-update-test").
					WithRegionID(config.RegionID).
					ClearWorkloadPools().
					WithWorkloadPool("pool-1", config.FlavorID, config.ImageID, 3).
					WithWorkloadPool("pool-3", config.FlavorID, config.ImageID, 4).
					BuildTyped()

				err := client.UpdateCluster(ctx, config.OrgID, config.ProjectID, clusterID, updatedPayload)
				Expect(err).NotTo(HaveOccurred())

				api.WaitForPoolReplicas(client, ctx, config, clusterID, "pool-1", 3)
				api.WaitForPoolReplicas(client, ctx, config, clusterID, "pool-3", 4)

				finalCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				api.VerifyMultiplePoolsReplicas(&finalCluster, map[string]int{
					"pool-1": 3,
					"pool-3": 4,
				})
			})
		})
	})

	Context("When creating large clusters", func() {
		Describe("Given a request for many workload pools", func() {
			It("should handle firewall configurations across pools", func() {
				pool1Name := "firewall-pool-1"
				pool2Name := "firewall-pool-2"

				cluster, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("firewall-test").
						WithRegionID(config.RegionID).
						ClearWorkloadPools().
						WithWorkloadPool(pool1Name, config.FlavorID, config.ImageID, 1).
						WithWorkloadPool(pool2Name, config.FlavorID, config.ImageID, 1).
						BuildTyped())

				Expect(cluster.Metadata.Id).To(Equal(clusterID))

				finalCluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
				Expect(err).NotTo(HaveOccurred())

				for _, poolName := range []string{pool1Name, pool2Name} {
					api.VerifyDefaultFirewallRule(&finalCluster, poolName)
				}
			})
		})
	})
})
