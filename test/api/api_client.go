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

//nolint:err113,revive // dynamic errors and naming conventions acceptable in test code
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
)

type APIClient struct {
	baseURL   string
	client    *http.Client
	authToken string
	config    *TestConfig
	endpoints *Endpoints
}

func NewAPIClient(baseURL string) *APIClient {
	config := LoadTestConfig()
	if baseURL == "" {
		baseURL = config.BaseURL
	}

	return newAPIClientWithConfig(config, baseURL)
}

func NewAPIClientWithConfig(config *TestConfig) *APIClient {
	return newAPIClientWithConfig(config, config.BaseURL)
}

// common constructor logic.
func newAPIClientWithConfig(config *TestConfig, baseURL string) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		authToken: config.AuthToken,
		config:    config,
		endpoints: NewEndpoints(),
	}
}

func (c *APIClient) SetAuthToken(token string) {
	c.authToken = token
}

// logError logs a generic error with trace context.
func (c *APIClient) logError(method, path string, duration time.Duration, traceParent string, err error, context string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] ERROR %s duration=%s traceparent=%s error=%v\n", method, path, context, duration, traceParent, err)
	c.logTraceContext(traceParent)
}

// logErrorWithStatus logs an error with HTTP status code.
func (c *APIClient) logErrorWithStatus(method, path string, duration time.Duration, statusCode int, traceParent string, err error, context string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] ERROR %s duration=%s status=%d traceparent=%s error=%v\n", method, path, context, duration, statusCode, traceParent, err)
	c.logTraceContext(traceParent)
}

// logUnexpectedStatus logs an unexpected HTTP status code.
func (c *APIClient) logUnexpectedStatus(method, path string, expectedStatus, actualStatus int, body, traceParent string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] UNEXPECTED STATUS expected=%d got=%d body=%s traceparent=%s\n", method, path, expectedStatus, actualStatus, body, traceParent)
	c.logTraceContext(traceParent)
}

// logTraceContext logs the trace context information.
func (c *APIClient) logTraceContext(traceParent string) {
	ginkgo.GinkgoWriter.Printf("TRACE CONTEXT: Use trace ID '%s' to search logs for this request\n", extractTraceID(traceParent))
}

// generateTraceID creates a new W3C trace ID.
// we are using this to create a new trace ID for each request so if an error occurs we can find the request in the logs.
func generateTraceID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)

	return hex.EncodeToString(bytes)
}

// generateSpanID creates a new W3C span ID.
func generateSpanID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)

	return hex.EncodeToString(bytes)
}

// createTraceParent creates a W3C traceparent header value.
func createTraceParent() string {
	traceID := generateTraceID()
	spanID := generateSpanID()

	return fmt.Sprintf("00-%s-%s-01", traceID, spanID)
}

// extractTraceID extracts the trace ID from a traceparent header value.
func extractTraceID(traceParent string) string {
	parts := strings.Split(traceParent, "-")
	if len(parts) >= 2 {
		return parts[1]
	}

	return traceParent
}

//nolint:cyclop // test code complexity is acceptable
func (c *APIClient) doRequest(ctx context.Context, method, path string, body io.Reader, expectedStatus int) (*http.Response, []byte, error) {
	fullURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	// Add W3C Trace Context headers
	traceParent := createTraceParent()
	req.Header.Set("Traceparent", traceParent)
	req.Header.Set("Tracestate", "test-automation=ginkgo")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		c.logError(method, path, duration, traceParent, err, "http request failed")
		return nil, nil, fmt.Errorf("http request failed: %w", err)
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logErrorWithStatus(method, path, duration, resp.StatusCode, traceParent, err, "reading response body")
		return resp, nil, fmt.Errorf("reading response body: %w", err)
	}

	if c.config.LogRequests {
		ginkgo.GinkgoWriter.Printf("[%s %s] status=%d duration=%s traceparent=%s\n", method, path, resp.StatusCode, duration, traceParent)
	}

	if c.config.LogResponses && len(respBody) > 0 {
		ginkgo.GinkgoWriter.Printf("[%s %s] response body: %s\n", method, path, string(respBody))
	}

	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		c.logUnexpectedStatus(method, path, expectedStatus, resp.StatusCode, string(respBody), traceParent)
		return resp, respBody, fmt.Errorf("unexpected status code: expected %d, got %d, body: %s (trace ID: %s)", expectedStatus, resp.StatusCode, string(respBody), extractTraceID(traceParent))
	}

	return resp, respBody, nil
}

// ResponseHandlerConfig configures how different status codes should be handled.
type ResponseHandlerConfig struct {
	ResourceType   string
	ResourceID     string
	ResourceIDType string
	AllowForbidden bool
	AllowNotFound  bool
}

// listResource is a generic helper for list operations.
func (c *APIClient) listResource(ctx context.Context, path string, config ResponseHandlerConfig) ([]map[string]interface{}, error) {
	//nolint:bodyclose // response body is closed in doRequest
	resp, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", config.ResourceType, err)
	}

	return c.handleResourceListResponse(resp, respBody, config)
}

