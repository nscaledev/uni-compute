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

//nolint:revive,staticcheck // dot imports are standard for Ginkgo/Gomega test code
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"

	"k8s.io/utils/ptr"
)

// ClusterPayloadBuilder builds cluster payloads for testing using type-safe OpenAPI structs.
type ClusterPayloadBuilder struct {
	cluster openapi.ComputeClusterWrite
	config  *TestConfig
}

// NewClusterPayload creates a new cluster payload builder with defaults from config.
func NewClusterPayload() *ClusterPayloadBuilder {
	config, err := LoadTestConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to load test configuration")

	timestamp := time.Now().Format("20060102-150405")
	uniqueName := fmt.Sprintf("testautomationcreate-%s", timestamp)

	return &ClusterPayloadBuilder{
		config: config,
		cluster: openapi.ComputeClusterWrite{
			Metadata: coreapi.ResourceWriteMetadata{
				Name:        uniqueName,
				Description: ptr.To(""),
			},
			Spec: openapi.ComputeClusterSpec{
				RegionId: config.RegionID,
				WorkloadPools: []openapi.ComputeClusterWorkloadPool{
					{
						Name: "test-pool",
						Machine: openapi.MachinePool{
							Replicas: 1,
							FlavorId: config.FlavorID,
							Disk: &openapi.Volume{
								Size: 30,
							},
							Firewall: &openapi.FirewallRules{
								{
									Direction: openapi.Ingress,
									Protocol:  openapi.Tcp,
									Port:      22,
									Prefixes:  []string{"0.0.0.0/0"},
								},
							},
							PublicIPAllocation: &openapi.PublicIPAllocation{
								Enabled: true,
							},
							Image: openapi.ComputeImage{
								Id: &config.ImageID,
							},
						},
					},
				},
			},
		},
	}
}

// WithName sets the cluster name.
func (b *ClusterPayloadBuilder) WithName(name string) *ClusterPayloadBuilder {
	b.cluster.Metadata.Name = name
	return b
}

// WithDescription sets the cluster description.
func (b *ClusterPayloadBuilder) WithDescription(desc string) *ClusterPayloadBuilder {
	b.cluster.Metadata.Description = ptr.To(desc)
	return b
}

// WithProjectID overrides the default project ID for multi-project testing.
func (b *ClusterPayloadBuilder) WithProjectID(projectID string) *ClusterPayloadBuilder {
	b.config.ProjectID = projectID
	return b
}

// WithRegionID sets the region ID.
func (b *ClusterPayloadBuilder) WithRegionID(regionID string) *ClusterPayloadBuilder {
	b.cluster.Spec.RegionId = regionID

	return b
}

// WithFlavorID sets the flavor ID for all workload pools.
func (b *ClusterPayloadBuilder) WithFlavorID(flavorID string) *ClusterPayloadBuilder {
	for i := range b.cluster.Spec.WorkloadPools {
		b.cluster.Spec.WorkloadPools[i].Machine.FlavorId = flavorID
	}

	return b
}

// WithImageID sets the image ID for all workload pools.
func (b *ClusterPayloadBuilder) WithImageID(imageID string) *ClusterPayloadBuilder {
	for i := range b.cluster.Spec.WorkloadPools {
		b.cluster.Spec.WorkloadPools[i].Machine.Image.Id = &imageID
	}

	return b
}

// WithWorkloadPool adds a workload pool configuration.
func (b *ClusterPayloadBuilder) WithWorkloadPool(name, flavorID, imageID string, replicas int) *ClusterPayloadBuilder {
	pool := openapi.ComputeClusterWorkloadPool{
		Name: name,
		Machine: openapi.MachinePool{
			Replicas: replicas,
			FlavorId: flavorID,
			Disk: &openapi.Volume{
				Size: 30,
			},
			Firewall: &openapi.FirewallRules{
				{
					Direction: openapi.Ingress,
					Protocol:  openapi.Tcp,
					Port:      22,
					Prefixes:  []string{"0.0.0.0/0"},
				},
			},
			PublicIPAllocation: &openapi.PublicIPAllocation{
				Enabled: true,
			},
			Image: openapi.ComputeImage{
				Id: &imageID,
			},
		},
	}

	b.cluster.Spec.WorkloadPools = append(b.cluster.Spec.WorkloadPools, pool)

	return b
}

// Build returns the cluster as a map for JSON marshaling (required for API client).
func (b *ClusterPayloadBuilder) Build() map[string]interface{} {
	jsonBytes, err := json.Marshal(b.cluster)
	Expect(err).NotTo(HaveOccurred(), "Failed to marshal cluster to JSON")

	var result map[string]interface{}
	err = json.Unmarshal(jsonBytes, &result)
	Expect(err).NotTo(HaveOccurred(), "Failed to unmarshal cluster JSON to map")

	return result
}

