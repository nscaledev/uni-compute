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
	"cmp"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"reflect"
	"slices"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/util"
	corev1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/conversion"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/saga"
	coreutil "github.com/unikorn-cloud/core/pkg/server/util"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	"github.com/unikorn-cloud/identity/pkg/handler/common"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RegionAPIClientGetter func(context.Context) (regionapi.ClientWithResponsesInterface, error)

type Client struct {
	// client ia a Kubernetes client.
	client client.Client
	// namespace we are running in.
	namespace string
	// identity is a client to access the identity service.
	identity identityclient.APIClientGetter
	// region is a client to access regions.
	region RegionAPIClientGetter
}

// New creates a new client.
func NewClient(client client.Client, namespace string, identity identityclient.APIClientGetter, region RegionAPIClientGetter) *Client {
	return &Client{
		client:    client,
		namespace: namespace,
		identity:  identity,
		region:    region,
	}
}

func convertCreateToUpdateRequest(in *computeapi.InstanceCreate) (*computeapi.InstanceUpdate, error) {
	t, err := json.Marshal(in)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to marshal request").WithError(err)
	}

	out := &computeapi.InstanceUpdate{}

	if err := json.Unmarshal(t, out); err != nil {
		return nil, errors.OAuth2ServerError("failed to unmarshal request").WithError(err)
	}

	return out, nil
}

func convertNetworking(in *computev1.ComputeInstanceNetworking) *computeapi.InstanceNetworking {
	if in == nil {
		return nil
	}

	var out computeapi.InstanceNetworking

	if in.PublicIP {
		out.PublicIP = ptr.To(true)
	}

	if len(in.SecurityGroupIDs) > 0 {
		out.SecurityGroups = ptr.To(in.SecurityGroupIDs)
	}

	if len(in.AllowedSourceAddresses) > 0 {
		allowedSourceAddresses := make([]string, len(in.AllowedSourceAddresses))

		for i := range in.AllowedSourceAddresses {
			allowedSourceAddresses[i] = in.AllowedSourceAddresses[i].String()
		}

		out.AllowedSourceAddresses = ptr.To(allowedSourceAddresses)
	}

	if reflect.ValueOf(out).IsZero() {
		return nil
	}

	return &out
}

func convertUserData(in []byte) *[]byte {
	if in == nil {
		return nil
	}

	return &in
}

func convertPowerState(in *regionv1.InstanceLifecyclePhase) *regionapi.InstanceLifecyclePhase {
	if in == nil || *in == "" {
		return nil
	}

	switch *in {
	case regionv1.InstanceLifecyclePhasePending:
		return ptr.To(regionapi.Pending)
	case regionv1.InstanceLifecyclePhaseRunning:
		return ptr.To(regionapi.Running)
	case regionv1.InstanceLifecyclePhaseStopping:
		return ptr.To(regionapi.Stopping)
	case regionv1.InstanceLifecyclePhaseStopped:
		return ptr.To(regionapi.Stopped)
	}

	return nil
}

func convert(in *computev1.ComputeInstance) *computeapi.InstanceRead {
	out := &computeapi.InstanceRead{
		Metadata: conversion.ProjectScopedResourceReadMetadata(in, in.Spec.Tags),
		Spec: computeapi.InstanceSpec{
			FlavorId:   in.Spec.FlavorID,
			ImageId:    in.Spec.ImageID,
			Networking: convertNetworking(in.Spec.Networking),
			UserData:   convertUserData(in.Spec.UserData),
		},
		Status: computeapi.InstanceStatus{
			RegionId:   in.Labels[regionconstants.RegionLabel],
			NetworkId:  in.Labels[regionconstants.NetworkLabel],
			PowerState: convertPowerState(in.Status.PowerState),
			PrivateIP:  in.Status.PrivateIP,
			PublicIP:   in.Status.PublicIP,
		},
	}

	return out
}

func convertList(in *computev1.ComputeInstanceList) []computeapi.InstanceRead {
	out := make([]computeapi.InstanceRead, len(in.Items))

	for i := range in.Items {
		out[i] = *convert(&in.Items[i])
	}

	return out
}

func generateNetworking(in *computeapi.InstanceNetworking) (*computev1.ComputeInstanceNetworking, error) {
	if in == nil {
		//nolint:nilnil
		return nil, nil
	}

	var temp computev1.ComputeInstanceNetworking

	networking := *in

	if networking.PublicIP != nil {
		temp.PublicIP = *networking.PublicIP
	}

	if networking.SecurityGroups != nil {
		temp.SecurityGroupIDs = *networking.SecurityGroups
	}

	if networking.AllowedSourceAddresses != nil {
		allowedSourceAddresses := *networking.AllowedSourceAddresses

		temp.AllowedSourceAddresses = make([]corev1.IPv4Prefix, len(allowedSourceAddresses))

		for i, v := range allowedSourceAddresses {
			_, prefix, err := net.ParseCIDR(v)
			if err != nil {
				return nil, errors.OAuth2InvalidRequest("failed to parse IPv4 prefix").WithError(err)
			}

			temp.AllowedSourceAddresses[i] = corev1.IPv4Prefix{
				IPNet: *prefix,
			}
		}
	}

	if reflect.ValueOf(temp).IsZero() {
		//nolint:nilnil
		return nil, nil
	}

	return &temp, nil
}

