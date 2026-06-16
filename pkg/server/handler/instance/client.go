/*
Copyright 2025 the Unikorn Authors.
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

package instance

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"github.com/google/uuid"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	computeids "github.com/unikorn-cloud/compute/pkg/ids"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	"github.com/unikorn-cloud/compute/pkg/server/handler/util"
	corev1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/conversion"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/saga"
	coreutil "github.com/unikorn-cloud/core/pkg/server/util"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	"github.com/unikorn-cloud/identity/pkg/handler/common"
	identityids "github.com/unikorn-cloud/identity/pkg/ids"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/principal"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionids "github.com/unikorn-cloud/region/pkg/ids"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
	servermanager "github.com/unikorn-cloud/region/pkg/provisioners/managers/server"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Client struct {
	// client ia a Kubernetes client.
	client client.Client
	// namespace we are running in.
	namespace string
	// identity is a client to access the identity service.
	identity identityapi.ClientWithResponsesInterface
	// region is a client to access regions.
	region regionapi.ClientWithResponsesInterface
}

// New creates a new client.
func NewClient(client client.Client, namespace string, identity identityapi.ClientWithResponsesInterface, region regionapi.ClientWithResponsesInterface) *Client {
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
		return nil, fmt.Errorf("%w: failed to marshal request", err)
	}

	out := &computeapi.InstanceUpdate{}

	if err := json.Unmarshal(t, out); err != nil {
		return nil, fmt.Errorf("%w: failed to unmarshal request", err)
	}

	return out, nil
}

func ConvertNetworking(in *computev1.ComputeInstanceNetworking) *computeapi.InstanceNetworking {
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

func ConvertUserData(in []byte) *[]byte {
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
		return ptr.To(regionapi.InstanceLifecyclePhasePending)
	case regionv1.InstanceLifecyclePhaseQueued:
		return ptr.To(regionapi.InstanceLifecyclePhaseQueued)
	case regionv1.InstanceLifecyclePhaseBuilding:
		return ptr.To(regionapi.InstanceLifecyclePhaseBuilding)
	case regionv1.InstanceLifecyclePhaseRunning:
		return ptr.To(regionapi.InstanceLifecyclePhaseRunning)
	case regionv1.InstanceLifecyclePhaseStopping:
		return ptr.To(regionapi.InstanceLifecyclePhaseStopping)
	case regionv1.InstanceLifecyclePhaseStopped:
		return ptr.To(regionapi.InstanceLifecyclePhaseStopped)
	}

	return nil
}

// parseOptionalSSHCertificateAuthorityID parses an optional SSH certificate authority
// ID stored as a string on the CRD back to the typed ID, failing closed on a malformed
// value. A nil input (no CA referenced) yields a nil output.
func parseOptionalSSHCertificateAuthorityID(in *string) (*regionapi.SshCertificateAuthorityId, error) {
	if in == nil {
		//nolint:nilnil
		return nil, nil
	}

	id, err := regionids.ParseSSHCertificateAuthorityID(*in)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

func convert(in *computev1.ComputeInstance) (*computeapi.InstanceRead, error) {
	// The flavor, image and SSH CA IDs are stored as strings on the CRD spec; parse
	// them back to the typed IDs for the read model. They were UUID-validated on the
	// write path, so a malformed value here means a tampered resource: fail closed.
	flavorID, err := regionids.ParseFlavorID(in.Spec.FlavorID)
	if err != nil {
		return nil, err
	}

	imageID, err := regionids.ParseImageID(in.Spec.ImageID)
	if err != nil {
		return nil, err
	}

	sshCertificateAuthorityID, err := parseOptionalSSHCertificateAuthorityID(in.Spec.SSHCertificateAuthorityID)
	if err != nil {
		return nil, err
	}

	out := &computeapi.InstanceRead{
		Metadata: conversion.ProjectScopedResourceReadMetadata(in, in.Spec.Tags),
		Spec: computeapi.InstanceSpec{
			FlavorId:                  flavorID,
			ImageId:                   imageID,
			Networking:                ConvertNetworking(in.Spec.Networking),
			SshCertificateAuthorityId: sshCertificateAuthorityID,
			UserData:                  ConvertUserData(in.Spec.UserData),
		},
		Status: computeapi.InstanceStatus{
			RegionId:   in.Labels[regionconstants.RegionLabel],
			NetworkId:  in.Labels[regionconstants.NetworkLabel],
			PowerState: convertPowerState(in.Status.PowerState),
			PrivateIP:  in.Status.PrivateIP,
			PublicIP:   in.Status.PublicIP,
			MacAddress: in.Status.MACAddress,
		},
	}

	return out, nil
}

func convertList(in *computev1.ComputeInstanceList) ([]computeapi.InstanceRead, error) {
	out := make([]computeapi.InstanceRead, len(in.Items))

	for i := range in.Items {
		item, err := convert(&in.Items[i])
		if err != nil {
			return nil, err
		}

		out[i] = *item
	}

	return out, nil
}

// scopeFromLabels recovers the typed owning organization and project IDs from a
// resource's labels, failing closed if either is missing or malformed.
func scopeFromLabels(l map[string]string) (identityids.OrganizationID, identityids.ProjectID, error) {
	organizationID, err := identityids.ParseOrganizationID(l[coreconstants.OrganizationLabel])
	if err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, err
	}

	projectID, err := identityids.ParseProjectID(l[coreconstants.ProjectLabel])
	if err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, err
	}

	return organizationID, projectID, nil
}

func GenerateNetworking(in *computeapi.InstanceNetworking) (*computev1.ComputeInstanceNetworking, error) {
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

func GenerateUserData(in *[]byte) []byte {
	if in == nil || len(*in) == 0 {
		return nil
	}

	return *in
}

func validateUserDataForSSHCertificateAuthority(sshCertificateAuthorityID *regionapi.SshCertificateAuthorityId, userData *[]byte) error {
	if sshCertificateAuthorityID == nil || userData == nil || len(*userData) == 0 {
		return nil
	}

	if err := servermanager.ValidateManagedUserData(*userData); err == nil {
		return nil
	}

	return errors.HTTPUnprocessableContent("userData must be a recognized cloud-init format when sshCertificateAuthorityId is specified")
}

func (c *Client) validateSSHCertificateAuthorityReference(ctx context.Context, organizationID identityids.OrganizationID, projectID identityids.ProjectID, sshCertificateAuthorityID *regionapi.SshCertificateAuthorityId) error {
	if sshCertificateAuthorityID == nil {
		return nil
	}

	response, err := c.region.GetApiV2SshcertificateauthoritiesSshCertificateAuthorityIDWithResponse(ctx, *sshCertificateAuthorityID)
	if err != nil {
		return err
	}

	if response.StatusCode() != http.StatusOK {
		return errors.PropagateError(response.HTTPResponse, response)
	}

	return validateSSHCertificateAuthorityScope(response.JSON200, organizationID, projectID)
}

func validateSSHCertificateAuthorityScope(resource *regionapi.SshCertificateAuthorityV2Response, organizationID identityids.OrganizationID, projectID identityids.ProjectID) error {
	if resource.Metadata.OrganizationId != organizationID.String() || resource.Metadata.ProjectId != projectID.String() {
		return errors.HTTPUnprocessableContent("sshCertificateAuthorityId must reference an SSH certificate authority in the same organization and project as the instance")
	}

	return nil
}

func (c *Client) validateCreateRequest(ctx context.Context, request *computeapi.InstanceCreate, organizationID identityids.OrganizationID, projectID identityids.ProjectID, regionID regionids.RegionID) (*regionapi.Flavor, error) {
	flavor, _, err := c.getAndValidateFlavorAndImage(principal.NewImpersonateContext(ctx), organizationID, regionID, request.Spec.FlavorId, request.Spec.ImageId)
	if err != nil {
		return nil, err
	}

	if err := c.validateSecurityGroups(ctx, request.Spec.Networking); err != nil {
		return nil, err
	}

	if err := validateUserDataForSSHCertificateAuthority(request.Spec.SshCertificateAuthorityId, request.Spec.UserData); err != nil {
		return nil, err
	}

	if err := c.validateSSHCertificateAuthorityReference(principal.NewImpersonateContext(ctx), organizationID, projectID, request.Spec.SshCertificateAuthorityId); err != nil {
		return nil, err
	}

	return flavor, nil
}

func (c *Client) validateUpdateRequest(ctx context.Context, request *computeapi.InstanceUpdate, organizationID identityids.OrganizationID, projectID identityids.ProjectID) error {
	if err := c.validateSecurityGroups(ctx, request.Spec.Networking); err != nil {
		return err
	}

	if err := validateUserDataForSSHCertificateAuthority(request.Spec.SshCertificateAuthorityId, request.Spec.UserData); err != nil {
		return err
	}

	if err := c.validateSSHCertificateAuthorityReference(principal.NewImpersonateContext(ctx), organizationID, projectID, request.Spec.SshCertificateAuthorityId); err != nil {
		return err
	}

	return nil
}

func (c *Client) generate(ctx context.Context, in *computeapi.InstanceUpdate, organizationID identityids.OrganizationID, projectID identityids.ProjectID, regionID regionids.RegionID, networkID regionids.NetworkID) (*computev1.ComputeInstance, error) {
	networking, err := GenerateNetworking(in.Spec.Networking)
	if err != nil {
		return nil, err
	}

	// The network ID is a UUID-backed typed ID; the deterministic metadata builder
	// keys the instance namespace off it.
	networkNamespace := uuid.UUID(networkID)

	var sshCertificateAuthorityID *string
	if in.Spec.SshCertificateAuthorityId != nil {
		sshCertificateAuthorityID = ptr.To(in.Spec.SshCertificateAuthorityId.String())
	}

	out := &computev1.ComputeInstance{
		ObjectMeta: conversion.NewDeterministicObjectMetadata(&in.Metadata, c.namespace, networkNamespace, in.Metadata.Name).
			WithOrganization(organizationID.String()).
			WithProject(projectID.String()).
			WithLabel(regionconstants.RegionLabel, regionID.String()).
			WithLabel(regionconstants.NetworkLabel, networkID.String()).
			Get(),
		Spec: computev1.ComputeInstanceSpec{
			Tags: conversion.GenerateTagList(in.Metadata.Tags),
			MachineGeneric: corev1.MachineGeneric{
				FlavorID: in.Spec.FlavorId.String(),
				ImageID:  in.Spec.ImageId.String(),
			},
			Networking:                networking,
			SSHCertificateAuthorityID: sshCertificateAuthorityID,
			UserData:                  GenerateUserData(in.Spec.UserData),
		},
	}

	if err := util.InjectUserPrincipal(ctx, organizationID, projectID); err != nil {
		return nil, fmt.Errorf("%w: unable to set principal information", err)
	}

	if err := common.SetIdentityMetadata(ctx, &out.ObjectMeta); err != nil {
		return nil, fmt.Errorf("%w: failed to set identity metadata", err)
	}

	return out, nil
}

func (c *Client) List(ctx context.Context, params computeapi.GetApiV2InstancesParams) (computeapi.InstancesRead, error) {
	var err error

	selector := labels.Everything()

	selector, err = rbac.AddOrganizationAndProjectIDQuery(ctx, selector, util.OrganizationIDQuery(params.OrganizationID), util.ProjectIDQuery(params.ProjectID))
	if err != nil {
		return nil, fmt.Errorf("%w: failed to add identity label selector", err)
	}

	selector, err = util.AddRegionIDQuery(selector, params.RegionID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to add region label selector", err)
	}

	selector, err = util.AddNetworkIDQuery(selector, params.NetworkID)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to add network label selector", err)
	}

	options := &client.ListOptions{
		Namespace:     c.namespace,
		LabelSelector: selector,
	}

	result := &computev1.ComputeInstanceList{}

	if err := c.client.List(ctx, result, options); err != nil {
		return nil, fmt.Errorf("%w: unable to list instances", err)
	}

	tagSelector, err := coreutil.DecodeTagSelectorParam(params.Tag)
	if err != nil {
		return nil, err
	}

	result.Items = slices.DeleteFunc(result.Items, func(resource computev1.ComputeInstance) bool {
		if !resource.Spec.Tags.ContainsAll(tagSelector) {
			return true
		}

		// Scope is recovered from the resource's labels (plain strings); parse to typed
		// IDs for the scope check and fail closed (drop the resource) if malformed.
		organizationID, projectID, err := scopeFromLabels(resource.Labels)
		if err != nil {
			return true
		}

		return rbac.AllowProjectScopeID(ctx, "compute:instances", identityapi.Read, organizationID, projectID) != nil
	})

	slices.SortStableFunc(result.Items, func(a, b computev1.ComputeInstance) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return convertList(result)
}

func (c *Client) generateAllocation(flavor *regionapi.Flavor, publicIP bool) identityapi.ResourceAllocationList {
	var gpus int

	if flavor.Spec.Gpu != nil {
		gpus = flavor.Spec.Gpu.PhysicalCount
	}

	var floatingips int

	if publicIP {
		floatingips = 1
	}

	required := identityapi.ResourceAllocationList{
		{
			Kind:      "servers",
			Committed: 1,
		},
		{
			Kind:      "gpus",
			Committed: gpus,
		},
		{
			Kind:      "floatingips",
			Committed: floatingips,
		},
	}

	return required
}

func (c *Client) getFlavor(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID, id regionids.FlavorID) (*regionapi.Flavor, error) {
	resources, err := region.New(c.region).Flavors(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	match := func(resource regionapi.Flavor) bool {
		return resource.Metadata.Id == id.String()
	}

	index := slices.IndexFunc(resources, match)
	if index < 0 {
		return nil, errors.OAuth2InvalidRequest("requested flavor does not exist or is not accessible")
	}

	return &resources[index], nil
}

func (c *Client) getImage(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID, id regionids.ImageID) (*regionapi.Image, error) {
	resources, err := region.New(c.region).Images(ctx, organizationID, regionID)
	if err != nil {
		return nil, err
	}

	match := func(resource regionapi.Image) bool {
		return resource.Metadata.Id == id.String()
	}

	index := slices.IndexFunc(resources, match)
	if index < 0 {
		return nil, errors.OAuth2InvalidRequest("requested image does not exist or is not accessible")
	}

	return &resources[index], nil
}

func (c *Client) validateSecurityGroups(ctx context.Context, networking *computeapi.InstanceNetworking) error {
	if networking == nil || networking.SecurityGroups == nil {
		return nil
	}

	for _, id := range *networking.SecurityGroups {
		// Security group IDs arrive as a list of plain strings (stored/echoed from
		// the read path); parse each to the typed ID, failing closed if malformed.
		securityGroupID, err := regionids.ParseSecurityGroupID(id)
		if err != nil {
			return err
		}

		if _, err := region.GetSecurityGroup(ctx, c.region, securityGroupID); err != nil {
			return err
		}
	}

	return nil
}

//nolint:unparam
func (c *Client) getAndValidateFlavorAndImage(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID, flavorID regionids.FlavorID, imageID regionids.ImageID) (*regionapi.Flavor, *regionapi.Image, error) {
	flavor, err := c.getFlavor(ctx, organizationID, regionID, flavorID)
	if err != nil {
		return nil, nil, err
	}

	image, err := c.getImage(ctx, organizationID, regionID, imageID)
	if err != nil {
		return nil, nil, err
	}

	if image.Status.State != regionapi.ImageStateReady {
		return nil, nil, errors.OAuth2InvalidRequest("Image is not in a ready state")
	}

	if flavor.Spec.Architecture != image.Spec.Architecture {
		return nil, nil, errors.OAuth2InvalidRequest("CPU architecture of flavor (", flavor.Spec.Architecture, ") does not match that of the image (", image.Spec.Architecture, "}")
	}

	if flavor.Spec.Disk < image.Spec.SizeGiB {
		return nil, nil, errors.OAuth2InvalidRequest("Flavor disk (", flavor.Spec.Disk, " GIB) is too small for the image (", image.Spec.SizeGiB, " GiB)")
	}

	if err := ValidateVirtualization(flavor, image); err != nil {
		return nil, nil, err
	}

	return flavor, image, nil
}

func ValidateVirtualization(flavor *regionapi.Flavor, image *regionapi.Image) error {
	flavorBaremetal := flavor.Spec.Baremetal != nil && *flavor.Spec.Baremetal

	switch image.Spec.Virtualization {
	case regionapi.ImageVirtualizationAny:
		// compatible with both baremetal and VM flavors
	case regionapi.ImageVirtualizationBaremetal:
		if !flavorBaremetal {
			return errors.OAuth2InvalidRequest("image requires a baremetal flavor")
		}
	case regionapi.ImageVirtualizationVirtualized:
		if flavorBaremetal {
			return errors.OAuth2InvalidRequest("image requires a virtualized flavor")
		}
	}

	return nil
}

type createSaga struct {
	client   *Client
	resource *computev1.ComputeInstance
	flavor   *regionapi.Flavor
}

func newCreateSaga(client *Client, resource *computev1.ComputeInstance, flavor *regionapi.Flavor) *createSaga {
	return &createSaga{
		client:   client,
		resource: resource,
		flavor:   flavor,
	}
}

func (s *createSaga) createAllocation(ctx context.Context) error {
	required := s.client.generateAllocation(s.flavor, s.resource.PublicIPEnabled())

	return identityclient.NewAllocations(s.client.client, s.client.identity).Create(ctx, s.resource, required)
}

func (s *createSaga) deleteAllocation(ctx context.Context) error {
	return identityclient.NewAllocations(s.client.client, s.client.identity).Delete(ctx, s.resource)
}

func (s *createSaga) createInstance(ctx context.Context) error {
	if err := s.client.client.Create(ctx, s.resource); err != nil {
		if kerrors.IsAlreadyExists(err) {
			return errors.HTTPConflict()
		}

		return fmt.Errorf("%w: unable to create instance", err)
	}

	return nil
}

func (s *createSaga) Actions() []saga.Action {
	return []saga.Action{
		saga.NewAction("create quota allocation", s.createAllocation, s.deleteAllocation),
		saga.NewAction("create instance", s.createInstance, nil),
	}
}

func (c *Client) Create(ctx context.Context, request *computeapi.InstanceCreate) (*computeapi.InstanceRead, error) {
	organizationID := request.Spec.OrganizationId
	projectID := request.Spec.ProjectId

	if err := rbac.AllowProjectScopeCreateID(ctx, c.identity, "compute:instances", identityapi.Create, organizationID, projectID); err != nil {
		return nil, err
	}

	// Inject the org/project into the principal so the region service can resolve
	// the user's scoped ACL, then impersonate so region enforces ReBAC on the network
	// rather than us doing a manual org/project ownership check here.
	if err := util.InjectUserPrincipal(ctx, organizationID, projectID); err != nil {
		return nil, err
	}

	network, err := region.GetNetwork(principal.NewImpersonateContext(ctx), c.region, request.Spec.NetworkId)
	if err != nil {
		return nil, err
	}

	// The region ID comes from the region read model (a string); parse it to the
	// typed ID for the downstream region calls, failing closed if malformed.
	regionID, err := regionids.ParseRegionID(network.Status.RegionId)
	if err != nil {
		return nil, err
	}

	flavor, err := c.validateCreateRequest(ctx, request, organizationID, projectID, regionID)
	if err != nil {
		return nil, err
	}

	updateRequest, err := convertCreateToUpdateRequest(request)
	if err != nil {
		return nil, err
	}

	resource, err := c.generate(ctx, updateRequest, organizationID, projectID, regionID, request.Spec.NetworkId)
	if err != nil {
		return nil, err
	}

	s := newCreateSaga(c, resource, flavor)

	if err := saga.Run(ctx, s); err != nil {
		return nil, err
	}

	return convert(resource)
}

func (c *Client) GetRaw(ctx context.Context, instanceID computeids.InstanceID) (*computev1.ComputeInstance, error) {
	result := &computev1.ComputeInstance{}

	if err := c.client.Get(ctx, client.ObjectKey{Namespace: c.namespace, Name: instanceID.String()}, result); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.HTTPNotFound().WithError(err)
		}

		return nil, fmt.Errorf("%w: unable to lookup instance", err)
	}

	organizationID, projectID, err := scopeFromLabels(result.Labels)
	if err != nil {
		return nil, err
	}

	if err := rbac.AllowProjectScopeID(ctx, "compute:instances", identityapi.Read, organizationID, projectID); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) Get(ctx context.Context, instanceID computeids.InstanceID) (*computeapi.InstanceRead, error) {
	result, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	return convert(result)
}

type updateSaga struct {
	client        *Client
	current       *computev1.ComputeInstance
	updated       *computev1.ComputeInstance
	currentFlavor *regionapi.Flavor
	flavor        *regionapi.Flavor
}

func newUpdateSaga(client *Client, current, updated *computev1.ComputeInstance, currentFlavor, flavor *regionapi.Flavor) *updateSaga {
	return &updateSaga{
		client:        client,
		current:       current,
		updated:       updated,
		currentFlavor: currentFlavor,
		flavor:        flavor,
	}
}

func (s *updateSaga) updateAllocation(ctx context.Context) error {
	required := s.client.generateAllocation(s.flavor, s.updated.PublicIPEnabled())

	return identityclient.NewAllocations(s.client.client, s.client.identity).Update(ctx, s.current, required)
}

func (s *updateSaga) revertAllocation(ctx context.Context) error {
	required := s.client.generateAllocation(s.currentFlavor, s.current.PublicIPEnabled())

	return identityclient.NewAllocations(s.client.client, s.client.identity).Update(ctx, s.current, required)
}

func (s *updateSaga) updateInstance(ctx context.Context) error {
	if err := s.client.client.Patch(ctx, s.updated, client.MergeFromWithOptions(s.current, &client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("%w: unable to update instance", err)
	}

	return nil
}

func (s *updateSaga) Actions() []saga.Action {
	return []saga.Action{
		saga.NewAction("update quota allocation", s.updateAllocation, s.revertAllocation),
		saga.NewAction("update instance", s.updateInstance, nil),
	}
}

func (c *Client) resolveUpdateFlavors(ctx context.Context, organizationID identityids.OrganizationID, regionID regionids.RegionID, current *computev1.ComputeInstance, request *computeapi.InstanceUpdate) (*regionapi.Flavor, *regionapi.Flavor, error) {
	// The current flavor/image IDs are read back from the CRD spec (strings); parse
	// them to typed IDs, failing closed if malformed.
	currentFlavorID, err := regionids.ParseFlavorID(current.Spec.FlavorID)
	if err != nil {
		return nil, nil, err
	}

	currentImageID, err := regionids.ParseImageID(current.Spec.ImageID)
	if err != nil {
		return nil, nil, err
	}

	currentFlavor, _, err := c.getAndValidateFlavorAndImage(ctx, organizationID, regionID, currentFlavorID, currentImageID)
	if err != nil {
		return nil, nil, err
	}

	flavor, _, err := c.getAndValidateFlavorAndImage(ctx, organizationID, regionID, request.Spec.FlavorId, request.Spec.ImageId)
	if err != nil {
		return nil, nil, err
	}

	return currentFlavor, flavor, nil
}

// authorizeUpdate recovers the typed scope, region and network IDs from the
// instance's labels (plain strings, failing closed if malformed) and authorizes
// the update against the project scope.
func (c *Client) authorizeUpdate(ctx context.Context, current *computev1.ComputeInstance) (identityids.OrganizationID, identityids.ProjectID, regionids.RegionID, regionids.NetworkID, error) {
	organizationID, projectID, err := scopeFromLabels(current.Labels)
	if err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, regionids.RegionID{}, regionids.NetworkID{}, err
	}

	regionID, err := regionids.ParseRegionID(current.Labels[regionconstants.RegionLabel])
	if err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, regionids.RegionID{}, regionids.NetworkID{}, err
	}

	networkID, err := regionids.ParseNetworkID(current.Labels[regionconstants.NetworkLabel])
	if err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, regionids.RegionID{}, regionids.NetworkID{}, err
	}

	if err := rbac.AllowProjectScopeID(ctx, "compute:instances", identityapi.Update, organizationID, projectID); err != nil {
		return identityids.OrganizationID{}, identityids.ProjectID{}, regionids.RegionID{}, regionids.NetworkID{}, err
	}

	return organizationID, projectID, regionID, networkID, nil
}

func (c *Client) Update(ctx context.Context, instanceID computeids.InstanceID, request *computeapi.InstanceUpdate) (*computeapi.InstanceRead, error) {
	current, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	organizationID, projectID, regionID, networkID, err := c.authorizeUpdate(ctx, current)
	if err != nil {
		return nil, err
	}

	if current.DeletionTimestamp != nil {
		return nil, errors.OAuth2InvalidRequest("server is being deleted")
	}

	if request.Metadata.Name != current.Labels[coreconstants.NameLabel] {
		return nil, errors.HTTPUnprocessableContent("instance names are immutable")
	}

	if err := util.InjectUserPrincipal(ctx, organizationID, projectID); err != nil {
		return nil, err
	}

	currentFlavor, flavor, err := c.resolveUpdateFlavors(principal.NewImpersonateContext(ctx), organizationID, regionID, current, request)
	if err != nil {
		return nil, err
	}

	if err := c.validateUpdateRequest(ctx, request, organizationID, projectID); err != nil {
		return nil, err
	}

	required, err := c.generate(ctx, request, organizationID, projectID, regionID, networkID)
	if err != nil {
		return nil, err
	}

	// Preserve allocation information.
	// TODO: this is smell code, perhaps we want to rejig the interface to accept both
	// current and updated resources, and that can transparently do the preservation.
	required.Annotations[coreconstants.AllocationAnnotation] = current.Annotations[coreconstants.AllocationAnnotation]

	updated := current.DeepCopy()
	updated.Labels = required.Labels
	updated.Annotations = required.Annotations
	updated.Spec = required.Spec

	s := newUpdateSaga(c, current, updated, currentFlavor, flavor)

	if err := saga.Run(ctx, s); err != nil {
		return nil, err
	}

	return convert(s.updated)
}

func (c *Client) Delete(ctx context.Context, instanceID computeids.InstanceID) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	if resource.DeletionTimestamp != nil {
		return nil
	}

	organizationID, projectID, err := scopeFromLabels(resource.Labels)
	if err != nil {
		return err
	}

	if err := rbac.AllowProjectScopeID(ctx, "compute:instances", identityapi.Delete, organizationID, projectID); err != nil {
		return err
	}

	if err := c.client.Delete(ctx, resource); err != nil {
		if kerrors.IsNotFound(err) {
			return errors.HTTPNotFound().WithError(err)
		}

		return fmt.Errorf("%w: unable to delete instance", err)
	}

	return nil
}

func (c *Client) serverID(ctx context.Context, instance *computev1.ComputeInstance) (regionids.ServerID, error) {
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
			constants.InstanceLabel + "=" + instance.Name,
		},
	}

	response, err := c.region.GetApiV2ServersWithResponse(ctx, params)
	if err != nil {
		return regionids.ServerID{}, fmt.Errorf("%w: unable to query servers for instance", err)
	}

	if response.StatusCode() != http.StatusOK {
		return regionids.ServerID{}, fmt.Errorf("%w: unable to query servers for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	servers := *response.JSON200

	if len(servers) != 1 {
		return regionids.ServerID{}, fmt.Errorf("%w: unable to query server for instance - incorrect number of matches", coreerrors.ErrConsistency)
	}

	// The server ID comes from the region read model (a string); parse it to the
	// typed ID used by the region server-action calls, failing closed if malformed.
	return regionids.ParseServerID(servers[0].Metadata.Id)
}

func (c *Client) SSHKey(ctx context.Context, instanceID computeids.InstanceID) (*regionapi.SshKey, error) {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return nil, err
	}

	response, err := c.region.GetApiV2ServersServerIDSshkeyWithResponse(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to fetch SSH key for instance", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: unable to fetch SSH key for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return response.JSON200, nil
}

func (c *Client) Start(ctx context.Context, instanceID computeids.InstanceID) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return err
	}

	response, err := c.region.PostApiV2ServersServerIDStartWithResponse(ctx, serverID)
	if err != nil {
		return fmt.Errorf("%w: unable to start server for instance", err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return fmt.Errorf("%w: unable to start server for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return nil
}

func (c *Client) Stop(ctx context.Context, instanceID computeids.InstanceID) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return err
	}

	response, err := c.region.PostApiV2ServersServerIDStopWithResponse(ctx, serverID)
	if err != nil {
		return fmt.Errorf("%w: unable to stop server for instance", err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return fmt.Errorf("%w: unable to stop server for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return nil
}

func (c *Client) Reboot(ctx context.Context, instanceID computeids.InstanceID, params computeapi.PostApiV2InstancesInstanceIDRebootParams) error {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return err
	}

	// TODO: we should just pass this through...
	if params.Hard != nil && *params.Hard {
		response, err := c.region.PostApiV2ServersServerIDHardrebootWithResponse(ctx, serverID)
		if err != nil {
			return fmt.Errorf("%w: unable to reboot server for instance", err)
		}

		if response.StatusCode() != http.StatusAccepted {
			return fmt.Errorf("%w: unable to reboot server for instance - incorrect status code", coreerrors.ErrAPIStatus)
		}

		return nil
	}

	response, err := c.region.PostApiV2ServersServerIDSoftrebootWithResponse(ctx, serverID)
	if err != nil {
		return fmt.Errorf("%w: unable to reboot server for instance", err)
	}

	if response.StatusCode() != http.StatusAccepted {
		return fmt.Errorf("%w: unable to reboot server for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return nil
}

// dropSystemTags removes any tags that are reserved for this service to use.
func dropSystemTags(meta *coreapi.ResourceMetadata) {
	if meta.Tags == nil {
		return
	}

	tags := *meta.Tags
	tags = slices.DeleteFunc(tags, func(t coreapi.Tag) bool {
		return strings.HasPrefix(t.Name, constants.SystemTagPrefix)
	})

	meta.Tags = &tags
}

func setTag(meta *coreapi.ResourceMetadata, tag, value string) {
	var tags coreapi.TagList
	if requestTags := meta.Tags; requestTags != nil {
		tags = *requestTags
	}

	tags = append(tags, coreapi.Tag{
		Name:  tag,
		Value: value,
	})

	meta.Tags = &tags
}

func (c *Client) Snapshot(ctx context.Context, instanceID computeids.InstanceID, params computeapi.InstanceSnapshotCreate) (*regionapi.ImageResponse, error) {
	// This implicitly checks read permission on the instance in question.
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return nil, err
	}

	var requestBody regionapi.SnapshotCreate
	requestBody.Metadata = params.Metadata

	dropSystemTags(&requestBody.Metadata)
	setTag(&requestBody.Metadata, constants.InstanceIDTag, instanceID.String())

	requestBody.Spec = regionapi.SnapshotCreateSpec{}

	response, err := c.region.PostApiV2ServersServerIDSnapshotWithResponse(ctx, serverID, requestBody)
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("%w: unable to create snapshot for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return response.JSON201, nil
}

func (c *Client) ConsoleOutput(ctx context.Context, instanceID computeids.InstanceID, params computeapi.GetApiV2InstancesInstanceIDConsoleoutputParams) (*regionapi.ConsoleOutputResponse, error) {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return nil, err
	}

	var requestParams *regionapi.GetApiV2ServersServerIDConsoleoutputParams

	if params.Length != nil {
		requestParams = &regionapi.GetApiV2ServersServerIDConsoleoutputParams{
			Length: params.Length,
		}
	}

	response, err := c.region.GetApiV2ServersServerIDConsoleoutputWithResponse(ctx, serverID, requestParams)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to get console output for instance", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: unable to get console output for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return response.JSON200, nil
}

func (c *Client) ConsoleSession(ctx context.Context, instanceID computeids.InstanceID) (*regionapi.ConsoleSessionResponse, error) {
	resource, err := c.GetRaw(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	serverID, err := c.serverID(ctx, resource)
	if err != nil {
		return nil, err
	}

	response, err := c.region.GetApiV2ServersServerIDConsolesessionsWithResponse(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: unable to start console session for instance", err)
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: unable to start console session for instance - incorrect status code", coreerrors.ErrAPIStatus)
	}

	return response.JSON200, nil
}