// BuildTyped returns the typed cluster struct directly.
func (b *ClusterPayloadBuilder) BuildTyped() openapi.ComputeClusterWrite {
	return b.cluster
}

// findOrphanedClusterID attempts to find a cluster by name when the ID wasn't captured during creation.
// Returns empty string if not found.
func findOrphanedClusterID(ctx context.Context, client *APIClient, config *TestConfig, clusterName string) string {
	GinkgoWriter.Printf("No cluster ID available, attempting to find cluster by name: %s\n", clusterName)

	clusters, listErr := client.ListOrganizationClusters(ctx, config.OrgID)
	if listErr != nil {
		GinkgoWriter.Printf("Warning: Could not list clusters for cleanup: %v\n", listErr)
		return ""
	}

	for _, cluster := range clusters {
		if cluster.Metadata.Name == clusterName {
			GinkgoWriter.Printf("Found orphaned cluster by name: %s (ID: %s)\n", clusterName, cluster.Metadata.Id)
			return cluster.Metadata.Id
		}
	}

	GinkgoWriter.Printf("Skipping cleanup: cluster not found by name\n")

	return ""
}

// CreateClusterWithCleanup creates a cluster, waits for provisioning, and schedules automatic cleanup.
// Accepts a typed struct for type safety (or use BuildTyped() from the builder).
func CreateClusterWithCleanup(client *APIClient, ctx context.Context, config *TestConfig, payload openapi.ComputeClusterWrite) (openapi.ComputeClusterRead, string) {
	var clusterID string

	clusterName := payload.Metadata.Name

	// Schedule cleanup FIRST - ensures cleanup runs even if creation/provisioning fails
	DeferCleanup(func() {
		if clusterID == "" {
			clusterID = findOrphanedClusterID(ctx, client, config, clusterName)
			if clusterID == "" {
				return
			}
		}

		GinkgoWriter.Printf("Cleaning up cluster: %s\n", clusterID)

		deleteErr := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if deleteErr != nil {
			GinkgoWriter.Printf("Warning: Failed to delete cluster %s: %v\n", clusterID, deleteErr)
		} else {
			GinkgoWriter.Printf("Successfully deleted cluster: %s\n", clusterID)
		}
	})

	// Check cluster quota before attempting creation
	GinkgoWriter.Printf("Checking cluster quota for organization %s\n", config.OrgID)

	if err := client.CheckClusterQuota(ctx, config.OrgID); err != nil {
		skipMsg := fmt.Sprintf("Skipping test due to insufficient cluster quota: %v", err)
		GinkgoWriter.Printf("QUOTA CONSTRAINT: %s\n", skipMsg)
		Skip(skipMsg)
	}

	cluster, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID, payload)
	if err != nil {
		// Check if this is a quota allocation error (insufficient resources)
		if strings.Contains(err.Error(), "insufficient resources") || strings.Contains(err.Error(), "failed to create quota allocation") {
			skipMsg := fmt.Sprintf("Skipping test due to insufficient resources: %v", err)
			GinkgoWriter.Printf("RESOURCE CONSTRAINT: %s\n", skipMsg)
			Skip(skipMsg)
		}

		Fail(fmt.Sprintf("Failed to create cluster: %v", err))
	}

	clusterID = cluster.Metadata.Id

	GinkgoWriter.Printf("Created cluster with ID: %s\n", clusterID)
	// Wait for cluster to be provisioned
	Eventually(func() string {
		updatedCluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if getErr != nil {
			return "error"
		}

		provisioningStatus := string(updatedCluster.Metadata.ProvisioningStatus)

		if provisioningStatus == "error" {
			Fail(fmt.Sprintf("Cluster %s entered error state during provisioning", clusterID))
		}

		return provisioningStatus
	}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(Equal("provisioned"))

	return cluster, clusterID
}

// MultiProjectClusterFixture represents clusters across multiple projects for testing.
type MultiProjectClusterFixture struct {
	Clusters []ClusterInfo
	Projects []string
}

// ClusterInfo holds cluster metadata and project information.
type ClusterInfo struct {
	Cluster   openapi.ComputeClusterRead
	ClusterID string
	ProjectID string
}

// CreateMultiProjectClusterFixture creates clusters in the specified projects for testing.
func CreateMultiProjectClusterFixture(client *APIClient, ctx context.Context, config *TestConfig, projectIDs []string) *MultiProjectClusterFixture {
	fixture := &MultiProjectClusterFixture{
		Clusters: make([]ClusterInfo, 0, len(projectIDs)),
		Projects: make([]string, 0, len(projectIDs)),
	}

	for i, projectID := range projectIDs {
		cluster, clusterID := createClusterInProject(client, ctx, config, projectID, i+1)
		fixture.Clusters = append(fixture.Clusters, ClusterInfo{
			Cluster:   cluster,
			ClusterID: clusterID,
			ProjectID: projectID,
		})
		fixture.Projects = append(fixture.Projects, projectID)
	}

	return fixture
}

