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
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ClusterPayloadBuilder builds cluster payloads for testing.
type ClusterPayloadBuilder struct {
	payload map[string]interface{}
	config  *TestConfig
}

// NewClusterPayload creates a new cluster payload builder with defaults from config.
func NewClusterPayload() *ClusterPayloadBuilder {
	config := LoadTestConfig()
	timestamp := time.Now().Format("20060102-150405")
	uniqueName := fmt.Sprintf("testautomationcreate-%s", timestamp)

	return &ClusterPayloadBuilder{
		config: config,
		payload: map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":        uniqueName,
				"description": "",
			},
			"spec": map[string]interface{}{
				"regionId": config.RegionID,
				"workloadPools": []map[string]interface{}{
					{
						"name": "test-pool",
						"machine": map[string]interface{}{
							"replicas": 1,
							"flavorId": config.FlavorID,
							"disk":     map[string]interface{}{"size": 30},
							"firewall": []map[string]interface{}{
								{
									"direction": "ingress",
									"protocol":  "tcp",
									"port":      22,
									"prefixes":  []string{"0.0.0.0/0"},
								},
							},
							"publicIPAllocation": map[string]interface{}{"enabled": true},
							"image": map[string]interface{}{
								"id": config.ImageID,
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
	metadata := b.payload["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure
	metadata["name"] = name

	return b
}

// WithDescription sets the cluster description.
func (b *ClusterPayloadBuilder) WithDescription(desc string) *ClusterPayloadBuilder {
	metadata := b.payload["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure
	metadata["description"] = desc

	return b
}

// WithProjectID overrides the default project ID for multi-project testing.
func (b *ClusterPayloadBuilder) WithProjectID(projectID string) *ClusterPayloadBuilder {
	b.config.ProjectID = projectID
	return b
}

// WithRegionID sets the region ID (pass empty string to omit).
func (b *ClusterPayloadBuilder) WithRegionID(regionID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure
	if regionID == "" {
		delete(spec, "regionId")
	} else {
		spec["regionId"] = regionID
	}

	return b
}

// WithFlavorID sets the flavor ID for all workload pools.
func (b *ClusterPayloadBuilder) WithFlavorID(flavorID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})        //nolint:forcetypeassert // safe: we control payload structure
	pools := spec["workloadPools"].([]map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure

	for _, pool := range pools {
		machine := pool["machine"].(map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure
		machine["flavorId"] = flavorID
	}

	return b
}

// WithImageID sets the image ID for all workload pools.
func (b *ClusterPayloadBuilder) WithImageID(imageID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})        //nolint:forcetypeassert // safe: we control payload structure
	pools := spec["workloadPools"].([]map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure

	for _, pool := range pools {
		machine := pool["machine"].(map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure
		image := machine["image"].(map[string]interface{})  //nolint:forcetypeassert // safe: we control payload structure
		image["id"] = imageID
	}

	return b
}

// WithWorkloadPool adds a workload pool configuration.
func (b *ClusterPayloadBuilder) WithWorkloadPool(name, flavorID, imageID string, replicas int) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})        //nolint:forcetypeassert // safe: we control payload structure
	pools := spec["workloadPools"].([]map[string]interface{}) //nolint:forcetypeassert // safe: we control payload structure

	pool := map[string]interface{}{
		"name": name,
		"machine": map[string]interface{}{
			"replicas": replicas,
			"flavorId": flavorID,
			"disk":     map[string]interface{}{"size": 30},
			"firewall": []map[string]interface{}{
				{
					"direction": "ingress",
					"protocol":  "tcp",
					"port":      22,
					"prefixes":  []string{"0.0.0.0/0"},
				},
			},
			"publicIPAllocation": map[string]interface{}{"enabled": true},
			"image": map[string]interface{}{
				"id": imageID,
			},
		},
	}

	pools = append(pools, pool)
	spec["workloadPools"] = pools

	return b
}

// Build returns the completed cluster payload.
func (b *ClusterPayloadBuilder) Build() map[string]interface{} {
	return b.payload
}

// CreateClusterWithCleanup creates a cluster, waits for provisioning, and schedules automatic cleanup.
func CreateClusterWithCleanup(client *APIClient, ctx context.Context, config *TestConfig, payload map[string]interface{}) (map[string]interface{}, string) {
	var clusterID string

	// Schedule cleanup FIRST - ensures cleanup runs even if creation/provisioning fails
	DeferCleanup(func() {
		if clusterID == "" {
			GinkgoWriter.Printf("Skipping cleanup: no cluster ID available\n")
			return
		}

		GinkgoWriter.Printf("Cleaning up cluster: %s\n", clusterID)

		deleteErr := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if deleteErr != nil {
			GinkgoWriter.Printf("Warning: Failed to delete cluster %s: %v\n", clusterID, deleteErr)
		} else {
			GinkgoWriter.Printf("Successfully deleted cluster: %s\n", clusterID)
		}
	})

	cluster, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID, payload)
	if err != nil {
		// Check if this is a quota allocation error (insufficient resources)
		if strings.Contains(err.Error(), "insufficient resources") || strings.Contains(err.Error(), "failed to create quota allocation") {
			Skip(fmt.Sprintf("Skipping test due to insufficient resources: %v", err))
		}

		panic(err)
	}

	metadata := cluster["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	clusterID = metadata["id"].(string)                      //nolint:forcetypeassert // safe: API response structure

	GinkgoWriter.Printf("Created cluster with ID: %s\n", clusterID)
	// Wait for cluster to be provisioned
	Eventually(func() string {
		updatedCluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if getErr != nil {
			return "error"
		}

		metadata := updatedCluster["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
		provisioningStatus := metadata["provisioningStatus"].(string)   //nolint:forcetypeassert // safe: API response structure

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
	Cluster   map[string]interface{}
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
func createClusterInProject(client *APIClient, ctx context.Context, config *TestConfig, projectID string, index int) (map[string]interface{}, string) {
	// Create a temporary config with the target project ID
	tempConfig := *config
	tempConfig.ProjectID = projectID

	// Use the standard CreateClusterWithCleanup which waits for provisioning
	cluster, clusterID := CreateClusterWithCleanup(client, ctx, &tempConfig,
		NewClusterPayload().
			WithName(fmt.Sprintf("org-list-test-project%d", index)).
			WithRegionID(config.RegionID).
			Build())

	return cluster, clusterID
}

// VerifyClusterPresence verifies that clusters are present in the list.
func VerifyClusterPresence(clusters []map[string]interface{}, expectedClusterIDs []string) {
	clusterIDs := extractClusterIDs(clusters)
	for _, expectedID := range expectedClusterIDs {
		Expect(clusterIDs).To(ContainElement(expectedID), "Expected cluster ID %s to be present in the list", expectedID)
	}
}

// VerifyProjectPresence verifies that projects are present in the cluster list.
func VerifyProjectPresence(clusters []map[string]interface{}, expectedProjectIDs []string) {
	projectIDs := extractProjectIDs(clusters)
	for _, expectedProjectID := range expectedProjectIDs {
		Expect(projectIDs).To(ContainElement(expectedProjectID), "Expected project ID %s to be present in the list", expectedProjectID)
	}
}

// extractFieldFromMetadata extracts a specific field from cluster metadata.
// Generic helper to reduce duplication across extractClusterIDs and extractProjectIDs.
func extractFieldFromMetadata(clusters []map[string]interface{}, fieldName string) []string {
	values := make([]string, len(clusters))

	for i, cluster := range clusters {
		metadata := cluster["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
		values[i] = metadata[fieldName].(string)                 //nolint:forcetypeassert // safe: API response structure
	}

	return values
}

// extractClusterIDs extracts cluster IDs from a list of cluster maps.
func extractClusterIDs(clusters []map[string]interface{}) []string {
	return extractFieldFromMetadata(clusters, "id")
}

// extractProjectIDs extracts project IDs from a list of cluster maps.
func extractProjectIDs(clusters []map[string]interface{}) []string {
	return extractFieldFromMetadata(clusters, "projectId")
}

// ClusterUpdateFixture represents a cluster setup for update testing.
type ClusterUpdateFixture struct {
	Cluster          map[string]interface{}
	ClusterID        string
	OriginalReplicas int
}

// CreateClusterUpdateFixture creates a cluster specifically for update testing.
func CreateClusterUpdateFixture(client *APIClient, ctx context.Context, config *TestConfig, clusterName string) *ClusterUpdateFixture {
	cluster, clusterID := CreateClusterWithCleanup(client, ctx, config,
		NewClusterPayload().
			WithName(clusterName).
			WithRegionID(config.RegionID).
			Build())

	// Extract original replicas count.
	spec := cluster["spec"].(map[string]interface{})         //nolint:forcetypeassert // safe: API response structure
	workloadPools := spec["workloadPools"].([]interface{})   //nolint:forcetypeassert // safe: API response structure
	firstPool := workloadPools[0].(map[string]interface{})   //nolint:forcetypeassert // safe: API response structure
	machine := firstPool["machine"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	originalReplicas := int(machine["replicas"].(float64))   //nolint:forcetypeassert // safe: API response structure

	return &ClusterUpdateFixture{
		Cluster:          cluster,
		ClusterID:        clusterID,
		OriginalReplicas: originalReplicas,
	}
}

// CreateUpdatePayload creates a cluster update payload with modified workload pools.
func (f *ClusterUpdateFixture) CreateUpdatePayload(config *TestConfig, newReplicas int) map[string]interface{} {
	return NewClusterPayload().
		WithName("update-test").
		WithRegionID(config.RegionID).
		WithWorkloadPool("test-pool", config.FlavorID, config.ImageID, newReplicas).
		Build()
}

// VerifyWorkloadPoolUpdate verifies that a cluster's workload pools were updated correctly.
func VerifyWorkloadPoolUpdate(cluster map[string]interface{}, expectedMinPools int) {
	Expect(cluster).To(HaveKey("spec"))
	spec := cluster["spec"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	Expect(spec).To(HaveKey("workloadPools"))

	workloadPools := spec["workloadPools"].([]interface{}) //nolint:forcetypeassert // safe: API response structure
	Expect(len(workloadPools)).To(BeNumerically(">=", expectedMinPools))
}

// GetMachineStatus retrieves the current status of a machine by ID from a cluster.
// Searches all workload pools for the machine.
func GetMachineStatus(cluster map[string]interface{}, machineID string) string {
	status := cluster["status"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	workloadPools := status["workloadPools"].([]interface{})

	// Search all pools for the machine
	for _, pool := range workloadPools {
		workloadPool := pool.(map[string]interface{})        //nolint:forcetypeassert // safe: API response structure
		machines := workloadPool["machines"].([]interface{}) //nolint:forcetypeassert // safe: API response structure

		for _, m := range machines {
			machine := m.(map[string]interface{})    //nolint:forcetypeassert // safe: API response structure
			if machine["id"].(string) == machineID { //nolint:forcetypeassert // safe: API response structure
				return machine["status"].(string) //nolint:forcetypeassert // safe: API response structure
			}
		}
	}

	return "not-found"
}

// ExtractMachineID extracts the first machine ID from a cluster's first workload pool.
func ExtractMachineID(cluster map[string]interface{}) string {
	status := cluster["status"].(map[string]interface{})     //nolint:forcetypeassert // safe: API response structure
	workloadPools := status["workloadPools"].([]interface{}) //nolint:forcetypeassert // safe: API response structure
	firstPool := workloadPools[0].(map[string]interface{})   //nolint:forcetypeassert // safe: API response structure
	machines := firstPool["machines"].([]interface{})        //nolint:forcetypeassert // safe: API response structure
	firstMachine := machines[0].(map[string]interface{})     //nolint:forcetypeassert // safe: API response structure

	return firstMachine["id"].(string) //nolint:forcetypeassert // safe: API response structure
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

		// Navigate through the nested structure using safe access
		status := cluster["status"].(map[string]interface{})     //nolint:forcetypeassert // safe: API response structure
		workloadPools := status["workloadPools"].([]interface{}) //nolint:forcetypeassert // safe: API response structure
		firstPool := workloadPools[0].(map[string]interface{})   //nolint:forcetypeassert // safe: API response structure
		machines := firstPool["machines"].([]interface{})        //nolint:forcetypeassert // safe: API response structure
		firstMachine := machines[0].(map[string]interface{})     //nolint:forcetypeassert // safe: API response structure
		mID := firstMachine["id"].(string)                       //nolint:forcetypeassert // safe: API response structure

		if mID == "" {
			return false
		}

		machineID = mID

		return true
	}).WithTimeout(config.TestTimeout).WithPolling(10*time.Second).Should(BeTrue(), "machines should be available in cluster")

	return machineID
}

// ExtractMachineIDsFromPool extracts machine IDs from a specific workload pool by name.
func ExtractMachineIDsFromPool(cluster map[string]interface{}, poolName string) []string {
	status := cluster["status"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	workloadPools := status["workloadPools"].([]interface{})

	GinkgoWriter.Printf("Searching for pool '%s' in %d total pools\n", poolName, len(workloadPools))

	for _, pool := range workloadPools {
		workloadPool := pool.(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
		name := workloadPool["name"].(string)         //nolint:forcetypeassert // safe: API response structure

		GinkgoWriter.Printf("Checking pool '%s'\n", name)

		if name == poolName {
			machines := workloadPool["machines"].([]interface{}) //nolint:forcetypeassert // safe: API response structure
			machineIDs := make([]string, 0, len(machines))

			for _, m := range machines {
				machine := m.(map[string]interface{})                   //nolint:forcetypeassert // safe: API response structure
				machineIDs = append(machineIDs, machine["id"].(string)) //nolint:forcetypeassert // safe: API response structure
			}

			GinkgoWriter.Printf("Found pool '%s' with %d machines\n", poolName, len(machineIDs))

			return machineIDs
		}
	}

	GinkgoWriter.Printf("Pool '%s' not found\n", poolName)

	return []string{}
}

// VerifyMachineEvicted checks that a machine ID is not present in a specific pool and the pool has the expected replica count.
func VerifyMachineEvicted(cluster map[string]interface{}, poolName, evictedMachineID string, expectedReplicas int) bool {
	status := cluster["status"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
	workloadPools := status["workloadPools"].([]interface{})

	for _, pool := range workloadPools {
		workloadPool := pool.(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
		name := workloadPool["name"].(string)         //nolint:forcetypeassert // safe: API response structure

		if name == poolName {
			machines := workloadPool["machines"].([]interface{}) //nolint:forcetypeassert // safe: API response structure

			for _, m := range machines {
				machine := m.(map[string]interface{})           //nolint:forcetypeassert // safe: API response structure
				if machine["id"].(string) == evictedMachineID { //nolint:forcetypeassert // safe: API response structure
					GinkgoWriter.Printf("Machine %s still present in pool %s (waiting for eviction)\n", evictedMachineID, poolName)
					return false
				}
			}

			if len(machines) != expectedReplicas {
				GinkgoWriter.Printf("Pool %s has %d machines (waiting for %d after eviction)\n", poolName, len(machines), expectedReplicas)
				return false
			}

			return true
		}
	}

	GinkgoWriter.Printf("Pool %s not found in cluster status\n", poolName)

	return false
}
