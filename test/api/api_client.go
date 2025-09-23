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

	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		config:    config,
		endpoints: NewEndpoints(),
	}
}

func NewAPIClientWithConfig(config *TestConfig) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(config.BaseURL, "/"),
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
		ginkgo.GinkgoWriter.Printf("[%s %s] ERROR duration=%s traceparent=%s error=%v\n", method, path, duration, traceParent, err)
		ginkgo.GinkgoWriter.Printf("TRACE CONTEXT: Use trace ID '%s' to search logs for this request\n", extractTraceID(traceParent))
		return nil, nil, fmt.Errorf("http request failed: %w", err)
	}

	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("[%s %s] ERROR reading body duration=%s status=%d traceparent=%s\n", method, path, duration, resp.StatusCode, traceParent)
		ginkgo.GinkgoWriter.Printf("TRACE CONTEXT: Use trace ID '%s' to search logs for this request\n", extractTraceID(traceParent))
		return resp, nil, fmt.Errorf("reading response body: %w", err)
	}

	if c.config.LogRequests {
		ginkgo.GinkgoWriter.Printf("[%s %s] status=%d duration=%s traceparent=%s\n", method, path, resp.StatusCode, duration, traceParent)
	}

	if c.config.LogResponses && len(respBody) > 0 {
		ginkgo.GinkgoWriter.Printf("[%s %s] response body: %s\n", method, path, string(respBody))
	}

	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		ginkgo.GinkgoWriter.Printf("[%s %s] UNEXPECTED STATUS expected=%d got=%d body=%s traceparent=%s\n", method, path, expectedStatus, resp.StatusCode, string(respBody), traceParent)
		ginkgo.GinkgoWriter.Printf("TRACE CONTEXT: Use trace ID '%s' to search logs for this failed request\n", extractTraceID(traceParent))
		return resp, respBody, fmt.Errorf("unexpected status code: expected %d, got %d, body: %s (trace ID: %s)", expectedStatus, resp.StatusCode, string(respBody), extractTraceID(traceParent))
	}

	return resp, respBody, nil
}

func (c *APIClient) ListRegions(ctx context.Context, orgID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListRegions(orgID)

	resp, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, 0) // Don't expect specific status
	if err != nil {
		return nil, fmt.Errorf("listing regions: %w", err)
	}

	// Handle different status codes appropriately
	switch resp.StatusCode {
	case http.StatusOK:
		var regions []map[string]interface{}
		if err := json.Unmarshal(respBody, &regions); err != nil {
			return nil, fmt.Errorf("unmarshaling regions response: %w", err)
		}
		return regions, nil
	case http.StatusForbidden, http.StatusNotFound:
		// Invalid organization ID - return empty list and error
		return []map[string]interface{}{}, fmt.Errorf("organization '%s' not found or access denied (status: %d)", orgID, resp.StatusCode)
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func (c *APIClient) ListFlavors(ctx context.Context, orgID, regionID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListFlavors(orgID, regionID)

	_, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("listing flavors: %w", err)
	}

	var flavors []map[string]interface{}
	if err := json.Unmarshal(respBody, &flavors); err != nil {
		return nil, fmt.Errorf("unmarshaling flavors response: %w", err)
	}

	return flavors, nil
}

func (c *APIClient) ListImages(ctx context.Context, orgID, regionID string) ([]map[string]interface{}, error) {
	path := c.endpoints.ListImages(orgID, regionID)

	_, respBody, err := c.doRequest(ctx, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("listing images: %w", err)
	}

	var images []map[string]interface{}
	if err := json.Unmarshal(respBody, &images); err != nil {
		return nil, fmt.Errorf("unmarshaling images response: %w", err)
	}

	return images, nil
}
