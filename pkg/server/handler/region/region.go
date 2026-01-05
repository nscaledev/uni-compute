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

package region

import (
	"context"
	"net/http"
	"slices"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	coreapiutils "github.com/unikorn-cloud/core/pkg/util/api"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
)

// Client provides a caching layer for retrieval of region assets, and lazy population.
type Client struct {
	client regionapi.ClientWithResponsesInterface
}

// New returns a new client.
func New(client regionapi.ClientWithResponsesInterface) *Client {
	return &Client{
		client: client,
	}
}

// List lists all regions.
func (c *Client) List(ctx context.Context, organizationID string) ([]regionapi.RegionRead, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsWithResponse(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	regions := *resp.JSON200

	filter := func(x regionapi.RegionRead) bool {
		return x.Spec.Type == regionapi.RegionTypeKubernetes
	}

	filtered := slices.DeleteFunc(regions, filter)

	return filtered, nil
}

// Flavors returns all compute compatible flavors.
func (c *Client) Flavors(ctx context.Context, organizationID, regionID string) ([]regionapi.Flavor, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsRegionIDFlavorsWithResponse(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	flavors := *resp.JSON200

	// TODO: filtering.
	return flavors, nil
}

// Images returns all compute compatible images.
func (c *Client) Images(ctx context.Context, organizationID, regionID string) ([]regionapi.Image, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsRegionIDImagesWithResponse(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	images := *resp.JSON200

	filtered := slices.DeleteFunc(images, func(image regionapi.Image) bool {
		// Delete images that declare any software versions - if it doesn't exist, assume general purpose.
		return image.Spec.SoftwareVersions != nil && len(*image.Spec.SoftwareVersions) > 0
	})

	return filtered, nil
}

func (c *Client) Servers(ctx context.Context, organizationID string, cluster *unikornv1.ComputeCluster) ([]regionapi.ServerRead, error) {
	params := &regionapi.GetApiV1OrganizationsOrganizationIDServersParams{
		Tag: util.ClusterTagSelector(cluster),
	}

	resp, err := c.client.GetApiV1OrganizationsOrganizationIDServersWithResponse(ctx, organizationID, params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	servers := *resp.JSON200

	return servers, nil
}

func (c *Client) DeleteServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	resp, err := c.client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) HardRebootServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	resp, err := c.client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDHardrebootWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return err
	}

	// FIXME: We should rethrow the not found error.
	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) SoftRebootServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	resp, err := c.client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDSoftrebootWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return err
	}

	// FIXME: We should rethrow the not found error.
	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) StartServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	resp, err := c.client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDStartWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return err
	}

	// FIXME: We should rethrow the not found error.
	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) StopServer(ctx context.Context, organizationID, projectID, identityID, serverID string) error {
	resp, err := c.client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDStopWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return err
	}

	// FIXME: We should rethrow the not found error.
	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) CreateConsoleSession(ctx context.Context, organizationID, projectID, identityID, serverID string) (*regionapi.ConsoleSessionResponse, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsolesessionsWithResponse(ctx, organizationID, projectID, identityID, serverID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return resp.JSON200, nil
}

func (c *Client) GetConsoleOutput(ctx context.Context, organizationID, projectID, identityID, serverID string, params *regionapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsoleoutputParams) (*regionapi.ConsoleOutputResponse, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsoleoutputWithResponse(ctx, organizationID, projectID, identityID, serverID, params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return resp.JSON200, nil
}

func GetNetwork(ctx context.Context, client regionapi.ClientWithResponsesInterface, organizationID, projectID, networkID string) (*regionapi.NetworkV2Read, error) {
	response, err := client.GetApiV2NetworksNetworkIDWithResponse(ctx, networkID)
	if err != nil {
		return nil, errors.OAuth2InvalidRequest("unable to get network").WithError(err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, errors.OAuth2ServerError("unable to get network")
	}

	network := response.JSON200

	if network.Metadata.OrganizationId != organizationID || network.Metadata.ProjectId != projectID {
		return nil, errors.OAuth2InvalidRequest("cluster network does not exist")
	}

	return network, nil
}