// createClusterInProject creates a cluster in a specific project with cleanup.
// Now uses CreateClusterWithCleanup to ensure consistent behavior with proper provisioning wait.
func createClusterInProject(client *APIClient, ctx context.Context, config *TestConfig, projectID string, index int) (openapi.ComputeClusterRead, string) {
	// Create a temporary config with the target project ID
	tempConfig := *config
	tempConfig.ProjectID = projectID

	// Use the standard CreateClusterWithCleanup which waits for provisioning
	cluster, clusterID := CreateClusterWithCleanup(client, ctx, &tempConfig,
		NewClusterPayload().
			WithName(fmt.Sprintf("org-list-test-project%d", index)).
			WithRegionID(config.RegionID).
			BuildTyped())

	return cluster, clusterID
}

// VerifyClusterPresence verifies that clusters are present in the list.
func VerifyClusterPresence(clusters []openapi.ComputeClusterRead, expectedClusterIDs []string) {
	clusterIDs := extractClusterIDs(clusters)
	for _, expectedID := range expectedClusterIDs {
		Expect(clusterIDs).To(ContainElement(expectedID), "Expected cluster ID %s to be present in the list", expectedID)
	}
}

// VerifyProjectPresence verifies that projects are present in the cluster list.
func VerifyProjectPresence(clusters []openapi.ComputeClusterRead, expectedProjectIDs []string) {
	projectIDs := extractProjectIDs(clusters)
	for _, expectedProjectID := range expectedProjectIDs {
		Expect(projectIDs).To(ContainElement(expectedProjectID), "Expected project ID %s to be present in the list", expectedProjectID)
	}
}

// extractClusterIDs extracts cluster IDs from a list of typed clusters.
func extractClusterIDs(clusters []openapi.ComputeClusterRead) []string {
	ids := make([]string, len(clusters))
	for i, cluster := range clusters {
		ids[i] = cluster.Metadata.Id
	}

	return ids
}

// extractProjectIDs extracts project IDs from a list of typed clusters.
func extractProjectIDs(clusters []openapi.ComputeClusterRead) []string {
	ids := make([]string, len(clusters))
	for i, cluster := range clusters {
		ids[i] = cluster.Metadata.ProjectId
	}

	return ids
}

// ClusterUpdateFixture represents a cluster setup for update testing.
type ClusterUpdateFixture struct {
	Cluster          openapi.ComputeClusterRead
	ClusterID        string
	OriginalReplicas int
}

// CreateClusterUpdateFixture creates a cluster specifically for update testing.
func CreateClusterUpdateFixture(client *APIClient, ctx context.Context, config *TestConfig, clusterName string) *ClusterUpdateFixture {
	cluster, clusterID := CreateClusterWithCleanup(client, ctx, config,
		NewClusterPayload().
			WithName(clusterName).
			WithRegionID(config.RegionID).
			BuildTyped())

	// Extract original replicas count.
	originalReplicas := cluster.Spec.WorkloadPools[0].Machine.Replicas

	return &ClusterUpdateFixture{
		Cluster:          cluster,
		ClusterID:        clusterID,
		OriginalReplicas: originalReplicas,
	}
}

// CreateUpdatePayload creates a cluster update payload with modified workload pools.
// Returns typed struct for type safety.
func (f *ClusterUpdateFixture) CreateUpdatePayload(config *TestConfig, newReplicas int) openapi.ComputeClusterWrite {
	return NewClusterPayload().
		WithName("update-test").
		WithRegionID(config.RegionID).
		WithWorkloadPool("test-pool", config.FlavorID, config.ImageID, newReplicas).
		BuildTyped()
}

// VerifyWorkloadPoolUpdate verifies that a cluster's workload pools were updated correctly.
func VerifyWorkloadPoolUpdate(cluster openapi.ComputeClusterRead, expectedMinPools int) {
	Expect(len(cluster.Spec.WorkloadPools)).To(BeNumerically(">=", expectedMinPools))
}

// GetMachineStatus retrieves the current status of a machine by ID from a cluster.
// Searches all workload pools for the machine.
func GetMachineStatus(cluster openapi.ComputeClusterRead, machineID string) string {
	if cluster.Status == nil || cluster.Status.WorkloadPools == nil {
		return "not-found"
	}

	// Search all pools for the machine
	for _, pool := range *cluster.Status.WorkloadPools {
		if pool.Machines == nil {
			continue
		}

		for _, machine := range *pool.Machines {
			if machine.Id == machineID {
				return string(machine.Status)
			}
		}
	}

	return "not-found"
}