// handleResourceListResponse handles common response patterns for resource listing endpoints.
func (c *APIClient) handleResourceListResponse(resp *http.Response, respBody []byte, config ResponseHandlerConfig) ([]map[string]interface{}, error) {
	switch resp.StatusCode {
	case http.StatusOK:
		var resources []map[string]interface{}
		if err := json.Unmarshal(respBody, &resources); err != nil {
			return nil, fmt.Errorf("unmarshaling %s response: %w", config.ResourceType, err)
		}

		return resources, nil
	case http.StatusNotFound:
		if config.AllowNotFound {
			// Return empty list with error for test scenarios (as sometimes we want to test the error case)
			return []map[string]interface{}{}, fmt.Errorf("%s '%s' not found (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
		}
		// Return error without empty list
		return nil, fmt.Errorf("%s '%s' not found (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
	case http.StatusForbidden:
		if config.AllowForbidden {
			// Return empty list with error for test scenarios
			return []map[string]interface{}{}, fmt.Errorf("%s '%s' access denied (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
		}
		// Return error without empty list
		return nil, fmt.Errorf("%s '%s' access denied (status: %d)", config.ResourceIDType, config.ResourceID, resp.StatusCode)
	case http.StatusInternalServerError:
		// Server error - always return empty list and error for test scenarios
		return []map[string]interface{}{}, fmt.Errorf("server error reading %s for %s '%s' (status: %d): %s", config.ResourceType, config.ResourceIDType, config.ResourceID, resp.StatusCode, string(respBody))
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func (c *APIClient) ListRegions(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListRegions(orgID)
	config := ResponseHandlerConfig{
		ResourceType:   "regions",
		ResourceID:     orgID,
		ResourceIDType: "organization",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listResource(ctx, path, config)
}

func (c *APIClient) ListFlavors(ctx context.Context, orgID, regionID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListFlavors(orgID, regionID)
	config := ResponseHandlerConfig{
		ResourceType:   "flavors",
		ResourceID:     regionID,
		ResourceIDType: "region",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listResource(ctx, path, config)
}

func (c *APIClient) ListImages(ctx context.Context, orgID, regionID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListImages(orgID, regionID)
	config := ResponseHandlerConfig{
		ResourceType:   "images",
		ResourceID:     regionID,
		ResourceIDType: "region",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listResource(ctx, path, config)
}

// CreateCluster creates a new compute cluster.
func (c *APIClient) CreateCluster(ctx context.Context, orgID, projectID string, body map[string]interface{}) (map[string]interface{}, error) {
	path := c.endpoints.CreateCluster(orgID, projectID)

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling cluster body: %w", err)
	}

	//nolint:bodyclose // response body is closed in doRequest
	_, respBody, err := c.doRequest(ctx, http.MethodPost, path, strings.NewReader(string(bodyBytes)), http.StatusAccepted)
	if err != nil {
		return nil, fmt.Errorf("creating cluster: %w", err)
	}

	var cluster map[string]interface{}
	if err := json.Unmarshal(respBody, &cluster); err != nil {
		return nil, fmt.Errorf("unmarshaling cluster response: %w", err)
	}

	return cluster, nil
}

// GetCluster retrieves a specific cluster.
// Im using this to poll with eventually to wait for the cluster to be provisioned.
func (c *APIClient) GetCluster(ctx context.Context, orgID, projectID, clusterID string) (map[string]interface{}, error) {
	path := c.endpoints.GetCluster(orgID, projectID, clusterID)

	//nolint:bodyclose // response body is closed in doRequest
	resp, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("getting cluster: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var cluster map[string]interface{}
		if err := json.Unmarshal(respBody, &cluster); err != nil {
			return nil, fmt.Errorf("unmarshaling cluster response: %w", err)
		}

		return cluster, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("cluster '%s' not found (status: %d)", clusterID, resp.StatusCode)
	case http.StatusForbidden:
		return nil, fmt.Errorf("cluster '%s' access denied (status: %d)", clusterID, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}
}

// ListClusters lists all clusters for a project.
func (c *APIClient) ListClusters(ctx context.Context, orgID, projectID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListClusters(orgID, projectID)
	config := ResponseHandlerConfig{
		ResourceType:   "clusters",
		ResourceID:     projectID,
		ResourceIDType: "project",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listResource(ctx, path, config)
}

// ListOrganizationClusters lists all clusters for an organization across all projects.
func (c *APIClient) ListOrganizationClusters(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListOrganizationClusters(orgID)
	config := ResponseHandlerConfig{
		ResourceType:   "clusters",
		ResourceID:     orgID,
		ResourceIDType: "organization",
		AllowForbidden: true,
		AllowNotFound:  true,
	}

	return c.listResource(ctx, path, config)
}

func (c *APIClient) UpdateCluster(ctx context.Context, orgID, projectID, clusterID string, body map[string]interface{}) error {
	path := c.endpoints.UpdateCluster(orgID, projectID, clusterID)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling request body: %w", err)
	}

	//nolint:bodyclose // response body is closed in doRequest
	_, _, err = c.doRequest(ctx, http.MethodPut, path, strings.NewReader(string(jsonBody)), http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("updating cluster: %w", err)
	}

	return nil
}

func (c *APIClient) DeleteCluster(ctx context.Context, orgID, projectID, clusterID string) error {
	path := c.endpoints.DeleteCluster(orgID, projectID, clusterID)

	//nolint:bodyclose // response body is closed in doRequest
	_, _, err := c.doRequest(ctx, http.MethodDelete, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("deleting cluster: %w", err)
	}

	return nil
}
