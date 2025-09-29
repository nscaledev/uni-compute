package api

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ClusterPayloadBuilder builds cluster payloads for testing
type ClusterPayloadBuilder struct {
	payload map[string]interface{}
	config  *TestConfig
}

// NewClusterPayload creates a new cluster payload builder with defaults from config
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

// WithName sets the cluster name
func (b *ClusterPayloadBuilder) WithName(name string) *ClusterPayloadBuilder {
	metadata := b.payload["metadata"].(map[string]interface{})
	metadata["name"] = name
	return b
}

// WithDescription sets the cluster description
func (b *ClusterPayloadBuilder) WithDescription(desc string) *ClusterPayloadBuilder {
	metadata := b.payload["metadata"].(map[string]interface{})
	metadata["description"] = desc
	return b
}

// WithRegionID sets the region ID (pass empty string to omit)
func (b *ClusterPayloadBuilder) WithRegionID(regionID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})
	if regionID == "" {
		delete(spec, "regionId")
	} else {
		spec["regionId"] = regionID
	}
	return b
}

// WithFlavorID sets the flavor ID for all workload pools
func (b *ClusterPayloadBuilder) WithFlavorID(flavorID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})
	pools := spec["workloadPools"].([]map[string]interface{})

	for _, pool := range pools {
		machine := pool["machine"].(map[string]interface{})
		machine["flavorId"] = flavorID
	}

	return b
}

// WithImageID sets the image ID for all workload pools
func (b *ClusterPayloadBuilder) WithImageID(imageID string) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})
	pools := spec["workloadPools"].([]map[string]interface{})

	for _, pool := range pools {
		machine := pool["machine"].(map[string]interface{})
		image := machine["image"].(map[string]interface{})
		image["id"] = imageID
	}

	return b
}

// WithWorkloadPool adds a workload pool configuration
func (b *ClusterPayloadBuilder) WithWorkloadPool(name, flavorID, imageID string, replicas int) *ClusterPayloadBuilder {
	spec := b.payload["spec"].(map[string]interface{})
	pools := spec["workloadPools"].([]map[string]interface{})

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

// Build returns the completed cluster payload
func (b *ClusterPayloadBuilder) Build() map[string]interface{} {
	return b.payload
}

// CreateClusterWithCleanup creates a cluster, waits for provisioning, and schedules automatic cleanup
func CreateClusterWithCleanup(client *APIClient, ctx context.Context, config *TestConfig, payload map[string]interface{}) (map[string]interface{}, string) {
	cluster, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID, payload)
	if err != nil {
		panic(err)
	}

	metadata := cluster["metadata"].(map[string]interface{})
	clusterID := metadata["id"].(string)

	GinkgoWriter.Printf("Created cluster with ID: %s\n", clusterID)
	// Wait for cluster to be provisioned
	Eventually(func() string {
		GinkgoWriter.Printf("Calling GetCluster to check if cluster is provisioned yet, this can take up to %s\n", config.TestTimeout)
		updatedCluster, getErr := client.GetCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if getErr != nil {
			return "error"
		}
		if updatedCluster == nil {
			return "nil-cluster"
		}
		metadata, ok := updatedCluster["metadata"].(map[string]interface{})
		if !ok || metadata == nil {
			return "no-metadata"
		}
		provisioningStatus, ok := metadata["provisioningStatus"].(string)
		if !ok {
			return "no-provisioning-status"
		}
		return provisioningStatus
	}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(Equal("provisioned"))

	// Schedule cleanup - this runs whether the test passes or fails so we don't need to clean up manually
	DeferCleanup(func() {
		GinkgoWriter.Printf("Cleaning up cluster: %s\n", clusterID)
		deleteErr := client.DeleteCluster(ctx, config.OrgID, config.ProjectID, clusterID)
		if deleteErr != nil {
			GinkgoWriter.Printf("Warning: Failed to delete cluster %s: %v\n", clusterID, deleteErr)
		} else {
			GinkgoWriter.Printf("Successfully deleted cluster: %s\n", clusterID)
		}
	})

	return cluster, clusterID
}
