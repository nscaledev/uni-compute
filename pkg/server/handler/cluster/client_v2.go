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
	"cmp"
	"context"
	"encoding/json"
	"slices"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	"github.com/unikorn-cloud/compute/pkg/server/handler/util"
	corev1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/server/conversion"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	coreutil "github.com/unikorn-cloud/core/pkg/server/util"
	"github.com/unikorn-cloud/identity/pkg/handler/common"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func convertCreateToUpdateRequest(in *computeapi.ClusterV2Create) (*computeapi.ClusterV2Update, error) {
	t, err := json.Marshal(in)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to marshal request").WithError(err)
	}

	out := &computeapi.ClusterV2Update{}

	if err := json.Unmarshal(t, out); err != nil {
		return nil, errors.OAuth2ServerError("failed to unmarshal request").WithError(err)
	}

	return out, nil
}

func convertPools(in []computev1.InstancePoolSpec) computeapi.PoolV2List {
	out := make(computeapi.PoolV2List, len(in))

	for i := range in {
		out[i] = computeapi.PoolV2{
			Name:       in[i].Name,
			Replicas:   in[i].Replicas,
			FlavorId:   in[i].Template.FlavorID,
			ImageId:    in[i].Template.ImageID,
			Networking: instance.ConvertNetworking(in[i].Template.Networking),
			UserData:   instance.ConvertUserData(in[i].Template.UserData),
		}
	}

	return out
}

func convertPoolsStatus(in []computev1.InstancePoolStatus) computeapi.PoolV2StatusList {
	out := make(computeapi.PoolV2StatusList, len(in))

	for i := range in {
		out[i] = computeapi.PoolV2Status{
			Name:     in[i].Name,
			Replicas: in[i].Replicas,
		}
	}

	return out
}

func convert(in *computev1.ComputeCluster) *computeapi.ClusterV2Read {
	out := &computeapi.ClusterV2Read{
		Metadata: conversion.ProjectScopedResourceReadMetadata(in, in.Spec.Tags),
		Spec: computeapi.ClusterV2Spec{
			Pools: convertPools(in.Spec.Pools),
		},
		Status: computeapi.ClusterV2Status{
			RegionId:  in.Labels[regionconstants.RegionLabel],
			NetworkId: in.Labels[regionconstants.NetworkLabel],
			Pools:     convertPoolsStatus(in.Status.Pools),
		},
	}

	return out
}

func convertList(in *computev1.ComputeClusterList) []computeapi.ClusterV2Read {
	out := make([]computeapi.ClusterV2Read, len(in.Items))

	for i := range in.Items {
		out[i] = *convert(&in.Items[i])
	}

	return out
}

func generatePools(in computeapi.PoolV2List) ([]computev1.InstancePoolSpec, error) {
	if len(in) == 0 {
		return nil, nil
	}

	out := make([]computev1.InstancePoolSpec, len(in))

	for i := range in {
		networking, err := instance.GenerateNetworking(in[i].Networking)
		if err != nil {
			return nil, err
		}

		out[i] = computev1.InstancePoolSpec{
			Name:     in[i].Name,
			Replicas: in[i].Replicas,
			Template: computev1.ComputeInstanceSpec{
				MachineGeneric: corev1.MachineGeneric{
					FlavorID: in[i].FlavorId,
					ImageID:  in[i].ImageId,
				},
				Networking: networking,
				UserData:   instance.GenerateUserData(in[i].UserData),
			},
		}
	}

	return out, nil
}