func generateUserdata(in *[]byte) []byte {
	if in == nil || len(*in) == 0 {
		return nil
	}

	return *in
}

func (c *Client) generate(ctx context.Context, in *computeapi.InstanceUpdate, organizationID, projectID, regionID, networkID string) (*computev1.ComputeInstance, error) {
	networking, err := generateNetworking(in.Spec.Networking)
	if err != nil {
		return nil, err
	}

	out := &computev1.ComputeInstance{
		ObjectMeta: conversion.NewObjectMetadata(&in.Metadata, c.namespace).
			WithOrganization(organizationID).
			WithProject(projectID).
			WithLabel(regionconstants.RegionLabel, regionID).
			WithLabel(regionconstants.NetworkLabel, networkID).
			Get(),
		Spec: computev1.ComputeInstanceSpec{
			MachineGeneric: corev1.MachineGeneric{
				FlavorID: in.Spec.FlavorId,
				ImageID:  in.Spec.ImageId,
			},
			Networking: networking,
			UserData:   generateUserdata(in.Spec.UserData),
		},
	}

	if err := util.InjectUserPrincipal(ctx, organizationID, projectID); err != nil {
		return nil, errors.OAuth2ServerError("unable to set principal information").WithError(err)
	}

	if err := common.SetIdentityMetadata(ctx, &out.ObjectMeta); err != nil {
		return nil, errors.OAuth2ServerError("failed to set identity metadata").WithError(err)
	}

	return out, nil
}

func (c *Client) List(ctx context.Context, params computeapi.GetApiV2InstancesParams) (computeapi.InstancesRead, error) {
	var err error

	selector := labels.Everything()

	selector, err = rbac.AddOrganizationAndProjectIDQuery(ctx, selector, util.OrganizationIDQuery(params.OrganizationID), util.ProjectIDQuery(params.ProjectID))
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to add identity label selector").WithError(err)
	}

	selector, err = util.AddRegionIDQuery(selector, params.RegionID)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to add region label selector").WithError(err)
	}

	selector, err = util.AddNetworkIDQuery(selector, params.NetworkID)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to add network label selector").WithError(err)
	}

	options := &client.ListOptions{
		Namespace:     c.namespace,
		LabelSelector: selector,
	}

	result := &computev1.ComputeInstanceList{}

	if err := c.client.List(ctx, result, options); err != nil {
		return nil, errors.OAuth2ServerError("unable to list instances").WithError(err)
	}

	tagSelector, err := coreutil.DecodeTagSelectorParam(params.Tag)
	if err != nil {
		return nil, err
	}

	result.Items = slices.DeleteFunc(result.Items, func(resource computev1.ComputeInstance) bool {
		return !resource.Spec.Tags.ContainsAll(tagSelector) ||
			rbac.AllowProjectScope(ctx, "compute:instances", identityapi.Read, resource.Labels[coreconstants.OrganizationLabel], resource.Labels[coreconstants.ProjectLabel]) != nil
	})

	slices.SortStableFunc(result.Items, func(a, b computev1.ComputeInstance) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return convertList(result), nil
}

type createSaga struct {
	client   *Client
	resource *computev1.ComputeInstance
}

func newCreateSaga(client *Client, resource *computev1.ComputeInstance) *createSaga {
	return &createSaga{
		client:   client,
		resource: resource,
	}
}

func (s *createSaga) createAllocation(ctx context.Context) error {
	required := identityapi.ResourceAllocationList{
		{
			Kind:      "servers",
			Committed: 1,
		},
	}

	return identityclient.NewAllocations(s.client.client, s.client.identity).Create(ctx, s.resource, required)
}

func (s *createSaga) deleteAllocation(ctx context.Context) error {
	return identityclient.NewAllocations(s.client.client, s.client.identity).Delete(ctx, s.resource)
}

func (s *createSaga) createInstance(ctx context.Context) error {
	if err := s.client.client.Create(ctx, s.resource); err != nil {
		return errors.OAuth2ServerError("unable to create instance").WithError(err)
	}

	return nil
}

func (s *createSaga) Actions() []saga.Action {
	return []saga.Action{
		saga.NewAction("create quota allocation", s.createAllocation, s.deleteAllocation),
		saga.NewAction("create instance", s.createInstance, nil),
	}
}

