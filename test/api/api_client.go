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

// newAPIClientWithConfig is the common constructor logic
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

// logError logs a generic error with trace context
func (c *APIClient) logError(method, path string, duration time.Duration, traceParent string, err error, context string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] ERROR %s duration=%s traceparent=%s error=%v\n", method, path, context, duration, traceParent, err)
	c.logTraceContext(traceParent)
}

// logErrorWithStatus logs an error with HTTP status code
func (c *APIClient) logErrorWithStatus(method, path string, duration time.Duration, statusCode int, traceParent string, err error, context string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] ERROR %s duration=%s status=%d traceparent=%s error=%v\n", method, path, context, duration, statusCode, traceParent, err)
	c.logTraceContext(traceParent)
}

// logUnexpectedStatus logs an unexpected HTTP status code
func (c *APIClient) logUnexpectedStatus(method, path string, expectedStatus, actualStatus int, body, traceParent string) {
	ginkgo.GinkgoWriter.Printf("[%s %s] UNEXPECTED STATUS expected=%d got=%d body=%s traceparent=%s\n", method, path, expectedStatus, actualStatus, body, traceParent)
	c.logTraceContext(traceParent)
}

// logTraceContext logs the trace context information
func (c *APIClient) logTraceContext(traceParent string) {
	ginkgo.GinkgoWriter.Printf("TRACE CONTEXT: Use trace ID '%s' to search logs for this request\n", extractTraceID(traceParent))
}

// generateTraceID creates a new W3C trace ID (32 hex characters)
// we are using this to create a new trace ID for each request so if an error occurs we can find the request in the logs
func generateTraceID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// generateSpanID creates a new W3C span ID (16 hex characters)
func generateSpanID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// createTraceParent creates a W3C traceparent header value
func createTraceParent() string {
	traceID := generateTraceID()
	spanID := generateSpanID()
	return fmt.Sprintf("00-%s-%s-01", traceID, spanID)
}

// extractTraceID extracts the trace ID from a traceparent header value
func extractTraceID(traceParent string) string {
	parts := strings.Split(traceParent, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return traceParent
}

func (c *APIClient) doRequest(ctx context.Context, method, path string, body io.Reader, expectedStatus int) (*http.Response, []byte, error) {
	fullURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	// Add W3C Trace Context headers
	traceParent := createTraceParent()
	req.Header.Set("traceparent", traceParent)
	req.Header.Set("tracestate", "test-automation=ginkgo")

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

// ResponseHandlerConfig configures how different status codes should be handled
type ResponseHandlerConfig struct {
	ResourceType   string
	ResourceID     string
	ResourceIDType string
	AllowForbidden bool
	AllowNotFound  bool
}

// ListResourceConfig defines configuration for list resource operations
type ListResourceConfig struct {
	ResourceType   string
	ResourceID     string
	ResourceIDType string
	AllowForbidden bool
	AllowNotFound  bool
}

// createResponseHandlerConfig creates a ResponseHandlerConfig from ListResourceConfig
func (c *APIClient) createResponseHandlerConfig(config ListResourceConfig) ResponseHandlerConfig {
	return ResponseHandlerConfig(config)
}

// listResource is a generic helper for list operations
func (c *APIClient) listResource(ctx context.Context, path string, config ListResourceConfig) ([]map[string]interface{}, error) {
	resp, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", config.ResourceType, err)
	}

	return c.handleResourceListResponse(resp, respBody, c.createResponseHandlerConfig(config))
}

// handleResourceListResponse handles common response patterns for resource listing endpoints
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
			// Return empty list with error for test scenarios
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
	config := ListResourceConfig{
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
	config := ListResourceConfig{
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
	config := ListResourceConfig{
		ResourceType:   "images",
		ResourceID:     regionID,
		ResourceIDType: "region",
		AllowForbidden: true,
		AllowNotFound:  true,
	}
	return c.listResource(ctx, path, config)
}