func (c *Client) generate(ctx context.Context, in *computeapi.ClusterV2Update, organizationID, projectID, regionID, networkID string) (*computev1.ComputeCluster, error) {
	pools, err := generatePools(in.Spec.Pools)
	if err != nil {
		return nil, err
	}

	out := &computev1.ComputeCluster{
		ObjectMeta: conversion.NewObjectMetadata(&in.Metadata, c.namespace).
			WithOrganization(organizationID).
			WithProject(projectID).
			WithLabel(regionconstants.RegionLabel, regionID).
			WithLabel(regionconstants.NetworkLabel, networkID).
			WithLabel(constants.ResourceAPIVersionLabel, constants.MarshalAPIVersion(2)).
			Get(),
		Spec: computev1.ComputeClusterSpec{
			Tags:  conversion.GenerateTagList(in.Metadata.Tags),
			Pools: pools,
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

func (c *Client) ListV2(ctx context.Context, params computeapi.GetApiV2ClustersParams) (computeapi.ClusterV2ReadList, error) {
	var err error

	selector := labels.SelectorFromSet(map[string]string{
		constants.ResourceAPIVersionLabel: constants.MarshalAPIVersion(2),
	})

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

	result := &computev1.ComputeClusterList{}

	if err := c.client.List(ctx, result, options); err != nil {
		return nil, errors.OAuth2ServerError("unable to list clusters").WithError(err)
	}

	tagSelector, err := coreutil.DecodeTagSelectorParam(params.Tag)
	if err != nil {
		return nil, err
	}

	result.Items = slices.DeleteFunc(result.Items, func(resource computev1.ComputeCluster) bool {
		return !resource.Spec.Tags.ContainsAll(tagSelector) ||
			rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Read, resource.Labels[coreconstants.OrganizationLabel], resource.Labels[coreconstants.ProjectLabel]) != nil
	})

	slices.SortStableFunc(result.Items, func(a, b computev1.ComputeCluster) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return convertList(result), nil
}

func (c *Client) CreateV2(ctx context.Context, request *computeapi.ClusterV2Create) (*computeapi.ClusterV2Read, error) {
	organizationID := request.Spec.OrganizationId
	projectID := request.Spec.ProjectId

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Create, organizationID, projectID); err != nil {
		return nil, err
	}

	// Lookup the network so that we can infer things about it, specifically the region ID
	// which can then be used to label the cluster for list API.  We need to double check
	// that the network matches the requested organization and project first.  Ideally we
	// would get the network impersonating the user principal and let the region service do
	// the necessary ReBAC checks, but we cannot do that yet.  If we could do that we could
	// infer the organization and project IDs too and not have to specify them in this API.
	network, err := region.GetNetwork(ctx, c.region, organizationID, projectID, request.Spec.NetworkId)
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

	if err := c.client.Create(ctx, resource); err != nil {
		return nil, errors.OAuth2ServerError("unable to create cluster").WithError(err)
	}

	return convert(resource), nil
}

func (c *Client) GetRawV2(ctx context.Context, clusterID string) (*computev1.ComputeCluster, error) {
	result := &computev1.ComputeCluster{}

	if err := c.client.Get(ctx, client.ObjectKey{Namespace: c.namespace, Name: clusterID}, result); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.HTTPNotFound().WithError(err)
		}

		return nil, errors.OAuth2ServerError("unable to lookup cluster").WithError(err)
	}

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Read, result.Labels[coreconstants.OrganizationLabel], result.Labels[coreconstants.ProjectLabel]); err != nil {
		return nil, err
	}

	// Only allow access to resources created by this API (temporarily).
	v, ok := result.Labels[constants.ResourceAPIVersionLabel]
	if !ok {
		return nil, errors.HTTPNotFound()
	}

	version, err := constants.UnmarshalAPIVersion(v)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to parse API version")
	}

	if version != 2 {
		return nil, errors.HTTPNotFound()
	}

	return result, nil
}

func (c *Client) GetV2(ctx context.Context, clusterID string) (*computeapi.ClusterV2Read, error) {
	result, err := c.GetRawV2(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	return convert(result), nil
}

func (c *Client) UpdateV2(ctx context.Context, clusterID string, request *computeapi.ClusterV2Update) (*computeapi.ClusterV2Read, error) {
	current, err := c.GetRawV2(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	organizationID := current.Labels[coreconstants.OrganizationLabel]
	projectID := current.Labels[coreconstants.ProjectLabel]
	regionID := current.Labels[regionconstants.RegionLabel]
	networkID := current.Labels[regionconstants.NetworkLabel]

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Update, organizationID, projectID); err != nil {
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
		return nil, errors.OAuth2ServerError("unable to update cluster").WithError(err)
	}

	return convert(updated), nil
}

func (c *Client) DeleteV2(ctx context.Context, clusterID string) error {
	resource, err := c.GetRawV2(ctx, clusterID)
	if err != nil {
		return err
	}

	if resource.DeletionTimestamp != nil {
		return nil
	}

	if err := rbac.AllowProjectScope(ctx, "compute:clusters", identityapi.Delete, resource.Labels[coreconstants.OrganizationLabel], resource.Labels[coreconstants.ProjectLabel]); err != nil {
		return err
	}

	if err := c.client.Delete(ctx, resource); err != nil {
		if kerrors.IsNotFound(err) {
			return errors.HTTPNotFound().WithError(err)
		}

		return errors.OAuth2ServerError("unable to delete cluster").WithError(err)
	}

	return nil
}
