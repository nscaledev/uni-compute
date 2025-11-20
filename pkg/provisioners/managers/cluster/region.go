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

package cluster

import (
	"context"
	"fmt"
	"net/http"

	"github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	regionclient "github.com/unikorn-cloud/region/pkg/client"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"sigs.k8s.io/controller-runtime/pkg/log"
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

	client, err := getter.ControllerClient(ctx, token, &p.cluster)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// getIdentity returns the cloud identity associated with a cluster.
func (p *Provisioner) getIdentity(ctx context.Context, client regionapi.ClientWithResponsesInterface) (*regionapi.IdentityRead, error) {
	log := log.FromContext(ctx)

	response, err := client.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation])
	if err != nil {
		return nil, err
	}

	identity, err := coreapi.ParseJSONPointerResponse[regionapi.IdentityRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	//nolint:exhaustive
	switch identity.Metadata.ProvisioningStatus {
	case coreapi.ResourceProvisioningStatusProvisioned:
		return identity, nil
	case coreapi.ResourceProvisioningStatusUnknown, coreapi.ResourceProvisioningStatusProvisioning:
		log.Info("waiting for identity to become ready")

		return nil, provisioners.ErrYield
	}

	return nil, fmt.Errorf("%w: unhandled status %s", ErrResourceDependency, identity.Metadata.ProvisioningStatus)
}

// deleteIdentity deletes an identity associated with a cluster.
func (p *Provisioner) deleteIdentity(ctx context.Context, client regionapi.ClientWithResponsesInterface) error {
	response, err := client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation])
	if err != nil {
		return err
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err == nil {
		// An accepted status means the API has recorded the deletion event, and
		// we can delete the cluster.  Yield and await deletion next time around.
		return fmt.Errorf("%w: awaiting identity deletion", provisioners.ErrYield)
	}

	if errorsv2.IsAPIResourceMissingError(err) {
		// A not found means it's been deleted already and can proceed.
		return nil
	}

	return err
}

// getNetwork returns the network associated with a compute cluster.
func (p *Provisioner) getNetwork(ctx context.Context, client regionapi.ClientWithResponsesInterface) (*regionapi.NetworkRead, error) {
	log := log.FromContext(ctx)

	networkID, ok := p.cluster.Labels[coreconstants.NetworkLabel]
	if !ok {
		//nolint: nilnil
		return nil, nil
	}

	response, err := client.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDNetworksNetworkIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], networkID)
	if err != nil {
		return nil, err
	}

	network, err := coreapi.ParseJSONPointerResponse[regionapi.NetworkRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	//nolint:exhaustive
	switch network.Metadata.ProvisioningStatus {
	case coreapi.ResourceProvisioningStatusProvisioned:
		return network, nil
	case coreapi.ResourceProvisioningStatusUnknown, coreapi.ResourceProvisioningStatusProvisioning:
		log.Info("waiting for network to become ready")

		return nil, provisioners.ErrYield
	}

	return nil, fmt.Errorf("%w: unhandled status %s", ErrResourceDependency, network.Metadata.ProvisioningStatus)
}

// listServers lists all servers that are part of this cluster.
func (p *Provisioner) listServers(ctx context.Context, client regionapi.ClientWithResponsesInterface) (regionapi.ServersResponse, error) {
	params := &regionapi.GetApiV1OrganizationsOrganizationIDServersParams{
		Tag: util.ClusterTagSelector(&p.cluster),
	}

	response, err := client.GetApiV1OrganizationsOrganizationIDServersWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], params)
	if err != nil {
		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.ServersRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	return *data, nil
}

// createServer creates a new server.
func (p *Provisioner) createServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, request *regionapi.ServerWrite) (*regionapi.ServerResponse, error) {
	response, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], *request)
	if err != nil {
		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.ServerRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusCreated)
}

// updateServer updates a server.
func (p *Provisioner) updateServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, serverID string, request *regionapi.ServerWrite) (*regionapi.ServerResponse, error) {
	response, err := client.PutApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], serverID, *request)
	if err != nil {
		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.ServerRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusAccepted)
}

// deleteServer deletes a server.
func (p *Provisioner) deleteServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, id string) error {
	response, err := client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], id)
	if err != nil {
		return err
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err == nil {
		// TODO: add to the status in a deprovisioning state.
		return nil
	}

	if errorsv2.IsAPIResourceMissingError(err) {
		// Gone already, ignore me!
		return nil
	}

	return err
}

// listSecurityGroups reads all security groups for the cluster.
func (p *Provisioner) listSecurityGroups(ctx context.Context, client regionapi.ClientWithResponsesInterface) (regionapi.SecurityGroupsResponse, error) {
	params := &regionapi.GetApiV1OrganizationsOrganizationIDSecuritygroupsParams{
		Tag: util.ClusterTagSelector(&p.cluster),
	}

	response, err := client.GetApiV1OrganizationsOrganizationIDSecuritygroupsWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], params)
	if err != nil {
		return nil, err
	}

	data, err := coreapi.ParseJSONPointerResponse[regionapi.SecurityGroupsRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusOK)
	if err != nil {
		return nil, err
	}

	return *data, nil
}

// createSecurityGroup creates a security group.
func (p *Provisioner) createSecurityGroup(ctx context.Context, client regionapi.ClientWithResponsesInterface, request *regionapi.SecurityGroupWrite) (*regionapi.SecurityGroupRead, error) {
	response, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDSecuritygroupsWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], *request)
	if err != nil {
		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.SecurityGroupRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusCreated)
}

// updateSecurityGroup updates a security group.
func (p *Provisioner) updateSecurityGroup(ctx context.Context, client regionapi.ClientWithResponsesInterface, id string, request *regionapi.SecurityGroupWrite) (*regionapi.SecurityGroupRead, error) {
	response, err := client.PutApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDSecuritygroupsSecurityGroupIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], id, *request)
	if err != nil {
		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.SecurityGroupRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusAccepted)
}

// deleteSecurityGroup delete's a security group.
func (p *Provisioner) deleteSecurityGroup(ctx context.Context, client regionapi.ClientWithResponsesInterface, id string) error {
	response, err := client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDSecuritygroupsSecurityGroupIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], id)
	if err != nil {
		return err
	}

	err = coreapi.AssertResponseStatus(response.HTTPResponse.Header, response.StatusCode(), http.StatusAccepted)
	if err != nil && !errorsv2.IsAPIResourceMissingError(err) {
		return err
	}

	return nil
}
