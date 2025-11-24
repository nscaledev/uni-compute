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

package region

import (
	"context"
	"net/http"
	"slices"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
)

// ClientGetterFunc allows us to lazily instantiate a client only when needed to
// avoid the TLS handshake and token exchange.
type ClientGetterFunc func(context.Context) (regionapi.ClientWithResponsesInterface, error)

// Client provides a caching layer for retrieval of region assets, and lazy population.
type Client struct {
	clientGetter ClientGetterFunc
}

// New returns a new client.
func New(clientGetter ClientGetterFunc) *Client {
	return &Client{
		clientGetter: clientGetter,
	}
}

func (c *Client) Client(ctx context.Context) (regionapi.ClientWithResponsesInterface, error) {
	regionAPIClient, err := c.clientGetter(ctx)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve region API client: %w", err).
			Prefixed()

		return nil, err
	}

	return regionAPIClient, nil
}

// List lists all regions.
func (c *Client) List(ctx context.Context, organizationID string) ([]regionapi.RegionRead, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDRegionsWithResponse(ctx, organizationID)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve regions: %w", err).
			Prefixed()

		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.Regions](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	regions := slices.DeleteFunc(*data, func(region regionapi.RegionRead) bool {
		return region.Spec.Type == regionapi.Kubernetes
	})

	return regions, nil
}

// Flavors returns all compute compatible flavors.
func (c *Client) Flavors(ctx context.Context, organizationID, regionID string) ([]regionapi.Flavor, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDRegionsRegionIDFlavorsWithResponse(ctx, organizationID, regionID)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve flavors: %w", err).
			Prefixed()

		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.Flavors](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	// TODO: filtering.
	return *data, nil
}

// Images returns all compute compatible images.
func (c *Client) Images(ctx context.Context, organizationID, regionID string) ([]regionapi.Image, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDRegionsRegionIDImagesWithResponse(ctx, organizationID, regionID)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve images: %w", err).
			Prefixed()

		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.Images](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	images := slices.DeleteFunc(*data, func(image regionapi.Image) bool {
		// Delete images that declare any software versions - if it doesn't exist, assume general purpose.
		return image.Spec.SoftwareVersions != nil && len(*image.Spec.SoftwareVersions) > 0
	})

	return images, nil
}

func (c *Client) Servers(ctx context.Context, organizationID string, cluster *unikornv1.ComputeCluster) ([]regionapi.ServerRead, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	params := &regionapi.GetApiV1OrganizationsOrganizationIDServersParams{
		Tag: util.ClusterTagSelector(cluster),
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDServersWithResponse(ctx, organizationID, params)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve servers: %w", err).
			Prefixed()

		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.ServersRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	return *data, nil
}

func (c *Client) DeleteServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return err
	}

	response, err := regionAPIClient.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to delete server: %w", err).
			Prefixed()
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}

func (c *Client) HardRebootServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDHardrebootWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to hard reboot server: %w", err).
			Prefixed()
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}

func (c *Client) SoftRebootServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDSoftrebootWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to soft reboot server: %w", err).
			Prefixed()
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}

func (c *Client) StartServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDStartWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to start server: %w", err).
			Prefixed()
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}

func (c *Client) StopServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDStopWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to stop server: %w", err).
			Prefixed()
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}

func (c *Client) CreateConsoleSession(ctx context.Context, organizationID, projectID, identityID, serverID string) (*regionapi.ConsoleSessionResponse, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsolesessionsWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to create console session: %w", err).
			Prefixed()

		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.ConsoleSession](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
}

func (c *Client) GetConsoleOutput(ctx context.Context, organizationID, projectID, identityID, serverID string, params *regionapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsoleoutputParams) (*regionapi.ConsoleOutputResponse, error) {
	regionAPIClient, err := c.Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsoleoutputWithResponse(ctx, organizationID, projectID, identityID, serverID, params)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve console output: %w", err).
			Prefixed()

		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.ConsoleOutput](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
}