// ExtractMachineID extracts the first machine ID from a cluster's first workload pool.
func ExtractMachineID(cluster openapi.ComputeClusterRead) string {
	if cluster.Status == nil || cluster.Status.WorkloadPools == nil || len(*cluster.Status.WorkloadPools) == 0 {
		return ""
	}

	firstPool := (*cluster.Status.WorkloadPools)[0]
	if firstPool.Machines == nil || len(*firstPool.Machines) == 0 {
		return ""
	}

	return (*firstPool.Machines)[0].Id
}

// WaitForMachineStatus waits for a specific machine to reach the expected status.
func WaitForMachineStatus(client *APIClient, ctx context.Context, config *TestConfig, clusterID, machineID, expectedStatus string) {
	Eventually(func() string {
		cluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if getErr != nil {
			return "error"
		}

		status := GetMachineStatus(cluster, machineID)
		if status != expectedStatus {
			GinkgoWriter.Printf("Waiting for machine %s to become %s (current: %s)\n", machineID, expectedStatus, status)
		}

		return status
	}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(Equal(expectedStatus))
}

// WaitForMachinesAvailable waits for machines to be available in the cluster and returns the first machine ID.
func WaitForMachinesAvailable(client *APIClient, ctx context.Context, config *TestConfig, clusterID string) string {
	var machineID string

	Eventually(func() bool {
		cluster, err := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if err != nil {
			return false
		}

		// Check if we have machines available
		if cluster.Status == nil || cluster.Status.WorkloadPools == nil || len(*cluster.Status.WorkloadPools) == 0 {
			return false
		}

		firstPool := (*cluster.Status.WorkloadPools)[0]
		if firstPool.Machines == nil || len(*firstPool.Machines) == 0 {
			return false
		}

		mID := (*firstPool.Machines)[0].Id
		if mID == "" {
			return false
		}

		machineID = mID

		return true
	}).WithTimeout(config.TestTimeout).WithPolling(10*time.Second).Should(BeTrue(), "machines should be available in cluster")

	return machineID
}

// ExtractMachineIDsFromPool extracts machine IDs from a specific workload pool by name.
func ExtractMachineIDsFromPool(cluster openapi.ComputeClusterRead, poolName string) []string {
	if cluster.Status == nil || cluster.Status.WorkloadPools == nil {
		return []string{}
	}

	GinkgoWriter.Printf("Searching for pool '%s' in %d total pools\n", poolName, len(*cluster.Status.WorkloadPools))

	for _, pool := range *cluster.Status.WorkloadPools {
		GinkgoWriter.Printf("Checking pool '%s'\n", pool.Name)

		if pool.Name == poolName {
			if pool.Machines == nil {
				GinkgoWriter.Printf("Found pool '%s' with 0 machines\n", poolName)
				return []string{}
			}

			machineIDs := make([]string, 0, len(*pool.Machines))
			for _, machine := range *pool.Machines {
				machineIDs = append(machineIDs, machine.Id)
			}

			GinkgoWriter.Printf("Found pool '%s' with %d machines\n", poolName, len(machineIDs))

			return machineIDs
		}
	}

	GinkgoWriter.Printf("Pool '%s' not found\n", poolName)

	return []string{}
}

// VerifyMachineEvicted checks that a machine ID is not present in a specific pool and the pool has the expected replica count.
func VerifyMachineEvicted(cluster openapi.ComputeClusterRead, poolName, evictedMachineID string, expectedReplicas int) bool {
	if cluster.Status == nil || cluster.Status.WorkloadPools == nil {
		GinkgoWriter.Printf("Cluster status or workload pools not available\n")
		return false
	}

	for _, pool := range *cluster.Status.WorkloadPools {
		if pool.Name == poolName {
			if pool.Machines == nil {
				GinkgoWriter.Printf("Pool %s has no machines (waiting for %d after eviction)\n", poolName, expectedReplicas)
				return expectedReplicas == 0
			}

			// Check if evicted machine is still present
			for _, machine := range *pool.Machines {
				if machine.Id == evictedMachineID {
					GinkgoWriter.Printf("Machine %s still present in pool %s (waiting for eviction)\n", evictedMachineID, poolName)
					return false
				}
			}

			// Check replica count
			if len(*pool.Machines) != expectedReplicas {
				GinkgoWriter.Printf("Pool %s has %d machines (waiting for %d after eviction)\n", poolName, len(*pool.Machines), expectedReplicas)
				return false
			}

			return true
		}
	}

	GinkgoWriter.Printf("Pool %s not found in cluster status\n", poolName)

	return false
}