func (c *Client) getNetwork(ctx context.Context, organizationID, projectID, networkID string) (*regionapi.NetworkV2Read, error) {
	region, err := c.region(ctx)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to create region client").WithError(err)
	}

	response, err := region.GetApiV2NetworksNetworkIDWithResponse(ctx, networkID)
	if err != nil {
		return nil, errors.OAuth2InvalidRequest("unable to get network").WithError(err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, errors.OAuth2InvalidRequest("unable to get network - wrong status code")
	}

	network := response.JSON200

	if network.Metadata.OrganizationId != organizationID || network.Metadata.ProjectId != projectID {
		return nil, errors.OAuth2InvalidRequest("instance network does not match requested organization/project")
	}

	return network, nil
}

func (c *Client) Create(ctx context.Context, request *computeapi.InstanceCreate) (*computeapi.InstanceRead, error) {
	organizationID := request.Spec.OrganizationId
	projectID := request.Spec.ProjectId

	if err := rbac.AllowProjectScope(ctx, "compute:instances", identityapi.Create, organizationID, projectID); err != nil {
		return nil, err
	}

	// Lookup the network so that we can infer things about it, specifically the region ID
	// which can then be used to label the instance for list API.  We need to double check
	// that the network matches the requested organization and project first.  Ideally we
	// would get the network impersonating the user principal and let the region service do
	// the necessary ReBAC checks, but we cannot do that yet.  If we could do that we could
	// infer the organization and project IDs too and not have to specify them in this API.
	network, err := c.getNetwork(ctx, organizationID, projectID, request.Spec.NetworkId)
	if err != nil {
		return nil, err
	}

	regionID := network.Status.RegionId

	updateRequest, err := convertCreateToUpdateRequest(request)
	if err != nil {
		return nil, err
	}

	resource, err := c.generate(ctx, updateRequest, organizationID, projectID, regionID, request.Spec.NetworkId)
	if err != nil {
		return nil, err
	}

	s := newCreateSaga(c, resource)

	if err := saga.Run(ctx, s); err != nil {
		return nil, err
	}

	return convert(resource), nil
}

func (c *Client) GetRaw(ctx context.Context, instanceID string) (*computev1.ComputeInstance, error) {
	result := &computev1.ComputeInstance{}

	if err := c.client.Get(ctx, client.ObjectKey{Namespace: c.namespace, Name: instanceID}, result); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.HTTPNotFound().WithError(err)
		}

		return nil, errors.OAuth2ServerError("unable to lookup instance").WithError(err)
	}

	if err := rbac.AllowProjectScope(ctx, "compute:instances", identityapi.Read, result.Labels[coreconstants.OrganizationLabel], result.Labels[coreconstants.ProjectLabel]); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) Get(ctx context.Context, instanceID string) (*computeapi.InstanceRead, error) {
	result, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return convert(result), nil
}

func (c *Client) Update(ctx context.Context, instanceID string, request *computeapi.InstanceUpdate) (*computeapi.InstanceRead, error) {
	current, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	organizationID := current.Labels[coreconstants.OrganizationLabel]
	projectID := current.Labels[coreconstants.ProjectLabel]
	regionID := current.Labels[regionconstants.RegionLabel]
	networkID := current.Labels[regionconstants.NetworkLabel]

	if err := rbac.AllowProjectScope(ctx, "compute:instances", identityapi.Update, organizationID, projectID); err != nil {
		return nil, err
	}

	if current.DeletionTimestamp != nil {
		return nil, errors.OAuth2InvalidRequest("server is being deleted")
	}

	required, err := c.generate(ctx, request, organizationID, projectID, regionID, networkID)
	if err != nil {
		return nil, err
	}

	updated := current.DeepCopy()
	updated.Labels = required.Labels
	updated.Annotations = required.Annotations
	updated.Spec = required.Spec

	if err := c.client.Patch(ctx, updated, client.MergeFrom(current)); err != nil {
		return nil, errors.OAuth2ServerError("unable to update instance").WithError(err)
	}

	return convert(updated), nil
}

func (c *Client) Delete(ctx context.Context, instanceID string) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	if resource.DeletionTimestamp != nil {
		return nil
	}

	if err := rbac.AllowProjectScope(ctx, "compute:instances", identityapi.Delete, resource.Labels[coreconstants.OrganizationLabel], resource.Labels[coreconstants.ProjectLabel]); err != nil {
		return err
	}

	if err := identityclient.NewAllocations(c.client, c.identity).Delete(ctx, resource); err != nil {
		return err
	}

	if err := c.client.Delete(ctx, resource); err != nil {
		if kerrors.IsNotFound(err) {
			return errors.HTTPNotFound().WithError(err)
		}

		return errors.OAuth2ServerError("unable to delete instance").WithError(err)
	}

	return nil
}

