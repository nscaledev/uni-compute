/*
Copyright 2024-2025 the Unikorn Authors.
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

//nolint:err113,revive // dynamic errors and naming conventions acceptable in test code
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/onsi/ginkgo/v2"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	coreclient "github.com/unikorn-cloud/core/pkg/testing/client"
	regionopenapi "github.com/unikorn-cloud/region/pkg/openapi"
)

// GinkgoLogger implements the Logger interface for Ginkgo tests.
type GinkgoLogger struct{}

func (g *GinkgoLogger) Printf(format string, args ...interface{}) {
	ginkgo.GinkgoWriter.Printf(format, args...)
}

// APIClient wraps the core API client with compute-specific methods.
type APIClient struct {
	*coreclient.APIClient
	config    *TestConfig
	endpoints *Endpoints
}

// NewAPIClient creates a new Compute API client.
func NewAPIClient(baseURL string) (*APIClient, error) {
	config, err := LoadTestConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load test configuration: %w", err)
	}

	if baseURL == "" {
		baseURL = config.BaseURL
	}

	return newAPIClientWithConfig(config, baseURL), nil
}

// NewAPIClientWithConfig creates a new Compute API client with the given config.
func NewAPIClientWithConfig(config *TestConfig) *APIClient {
	return newAPIClientWithConfig(config, config.BaseURL)
}

// common constructor logic.
func newAPIClientWithConfig(config *TestConfig, baseURL string) *APIClient {
	coreClient := coreclient.NewAPIClient(baseURL, config.AuthToken, config.RequestTimeout, &GinkgoLogger{})
	coreClient.SetLogRequests(config.LogRequests)
	coreClient.SetLogResponses(config.LogResponses)

	return &APIClient{
		APIClient: coreClient,
		config:    config,
		endpoints: NewEndpoints(),
	}
}

// listClusters is a typed helper for listing clusters.
func (c *APIClient) listClusters(ctx context.Context, path string, config coreclient.ResponseHandlerConfig) ([]openapi.ComputeClusterRead, error) {
	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", config.ResourceType, err)
	}

	return c.handleClusterListResponse(resp, respBody, config)
}

// handleClusterListResponse handles typed cluster list responses.
func (c *APIClient) handleClusterListResponse(resp *http.Response, respBody []byte, config coreclient.ResponseHandlerConfig) ([]openapi.ComputeClusterRead, error) {
	switch resp.StatusCode {
	case http.StatusOK:
		var clusters []openapi.ComputeClusterRead
		if err := json.Unmarshal(respBody, &clusters); err != nil {
			return nil, fmt.Errorf("unmarshaling %s response: %w", config.ResourceType, err)
		}

		return clusters, nil
	case http.StatusNotFound:
		if config.AllowNotFound {
			// Return empty list with error for test scenarios
			return []openapi.ComputeClusterRead{}, fmt.Errorf("%s '%s' not found (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
		}

		return nil, fmt.Errorf("%s '%s' not found (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
	case http.StatusForbidden:
		if config.AllowForbidden {
			// Return empty list with error for test scenarios
			return []openapi.ComputeClusterRead{}, fmt.Errorf("%s '%s' access denied (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
		}

		return nil, fmt.Errorf("%s '%s' access denied (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
	case http.StatusInternalServerError:
		// Server error - always return empty list and error for test scenarios
		return []openapi.ComputeClusterRead{}, fmt.Errorf("server error reading %s for %s '%s' (status: %d): %s", config.ResourceType, config.ResourceIDType, config.ResourceID, resp.StatusCode, string(respBody))
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func (c *APIClient) ListRegions(ctx context.Context, orgID string) (regionopenapi.Regions, error) {
	path := c.endpoints.ListRegions(orgID)
	config := coreclient.ResponseHandlerConfig{
		ResourceType:   "regions",
		ResourceID:     orgID,
		ResourceIDType: "organization",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return coreclient.ListResource[regionopenapi.RegionRead](ctx, c.APIClient, path, config)
}

func (c *APIClient) ListFlavors(ctx context.Context, orgID, regionID string) (regionopenapi.Flavors, error) {
	path := c.endpoints.ListFlavors(orgID, regionID)
	config := coreclient.ResponseHandlerConfig{
		ResourceType:   "flavors",
		ResourceID:     regionID,
		ResourceIDType: "region",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return coreclient.ListResource[regionopenapi.Flavor](ctx, c.APIClient, path, config)
}

func (c *APIClient) ListImages(ctx context.Context, orgID, regionID string) (regionopenapi.Images, error) {
	path := c.endpoints.ListImages(orgID, regionID)
	config := coreclient.ResponseHandlerConfig{
		ResourceType:   "images",
		ResourceID:     regionID,
		ResourceIDType: "region",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return coreclient.ListResource[regionopenapi.Image](ctx, c.APIClient, path, config)
}

// CreateCluster creates a new compute cluster.
// Accepts a typed struct for type safety, then converts to JSON for the request.
func (c *APIClient) CreateCluster(ctx context.Context, orgID, projectID string, body openapi.ComputeClusterWrite) (openapi.ComputeClusterRead, error) {
	path := c.endpoints.CreateCluster(orgID, projectID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return openapi.ComputeClusterRead{}, fmt.Errorf("marshaling cluster body: %w", err)
	}

	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodPost, path, strings.NewReader(string(bodyBytes)), 0)
	if err != nil {
		return openapi.ComputeClusterRead{}, fmt.Errorf("creating cluster: %w", err)
	}

	// Check for quota allocation errors (500 with specific message)
	if resp.StatusCode == http.StatusInternalServerError && strings.Contains(string(respBody), "failed to create quota allocation") {
		return openapi.ComputeClusterRead{}, fmt.Errorf("insufficient resources: failed to create quota allocation - environment may not have enough capacity (trace ID: %s)", coreclient.ExtractTraceID(resp.Header.Get("Traceparent")))
	}

	if resp.StatusCode != http.StatusAccepted {
		return openapi.ComputeClusterRead{}, fmt.Errorf("unexpected status code: expected %d, got %d, body: %s", http.StatusAccepted, resp.StatusCode, string(respBody))
	}

	var cluster openapi.ComputeClusterRead
	if err := json.Unmarshal(respBody, &cluster); err != nil {
		return openapi.ComputeClusterRead{}, fmt.Errorf("unmarshaling cluster response: %w", err)
	}

	return cluster, nil
}

// GetCluster retrieves a specific cluster.
// Im using this to poll with eventually to wait for the cluster to be provisioned.
func (c *APIClient) GetCluster(ctx context.Context, orgID, projectID, clusterID string) (openapi.ComputeClusterRead, error) {
	path := c.endpoints.GetCluster(orgID, projectID, clusterID)

	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return openapi.ComputeClusterRead{}, fmt.Errorf("getting cluster: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var cluster openapi.ComputeClusterRead
		if err := json.Unmarshal(respBody, &cluster); err != nil {
			return openapi.ComputeClusterRead{}, fmt.Errorf("unmarshaling cluster response: %w", err)
		}

		return cluster, nil
	case http.StatusNotFound:
		return openapi.ComputeClusterRead{}, fmt.Errorf("cluster '%s' not found (status: %d)", clusterID, resp.StatusCode)
	case http.StatusForbidden:
		return openapi.ComputeClusterRead{}, fmt.Errorf("cluster '%s' access denied (status: %d)", clusterID, resp.StatusCode)
	default:
		return openapi.ComputeClusterRead{}, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}
}

// ListClusters lists all clusters for a project.
func (c *APIClient) ListClusters(ctx context.Context, orgID, projectID string) ([]openapi.ComputeClusterRead, error) {
	path := c.endpoints.ListClusters(orgID, projectID)
	config := coreclient.ResponseHandlerConfig{
		ResourceType:   "clusters",
		ResourceID:     projectID,
		ResourceIDType: "project",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listClusters(ctx, path, config)
}

// ListOrganizationClusters lists all clusters for an organization across all projects.
func (c *APIClient) ListOrganizationClusters(ctx context.Context, orgID string) ([]openapi.ComputeClusterRead, error) {
	path := c.endpoints.ListOrganizationClusters(orgID)
	config := coreclient.ResponseHandlerConfig{
		ResourceType:   "clusters",
		ResourceID:     orgID,
		ResourceIDType: "organization",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listClusters(ctx, path, config)
}

// UpdateCluster updates an existing cluster.
// Accepts a typed struct for type safety, then converts to JSON for the request.
func (c *APIClient) UpdateCluster(ctx context.Context, orgID, projectID, clusterID string, body openapi.ComputeClusterWrite) error {
	path := c.endpoints.UpdateCluster(orgID, projectID, clusterID)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err = c.DoRequest(ctx, http.MethodPut, path, strings.NewReader(string(jsonBody)), http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("updating cluster: %w", err)
	}

	return nil
}

func (c *APIClient) DeleteCluster(ctx context.Context, orgID, projectID, clusterID string) error {
	path := c.endpoints.DeleteCluster(orgID, projectID, clusterID)

	//nolint:bodyclose // response body is closed in DoRequest
	resp, _, err := c.DoRequest(ctx, http.MethodDelete, path, nil, 0)
	if err != nil {
		return fmt.Errorf("deleting cluster: %w", err)
	}

	// Accept both 202 (deletion in progress) and 404 (already deleted/never existed)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// StartMachine starts a stopped machine.
func (c *APIClient) StartMachine(ctx context.Context, orgID, projectID, clusterID, machineID string) error {
	path := c.endpoints.StartMachine(orgID, projectID, clusterID, machineID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("starting machine: %w", err)
	}

	return nil
}

// StopMachine stops a running machine.
func (c *APIClient) StopMachine(ctx context.Context, orgID, projectID, clusterID, machineID string) error {
	path := c.endpoints.StopMachine(orgID, projectID, clusterID, machineID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("stopping machine: %w", err)
	}

	return nil
}

// SoftRebootMachine performs a graceful reboot of a machine.
func (c *APIClient) SoftRebootMachine(ctx context.Context, orgID, projectID, clusterID, machineID string) error {
	path := c.endpoints.SoftRebootMachine(orgID, projectID, clusterID, machineID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("soft rebooting machine: %w", err)
	}

	return nil
}

// HardRebootMachine performs a hard reboot of a machine.
func (c *APIClient) HardRebootMachine(ctx context.Context, orgID, projectID, clusterID, machineID string) error {
	path := c.endpoints.HardRebootMachine(orgID, projectID, clusterID, machineID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("hard rebooting machine: %w", err)
	}

	return nil
}

// EvictMachines evicts specified machines from a cluster.
// Uses typed struct for type safety, then converts to JSON for the request.
func (c *APIClient) EvictMachines(ctx context.Context, orgID, projectID, clusterID string, machineIDs []string) error {
	path := c.endpoints.EvictMachines(orgID, projectID, clusterID)

	body := openapi.EvictionWrite{
		MachineIDs: machineIDs,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling eviction request: %w", err)
	}

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err = c.DoRequest(ctx, http.MethodPost, path, strings.NewReader(string(bodyBytes)), http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("evicting machines: %w", err)
	}

	return nil
}

// QuotaInfo represents quota information for a resource.
type QuotaInfo struct {
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	Used        int    `json:"used"`
	Committed   int    `json:"committed"`
	Reserved    int    `json:"reserved"`
	Default     int    `json:"default"`
	Free        int    `json:"free"`
}

// QuotaResponse represents the quota API response structure.
type QuotaResponse struct {
	Quotas []QuotaInfo `json:"quotas"`
}

// CheckClusterQuota checks if there is sufficient cluster quota available.
func (c *APIClient) CheckClusterQuota(ctx context.Context, orgID string) error {
	// Build the quota URL using the identity base URL
	quotaURL := fmt.Sprintf("%s/api/v1/organizations/%s/quotas",
		strings.TrimSuffix(c.config.IdentityBaseURL, "/"), orgID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaURL, nil)
	if err != nil {
		return fmt.Errorf("creating quota request: %w", err)
	}

	// Add auth token
	if c.config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.AuthToken)
	}

	client := &http.Client{Timeout: c.config.RequestTimeout}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("checking quota: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("quota check failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading quota response: %w", err)
	}

	var response QuotaResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return fmt.Errorf("parsing quota response: %w", err)
	}

	// Find the clusters quota
	for _, quota := range response.Quotas {
		if quota.Kind == "clusters" {
			if quota.Free <= 0 {
				return fmt.Errorf("insufficient cluster quota: free=%d, used=%d, quantity=%d (need at least 1 free cluster)", quota.Free, quota.Used, quota.Quantity)
			}
			// Quota is sufficient
			ginkgo.GinkgoWriter.Printf("Cluster quota check passed: free=%d, used=%d, quantity=%d\n", quota.Free, quota.Used, quota.Quantity)

			return nil
		}
	}

	return fmt.Errorf("clusters quota not found in response")
}

// CreateInstance creates a new instance.
func (c *APIClient) CreateInstance(ctx context.Context, payload openapi.InstanceCreate) (openapi.InstanceRead, error) {
	path := c.endpoints.CreateInstance()

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return openapi.InstanceRead{}, fmt.Errorf("marshaling instance body: %w", err)
	}

	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodPost, path, strings.NewReader(string(bodyBytes)), 0)
	if err != nil {
		return openapi.InstanceRead{}, fmt.Errorf("creating instance: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return openapi.InstanceRead{}, fmt.Errorf("unexpected status code: expected %d, got %d, body: %s", http.StatusCreated, resp.StatusCode, string(respBody))
	}

	var instance openapi.InstanceRead
	if err := json.Unmarshal(respBody, &instance); err != nil {
		return openapi.InstanceRead{}, fmt.Errorf("unmarshaling instance response: %w", err)
	}

	return instance, nil
}

// GetInstance retrieves a specific instance.
func (c *APIClient) GetInstance(ctx context.Context, instanceID string) (openapi.InstanceRead, error) {
	path := c.endpoints.GetInstance(instanceID)

	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return openapi.InstanceRead{}, fmt.Errorf("getting instance: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var instance openapi.InstanceRead
		if err := json.Unmarshal(respBody, &instance); err != nil {
			return openapi.InstanceRead{}, fmt.Errorf("unmarshaling instance response: %w", err)
		}

		return instance, nil
	case http.StatusNotFound:
		return openapi.InstanceRead{}, fmt.Errorf("instance '%s' not found (status: %d)", instanceID, resp.StatusCode)
	case http.StatusForbidden:
		return openapi.InstanceRead{}, fmt.Errorf("instance '%s' access denied (status: %d)", instanceID, resp.StatusCode)
	default:
		return openapi.InstanceRead{}, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}
}

// DeleteInstance deletes an instance.
func (c *APIClient) DeleteInstance(ctx context.Context, instanceID string) error {
	path := c.endpoints.DeleteInstance(instanceID)

	//nolint:bodyclose // response body is closed in DoRequest
	resp, _, err := c.DoRequest(ctx, http.MethodDelete, path, nil, 0)
	if err != nil {
		return fmt.Errorf("deleting instance: %w", err)
	}

	// Accept both 202 (deletion in progress) and 404 (already deleted/never existed)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// GetInstanceConsoleOutput retrieves console output for an instance.
func (c *APIClient) GetInstanceConsoleOutput(ctx context.Context, instanceID string, length *int) (*regionopenapi.ConsoleOutputResponse, error) {
	path := c.endpoints.GetInstanceConsoleOutput(instanceID)

	if length != nil {
		u, err := url.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("parsing path: %w", err)
		}

		q := u.Query()
		q.Set("length", fmt.Sprintf("%d", *length))
		u.RawQuery = q.Encode()
		path = u.String()
	}

	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("getting console output for instance: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: expected %d, got %d, body: %s", http.StatusOK, resp.StatusCode, string(respBody))
	}

	var consoleOutput regionopenapi.ConsoleOutputResponse
	if err := json.Unmarshal(respBody, &consoleOutput); err != nil {
		return nil, fmt.Errorf("unmarshaling console output response: %w", err)
	}

	return &consoleOutput, nil
}
