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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// StopInstance stops a running instance.
func (c *APIClient) StopInstance(ctx context.Context, instanceID string) error {
	path := c.endpoints.StopInstance(instanceID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("stopping instance: %w", err)
	}

	return nil
}

// StartInstance starts a stopped instance.
func (c *APIClient) StartInstance(ctx context.Context, instanceID string) error {
	path := c.endpoints.StartInstance(instanceID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("starting instance: %w", err)
	}

	return nil
}

// RebootInstance reboots an instance. Set hard to true for a hard power cycle.
func (c *APIClient) RebootInstance(ctx context.Context, instanceID string, hard bool) error {
	path := c.endpoints.RebootInstance(instanceID)
	if hard {
		path += "?hard=true"
	}

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodPost, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("rebooting instance: %w", err)
	}

	return nil
}

func (c *APIClient) SnapshotInstance(ctx context.Context, instanceID string, name string) (*regionopenapi.Image, error) {
	path := c.endpoints.GetInstanceSnapshot(instanceID)

	body := fmt.Sprintf(`{
		"metadata": {"name": %q}
	}`, name)

	//nolint:bodyclose // response body is closed in DoRequest
	_, respBody, err := c.DoRequest(ctx, http.MethodPost, path, bytes.NewBufferString(body), http.StatusCreated)
	if err != nil {
		return nil, fmt.Errorf("requesting snapshot for instance: %w", err)
	}

	var image regionopenapi.Image
	if err := json.Unmarshal(respBody, &image); err != nil {
		return nil, fmt.Errorf("unmarshaling snapshot response: %w", err)
	}

	return &image, nil
}

// APIClient wraps the core API client with compute-specific methods.
type RegionAPIClient struct {
	*coreclient.APIClient
	config    *TestConfig
	endpoints *RegionEndpoints
}

func NewRegionClient(baseURL string) (*RegionAPIClient, error) {
	config, err := LoadTestConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load test configuration: %w", err)
	}

	if baseURL == "" {
		baseURL = config.RegionBaseURL
	}

	return newRegionAPIClientWithConfig(baseURL, config), nil
}

func newRegionAPIClientWithConfig(baseURL string, config *TestConfig) *RegionAPIClient {
	coreClient := coreclient.NewAPIClient(baseURL, config.AuthToken, config.RequestTimeout, &GinkgoLogger{})
	coreClient.SetLogRequests(config.LogRequests)
	coreClient.SetLogResponses(config.LogResponses)

	return &RegionAPIClient{
		APIClient: coreClient,
		config:    config,
		endpoints: &RegionEndpoints{},
	}
}

func (c *RegionAPIClient) CreateImage(ctx context.Context, organizationID, regionID string, request regionopenapi.ImageCreate) (*regionopenapi.Image, error) {
	path := c.endpoints.ListImages(organizationID, regionID)

	reqBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling image request: %w", err)
	}

	//nolint:bodyclose // response body is closed in DoRequest
	_, respBody, err := c.DoRequest(ctx, http.MethodPost, path, bytes.NewReader(reqBody), http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("creating image: %w", err)
	}

	var image regionopenapi.Image
	if err := json.Unmarshal(respBody, &image); err != nil {
		return nil, fmt.Errorf("unmarshaling image: %w", err)
	}

	return &image, nil
}

func (c *RegionAPIClient) ListImages(ctx context.Context, organizationID, regionID string) ([]regionopenapi.Image, error) {
	path := c.endpoints.ListImages(organizationID, regionID)

	return coreclient.ListResource[regionopenapi.Image](
		ctx,
		c.APIClient,
		path,
		coreclient.ResponseHandlerConfig{
			ResourceType:   "images",
			ResourceID:     regionID,
			ResourceIDType: "region",
		},
	)
}

func (c *RegionAPIClient) DeleteImage(ctx context.Context, organizationID, regionID, imageID string) error {
	path := c.endpoints.DeleteImage(organizationID, regionID, imageID)

	//nolint:bodyclose // response body is closed in DoRequest
	_, _, err := c.DoRequest(ctx, http.MethodDelete, path, nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("deleting image (ID: %s): %w", imageID, err)
	}

	return nil
}

type RegionEndpoints struct{}

func (*RegionEndpoints) ListImages(organizationID, regionID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/regions/%s/images", organizationID, regionID)
}

func (*RegionEndpoints) DeleteImage(organizationID, regionID, imageID string) string {
	return fmt.Sprintf("/api/v1/organizations/%s/regions/%s/images/%s", organizationID, regionID, imageID)
}