func (c *Client) serverID(ctx context.Context, region regionapi.ClientWithResponsesInterface, instance *computev1.ComputeInstance) (string, error) {
	reference, err := manager.GenerateResourceReference(c.client, instance)
	if err != nil {
		return "", errors.OAuth2ServerError("unable to generate instance reference").WithError(err)
	}

	// Constrain the search domain.
	params := &regionapi.GetApiV2ServersParams{
		OrganizationID: &computeapi.OrganizationIDQueryParameter{
			instance.Labels[coreconstants.OrganizationLabel],
		},
		ProjectID: &computeapi.ProjectIDQueryParameter{
			instance.Labels[coreconstants.ProjectLabel],
		},
		RegionID: &computeapi.RegionIDQueryParameter{
			instance.Labels[regionconstants.RegionLabel],
		},
		NetworkID: &computeapi.NetworkIDQueryParameter{
			instance.Labels[regionconstants.NetworkLabel],
		},
		Tag: &coreapi.TagSelectorParameter{
			reference,
		},
	}

	response, err := region.GetApiV2ServersWithResponse(ctx, params)
	if err != nil {
		return "", errors.OAuth2ServerError("unable to query servers for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusOK {
		return "", errors.OAuth2ServerError("unable to query servers for instance - incorrect status code")
	}

	servers := *response.JSON200

	if len(servers) != 1 {
		return "", errors.OAuth2ServerError("unable to query server for instance - incorrect number of matches")
	}

	return servers[0].Metadata.Id, nil
}

func (c *Client) Start(ctx context.Context, instanceID string) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	region, err := c.region(ctx)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, region, resource)
	if err != nil {
		return err
	}

	response, err := region.PostApiV2ServersServerIDStartWithResponse(ctx, serverID)
	if err != nil {
		return errors.OAuth2ServerError("unable to start server for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return errors.OAuth2ServerError("unable to start server for instance - incorrect status code")
	}

	return nil
}

func (c *Client) Stop(ctx context.Context, instanceID string) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	region, err := c.region(ctx)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, region, resource)
	if err != nil {
		return err
	}

	response, err := region.PostApiV2ServersServerIDStopWithResponse(ctx, serverID)
	if err != nil {
		return errors.OAuth2ServerError("unable to stop server for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return errors.OAuth2ServerError("unable to start server for instance - incorrect status code")
	}

	return nil
}

func (c *Client) Reboot(ctx context.Context, instanceID string, params computeapi.PostApiV2InstancesInstanceIDRebootParams) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	region, err := c.region(ctx)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, region, resource)
	if err != nil {
		return err
	}

	// TODO: we should just pass this through...
	if params.Hard != nil && *params.Hard {
		response, err := region.PostApiV2ServersServerIDHardrebootWithResponse(ctx, serverID)
		if err != nil {
			return errors.OAuth2ServerError("unable to reboot server for instance").WithError(err)
		}

		if response.StatusCode() != http.StatusAccepted {
			return errors.OAuth2ServerError("unable to reboot server for instance - incorrect status code")
		}

		return nil
	}

	response, err := region.PostApiV2ServersServerIDSoftrebootWithResponse(ctx, serverID)
	if err != nil {
		return errors.OAuth2ServerError("unable to reboot server for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return errors.OAuth2ServerError("unable to reboot server for instance - incorrect status code")
	}

	return nil
}

func (c *Client) ConsoleOutput(ctx context.Context, instanceID string, params computeapi.GetApiV2InstancesInstanceIDConsoleoutputParams) (*regionapi.ConsoleOutputResponse, error) {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	region, err := c.region(ctx)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, region, resource)
	if err != nil {
		return nil, err
	}

	var requestParams *regionapi.GetApiV2ServersServerIDConsoleoutputParams

	if params.Length != nil {
		requestParams = &regionapi.GetApiV2ServersServerIDConsoleoutputParams{
			Length: params.Length,
		}
	}

	response, err := region.GetApiV2ServersServerIDConsoleoutputWithResponse(ctx, serverID, requestParams)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to get console output for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return nil, errors.OAuth2ServerError("unable to get console output for instance - incorrect status code")
	}

	return response.JSON200, nil
}

func (c *Client) ConsoleSession(ctx context.Context, instanceID string) (*regionapi.ConsoleSessionResponse, error) {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	region, err := c.region(ctx)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, region, resource)
	if err != nil {
		return nil, err
	}

	response, err := region.GetApiV2ServersServerIDConsolesessionsWithResponse(ctx, serverID)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to start console session for instance").WithError(err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, errors.OAuth2ServerError("unable to start console session for instance - incorrect status code")
	}

	return response.JSON200, nil
}
