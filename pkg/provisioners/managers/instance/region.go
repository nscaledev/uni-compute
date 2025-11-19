/*
Copyright 2025 the Unikorn Authors.

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

package instance

import (
	"context"
	"net/http"

	"github.com/unikorn-cloud/compute/pkg/constants"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreapiutils "github.com/unikorn-cloud/core/pkg/util/api"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	regionclient "github.com/unikorn-cloud/region/pkg/client"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
)

// getRegionClient returns an authenticated client.
// TODO: the client should be cached for an appropriate period to avoid polluting the
// caches in identity with new tokens during busy periods.
func (p *Provisioner) getRegionClient(ctx context.Context) (regionapi.ClientWithResponsesInterface, error) {
	cli, err := coreclient.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	tokenIssuer := identityclient.NewTokenIssuer(cli, p.options.identityOptions, &p.options.clientOptions, constants.ServiceDescriptor())

	token, err := tokenIssuer.Issue(ctx)
	if err != nil {
		return nil, err
	}

	getter := regionclient.New(cli, p.options.regionOptions, &p.options.clientOptions)

	client, err := getter.ControllerClient(ctx, token, &p.instance)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// getServer lists all servers that are part of this cluster.
func (p *Provisioner) getServer(ctx context.Context, client regionapi.ClientWithResponsesInterface) (*regionapi.ServerV2Response, error) {
	params := &regionapi.GetApiV2ServersParams{
		OrganizationID: &regionapi.OrganizationIDQueryParameter{
			p.instance.Labels[coreconstants.OrganizationLabel],
		},
		ProjectID: &regionapi.ProjectIDQueryParameter{
			p.instance.Labels[coreconstants.ProjectLabel],
		},
		RegionID: &regionapi.RegionIDQueryParameter{
			p.instance.Labels[regionconstants.RegionLabel],
		},
		NetworkID: &regionapi.NetworkIDQueryParameter{
			p.instance.Labels[regionconstants.NetworkLabel],
		},
		Tag: &coreapi.TagSelectorParameter{
			constants.InstanceLabel + "=" + p.instance.Name,
		},
	}

	response, err := client.GetApiV2ServersWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusOK {
		return nil, coreapiutils.ExtractError(response.StatusCode(), response)
	}

	result := *response.JSON200

	if len(result) == 0 {
		//nolint:nilnil
		return nil, nil
	}

	return &result[0], nil
}

// createServer creates a new server.
func (p *Provisioner) createServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, request *regionapi.ServerV2Create) (*regionapi.ServerV2Response, error) {
	resp, err := client.PostApiV2ServersWithResponse(ctx, *request)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusCreated {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return resp.JSON201, nil
}

// updateServer updates a server.
func (p *Provisioner) updateServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, serverID string, request *regionapi.ServerV2Update) (*regionapi.ServerV2Response, error) {
	resp, err := client.PutApiV2ServersServerIDWithResponse(ctx, serverID, *request)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusAccepted {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return resp.JSON202, nil
}

// deleteServer deletes a server.
func (p *Provisioner) deleteServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, id string) error {
	resp, err := client.DeleteApiV2ServersServerIDWithResponse(ctx, id)
	if err != nil {
		return err
	}

	// Gone already, ignore me!
	if resp.StatusCode() == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode() != http.StatusAccepted {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	// TODO: add to the status in a deprovisioning state.
	return nil
}
