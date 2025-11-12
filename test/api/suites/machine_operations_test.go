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

//nolint:testpackage,revive // test package in suites is standard for these tests
package suites

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var _ = Describe("Machine Operations", func() {
	Context("When performing machine power operations", func() {
		Describe("Given a valid machine exists", func() {
			var (
				clusterID string
				machineID string
			)

			BeforeEach(func() {
				// Create a cluster with a single machine for power operation tests
				_, cID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("machine-power-ops-test").
						WithRegionID(config.RegionID).
						BuildTyped())

				clusterID = cID

				// Wait for machine to be available
				machineID = api.WaitForMachinesAvailable(client, ctx, config, clusterID)

				// Wait for machine to reach Running state
				api.WaitForMachineStatus(client, ctx, config, clusterID, machineID, "Running")

				GinkgoWriter.Printf("Using cluster %s with machine %s for power operations\n", clusterID, machineID)
			})

			It("should successfully stop a running machine", func() {
				GinkgoWriter.Printf("Stopping machine %s\n", machineID)
				err := client.StopMachine(ctx, config.OrgID, config.ProjectID, clusterID, machineID)
				Expect(err).NotTo(HaveOccurred())
				GinkgoWriter.Printf("Stop request accepted for machine %s\n", machineID)

				// Verify machine status transitions
				Eventually(func() string {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						GinkgoWriter.Printf("Error getting cluster: %v\n", getErr)
						return "error"
					}

					status := api.GetMachineStatus(cluster, machineID)
					GinkgoWriter.Printf("Machine %s current status: %s (waiting for Stopped)\n", machineID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Stopped"))
			})

			It("should successfully start a stopped machine", func() {
				// First stop the machine
				GinkgoWriter.Printf("Stopping machine %s\n", machineID)
				err := client.StopMachine(ctx, config.OrgID, config.ProjectID, clusterID, machineID)
				Expect(err).NotTo(HaveOccurred())

				// Wait for machine to be stopped
				Eventually(func() string {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						return "error"
					}

					status := api.GetMachineStatus(cluster, machineID)
					GinkgoWriter.Printf("Machine %s current status: %s (waiting for Stopped)\n", machineID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Stopped"))

				// Now start the machine
				GinkgoWriter.Printf("Starting machine %s\n", machineID)
				err = client.StartMachine(ctx, config.OrgID, config.ProjectID, clusterID, machineID)
				Expect(err).NotTo(HaveOccurred())

				// Verify machine returns to running state
				Eventually(func() string {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						return "error"
					}

					status := api.GetMachineStatus(cluster, machineID)
					GinkgoWriter.Printf("Machine %s current status: %s (waiting for Running)\n", machineID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})

			It("should successfully soft reboot a running machine", func() {
				GinkgoWriter.Printf("Soft rebooting machine %s\n", machineID)
				err := client.SoftRebootMachine(ctx, config.OrgID, config.ProjectID, clusterID, machineID)
				Expect(err).NotTo(HaveOccurred())

				// Verify machine eventually returns to active state after reboot
				Eventually(func() string {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						return "error"
					}

					status := api.GetMachineStatus(cluster, machineID)
					GinkgoWriter.Printf("Machine %s current status: %s (waiting for Running after reboot)\n", machineID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})

			It("should successfully hard reboot a running machine", func() {
				GinkgoWriter.Printf("Hard rebooting machine %s\n", machineID)
				err := client.HardRebootMachine(ctx, config.OrgID, config.ProjectID, clusterID, machineID)
				Expect(err).NotTo(HaveOccurred())

				// Verify machine eventually returns to active state after reboot
				Eventually(func() string {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						return "error"
					}

					status := api.GetMachineStatus(cluster, machineID)
					GinkgoWriter.Printf("Machine %s current status: %s (waiting for Running after reboot)\n", machineID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})
		})
	})

	Context("When evicting machines from a cluster", func() {
		Describe("Given valid eviction parameters", func() {
			It("should successfully evict specified machines", func() {
				// Create a cluster with multiple machines (2 replicas)
				_, clusterID := api.CreateClusterWithCleanup(client, ctx, config,
					api.NewClusterPayload().
						WithName("eviction-test").
						WithRegionID(config.RegionID).
						WithWorkloadPool("eviction-pool", config.FlavorID, config.ImageID, 2).
						BuildTyped())

				// Wait for machines to be available and extract machine IDs
				var machineIDs []string
				Eventually(func() bool {
					cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						GinkgoWriter.Printf("Error getting cluster: %v\n", getErr)
						return false
					}

					machineIDs = api.ExtractMachineIDsFromPool(cluster, "eviction-pool")
					GinkgoWriter.Printf("Found %d machines in eviction-pool (waiting until they are provisioned)\n", len(machineIDs))

					return len(machineIDs) >= 2
				}).WithTimeout(config.TestTimeout).WithPolling(10*time.Second).Should(BeTrue(), "cluster should have at least 2 machines")

				machineIDToEvict := machineIDs[0]
				GinkgoWriter.Printf("Evicting machine %s from cluster %s\n", machineIDToEvict, clusterID)

				// Evict one machine (this scales down from 2 to 1 replica)
				err := client.EvictMachines(ctx, config.OrgID, config.ProjectID, clusterID, []string{machineIDToEvict})
				Expect(err).NotTo(HaveOccurred())

				// Verify the machine is evicted and pool scales down to 1 replica
				Eventually(func() bool {
					updatedCluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
					if getErr != nil {
						return false
					}

					return api.VerifyMachineEvicted(updatedCluster, "eviction-pool", machineIDToEvict, 1)
				}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(BeTrue())
			})
		})
	})
})
