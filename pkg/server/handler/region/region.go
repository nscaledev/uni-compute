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
	"fmt"
	"net/http"
	"slices"

	"github.com/unikorn-cloud/core/pkg/server/errors"
	identityids "github.com/unikorn-cloud/identity/pkg/ids"
	regionids "github.com/unikorn-cloud/region/pkg/ids"
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
func (c *Client) List(ctx context.Context, organizationID identityids.OrganizationID) ([]regionapi.RegionRead, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsWithResponse(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(resp.HTTPResponse, resp)
	}

	regions := *resp.JSON200

	filter := func(x regionapi.RegionRead) bool {
		return x.Spec.Type == regionapi.RegionTypeKubernetes
	}

	filtered := slices.DeleteFunc(regions, filter)

	return filtered, nil
}

// Flavors returns all compute compatible flavors.
func (c *Client) Flavors(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID) ([]regionapi.Flavor, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsRegionIDFlavorsWithResponse(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(resp.HTTPResponse, resp)
	}

	flavors := *resp.JSON200

	filtered := slices.DeleteFunc(flavors, func(flavor regionapi.Flavor) bool {
		return flavor.Spec.PinnedOnly != nil && *flavor.Spec.PinnedOnly
	})

	return filtered, nil
}

// Images returns the curated image catalog exposed by the Compute API.
func (c *Client) Images(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID) ([]regionapi.Image, error) {
	resp, err := c.client.GetApiV1OrganizationsOrganizationIDRegionsRegionIDImagesWithResponse(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(resp.HTTPResponse, resp)
	}

	images := *resp.JSON200

	filtered := slices.DeleteFunc(images, func(image regionapi.Image) bool {
		return image.Spec.SoftwareVersions != nil && len(*image.Spec.SoftwareVersions) > 0
	})

	return filtered, nil
}

// AvailableImages returns every image Region reports as available to the organization.
// Unlike Images, it does not apply readiness or software-version filters.
func (c *Client) AvailableImages(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID) ([]regionapi.Image, error) {
	organizationIDs := regionapi.OrganizationIDQueryParameter{organizationID.String()}
	scope := regionapi.GetApiV2RegionsRegionIDImagesParamsScopeAvailable
	params := &regionapi.GetApiV2RegionsRegionIDImagesParams{
		OrganizationID: &organizationIDs,
		Scope:          &scope,
	}

	resp, err := c.client.GetApiV2RegionsRegionIDImagesWithResponse(ctx, regionID, params)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(resp.HTTPResponse, resp)
	}

	return *resp.JSON200, nil
}

func GetNetwork(ctx context.Context, client regionapi.ClientWithResponsesInterface, networkID regionids.NetworkID) (*regionapi.NetworkV2Read, error) {
	response, err := client.GetApiV2NetworksNetworkIDWithResponse(ctx, networkID)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to get network", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(response.HTTPResponse, response)
	}

	return response.JSON200, nil
}

func GetSecurityGroup(ctx context.Context, client regionapi.ClientWithResponsesInterface, securityGroupID regionids.SecurityGroupID) (*regionapi.SecurityGroupV2Read, error) {
	response, err := client.GetApiV2SecuritygroupsSecurityGroupIDWithResponse(ctx, securityGroupID)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to get security group", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, errors.PropagateError(response.HTTPResponse, response)
	}

	return response.JSON200, nil
}
