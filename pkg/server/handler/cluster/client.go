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

package cluster

import (
	"context"
	goerrors "errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	"github.com/unikorn-cloud/compute/pkg/server/handler/identity"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	"github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/conversion"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	coreutil "github.com/unikorn-cloud/core/pkg/server/util"
	coreapiutils "github.com/unikorn-cloud/core/pkg/util/api"
	"github.com/unikorn-cloud/identity/pkg/handler/common"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	"github.com/unikorn-cloud/identity/pkg/principal"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrConsistency = goerrors.New("consistency error")
)

type Options struct {
	NodeNetwork    net.IPNet
	DNSNameservers []net.IP
}

func (o *Options) AddFlags(f *pflag.FlagSet) {
	_, nodeNetwork, _ := net.ParseCIDR("192.168.0.0/24")

	dnsNameservers := []net.IP{net.ParseIP("8.8.8.8")}

	f.IPNetVar(&o.NodeNetwork, "default-node-network", *nodeNetwork, "Default node network to use when creating a cluster")
	f.IPSliceVar(&o.DNSNameservers, "default-dns-nameservers", dnsNameservers, "Default DNS nameserver to use when creating a cluster")
}

// Client wraps up cluster related management handling.
type Client struct {
	// client allows Compute API access.
	client client.Client

	// namespace the controller runs in.
	namespace string

	// options control various defaults and the like.
	options *Options

	// identity is a client to access the identity service.
	identity *identity.Client

	// region is a client to access regions.
	region *region.Client
}

// NewClient returns a new client with required parameters.
func NewClient(client client.Client, namespace string, options *Options, identity *identity.Client, region *region.Client) *Client {
	return &Client{
		client:    client,
		namespace: namespace,
		options:   options,
		identity:  identity,
		region:    region,
	}
}

// List returns all clusters owned by the implicit control plane.
func (c *Client) List(ctx context.Context, organizationID string, params openapi.GetApiV1OrganizationsOrganizationIDClustersParams) (openapi.ComputeClusters, error) {
	requirement, err := labels.NewRequirement(constants.OrganizationLabel, selection.Equals, []string{organizationID})
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to build label selector").WithError(err)
	}

	selector := labels.NewSelector()
	selector = selector.Add(*requirement)

	options := &client.ListOptions{
		LabelSelector: selector,
	}

	result := &unikornv1.ComputeClusterList{}

	if err := c.client.List(ctx, result, options); err != nil {
		return nil, errors.OAuth2ServerError("failed to list clusters").WithError(err)
	}

	tagSelector, err := coreutil.DecodeTagSelectorParam(params.Tag)
	if err != nil {
		return nil, err
	}

	result.Items = slices.DeleteFunc(result.Items, func(resource unikornv1.ComputeCluster) bool {
		return !resource.Spec.Tags.ContainsAll(tagSelector)
	})

	slices.SortStableFunc(result.Items, func(a, b unikornv1.ComputeCluster) int {
		return strings.Compare(a.Name, b.Name)
	})

	return newGenerator(c.client, c.options, c.region, "", organizationID, "", nil).convertList(result), nil
}

func (c *Client) Get(ctx context.Context, organizationID, projectID, clusterID string) (*openapi.ComputeClusterRead, error) {
	namespace, err := common.ProjectNamespace(ctx, c.client, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	result, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return nil, err
	}

	return newGenerator(c.client, c.options, c.region, "", organizationID, "", nil).convert(result), nil
}

// get returns the cluster.
func (c *Client) get(ctx context.Context, namespace, clusterID string) (*unikornv1.ComputeCluster, error) {
	result := &unikornv1.ComputeCluster{}

	if err := c.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterID}, result); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, errors.HTTPNotFound().WithError(err)
		}

		return nil, errors.OAuth2ServerError("unable to get cluster").WithError(err)
	}

	return result, nil
}

func (c *Client) generateAllocations(ctx context.Context, organizationID string, resource *unikornv1.ComputeCluster) (*identityapi.AllocationWrite, error) {
	flavors, err := c.region.Flavors(ctx, organizationID, resource.Spec.RegionID)
	if err != nil {
		return nil, err
	}

	var serversCommitted int

	var gpusCommitted int

	// NOTE: the control plane is "free".
	for _, pool := range resource.Spec.WorkloadPools.Pools {
		serversMinimum := pool.Replicas

		serversCommitted += serversMinimum

		flavorByID := func(f regionapi.Flavor) bool {
			return f.Metadata.Id == pool.FlavorID
		}

		index := slices.IndexFunc(flavors, flavorByID)
		if index < 0 {
			return nil, fmt.Errorf("%w: flavorID does not exist", ErrConsistency)
		}

		flavor := flavors[index]

		if flavor.Spec.Gpu != nil {
			gpusCommitted += serversMinimum * flavor.Spec.Gpu.PhysicalCount
		}
	}

	request := &identityapi.AllocationWrite{
		Metadata: coreapi.ResourceWriteMetadata{
			Name: constants.UndefinedName,
		},
		Spec: identityapi.AllocationSpec{
			Kind: "computecluster",
			Id:   resource.Name,
			Allocations: identityapi.ResourceAllocationList{
				{
					Kind:      "clusters",
					Committed: 1,
					Reserved:  0,
				},
				{
					Kind:      "servers",
					Committed: serversCommitted,
					Reserved:  0,
				},
				{
					Kind:      "gpus",
					Committed: gpusCommitted,
					Reserved:  0,
				},
			},
		},
	}

	return request, nil
}

func (c *Client) createAllocation(ctx context.Context, resource *unikornv1.ComputeCluster) (*identityapi.AllocationRead, error) {
	principal, err := principal.GetPrincipal(ctx)
	if err != nil {
		return nil, err
	}

	allocations, err := c.generateAllocations(ctx, principal.OrganizationID, resource)
	if err != nil {
		return nil, err
	}

	client, err := c.identity.Client(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsWithResponse(ctx, principal.OrganizationID, principal.ProjectID, *allocations)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() != http.StatusCreated {
		return nil, coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return resp.JSON201, nil
}

func (c *Client) updateAllocation(ctx context.Context, resource *unikornv1.ComputeCluster) error {
	principal, err := principal.GetPrincipal(ctx)
	if err != nil {
		return err
	}

	allocations, err := c.generateAllocations(ctx, principal.OrganizationID, resource)
	if err != nil {
		return err
	}

	client, err := c.identity.Client(ctx)
	if err != nil {
		return err
	}

	resp, err := client.PutApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(ctx, principal.OrganizationID, principal.ProjectID, resource.Annotations[constants.AllocationAnnotation], *allocations)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) deleteAllocation(ctx context.Context, allocationID string) error {
	client, err := c.identity.Client(ctx)
	if err != nil {
		return err
	}

	principal, err := principal.GetPrincipal(ctx)
	if err != nil {
		return err
	}

	resp, err := client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDAllocationsAllocationIDWithResponse(ctx, principal.OrganizationID, principal.ProjectID, allocationID)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusAccepted {
		return coreapiutils.ExtractError(resp.StatusCode(), resp)
	}

	return nil
}

func (c *Client) createIdentity(ctx context.Context, organizationID, projectID, regionID, clusterID string) (*regionapi.IdentityRead, error) {
	tags := coreapi.TagList{
		coreapi.Tag{
			Name:  constants.ComputeClusterLabel,
			Value: clusterID,
		},
	}

	request := regionapi.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesJSONRequestBody{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        "compute-cluster-" + clusterID,
			Description: ptr.To("Identity for Compute cluster " + clusterID),
			Tags:        &tags,
		},
		Spec: regionapi.IdentityWriteSpec{
			RegionId: regionID,
		},
	}

	client, err := c.region.Client(ctx)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to create region client").WithError(err)
	}

	resp, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesWithResponse(ctx, organizationID, projectID, request)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to create identity").WithError(err)
	}

	if resp.StatusCode() != http.StatusCreated {
		return nil, errors.OAuth2ServerError("unable to create identity").WithError(coreapiutils.ExtractError(resp.StatusCode(), resp))
	}

	return resp.JSON201, nil
}

func (c *Client) createNetworkOpenstack(ctx context.Context, organizationID, projectID string, cluster *unikornv1.ComputeCluster, identity *regionapi.IdentityRead) (*regionapi.NetworkRead, error) {
	tags := coreapi.TagList{
		coreapi.Tag{
			Name:  constants.ComputeClusterLabel,
			Value: cluster.Name,
		},
	}

	dnsNameservers := make([]string, len(cluster.Spec.Network.DNSNameservers))

	for i, ip := range cluster.Spec.Network.DNSNameservers {
		dnsNameservers[i] = ip.String()
	}

	request := regionapi.NetworkWrite{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        "compute-cluster-" + cluster.Name,
			Description: ptr.To("Network for cluster " + cluster.Name),
			Tags:        &tags,
		},
		Spec: &regionapi.NetworkWriteSpec{
			Prefix:         cluster.Spec.Network.NodeNetwork.String(),
			DnsNameservers: dnsNameservers,
		},
	}

	client, err := c.region.Client(ctx)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to create region client").WithError(err)
	}

	resp, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDNetworksWithResponse(ctx, organizationID, projectID, identity.Metadata.Id, request)
	if err != nil {
		return nil, errors.OAuth2ServerError("unable to create network").WithError(err)
	}

	if resp.StatusCode() != http.StatusCreated {
		return nil, errors.OAuth2ServerError("unable to create network").WithError(coreapiutils.ExtractError(resp.StatusCode(), resp))
	}

	return resp.JSON201, nil
}

func (c *Client) applyCloudSpecificConfiguration(ctx context.Context, organizationID, projectID string, allocation *identityapi.AllocationRead, identity *regionapi.IdentityRead, cluster *unikornv1.ComputeCluster) error {
	// Save the identity ID for later cleanup.
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}

	cluster.Annotations[constants.AllocationAnnotation] = allocation.Metadata.Id
	cluster.Annotations[constants.IdentityAnnotation] = identity.Metadata.Id

	// Provision a network for nodes to attach to.
	network, err := c.createNetworkOpenstack(ctx, organizationID, projectID, cluster, identity)
	if err != nil {
		return errors.OAuth2ServerError("failed to create physical network").WithError(err)
	}

	cluster.Annotations[constants.PhysicalNetworkAnnotation] = network.Metadata.Id

	return nil
}

func metadataMutator(required, current metav1.Object) error {
	req := required.GetAnnotations()
	if req == nil {
		req = map[string]string{}
	}

	cur := current.GetAnnotations()

	// Preserve the identity annotation and allocation.
	// NOTE: these are guarded by a validating admission policy so should exist.
	if v, ok := cur[constants.IdentityAnnotation]; ok {
		req[constants.IdentityAnnotation] = v
	}

	if v, ok := cur[constants.AllocationAnnotation]; ok {
		req[constants.AllocationAnnotation] = v
	}

	// Optionally preserve the network if one is provisioned.
	if v, ok := cur[constants.PhysicalNetworkAnnotation]; ok {
		req[constants.PhysicalNetworkAnnotation] = v
	}

	required.SetAnnotations(req)

	return nil
}

// Create creates the implicit cluster indentified by the JTW claims.
func (c *Client) Create(ctx context.Context, organizationID, projectID string, request *openapi.ComputeClusterWrite) (*openapi.ComputeClusterRead, error) {
	namespace, err := common.ProjectNamespace(ctx, c.client, organizationID, projectID)
	if err != nil {
		return nil, err
	}

	cluster, err := newGenerator(c.client, c.options, c.region, namespace.Name, organizationID, projectID, nil).generate(ctx, request)
	if err != nil {
		return nil, err
	}

	// TODO: allocations should be deleted on error beyond this point!
	allocation, err := c.createAllocation(ctx, cluster)
	if err != nil {
		return nil, errors.OAuth2ServerError("failed to create quota allocation").WithError(err)
	}

	// TODO: identities should be deleted on error beyond this point!
	identity, err := c.createIdentity(ctx, organizationID, projectID, request.Spec.RegionId, cluster.Name)
	if err != nil {
		return nil, err
	}

	if err := c.applyCloudSpecificConfiguration(ctx, organizationID, projectID, allocation, identity, cluster); err != nil {
		return nil, err
	}

	if err := c.client.Create(ctx, cluster); err != nil {
		return nil, errors.OAuth2ServerError("failed to create cluster").WithError(err)
	}

	return newGenerator(c.client, c.options, c.region, "", organizationID, "", nil).convert(cluster), nil
}

// Delete deletes the implicit cluster indentified by the JTW claims.
func (c *Client) Delete(ctx context.Context, organizationID, projectID, clusterID string) error {
	namespace, err := common.ProjectNamespace(ctx, c.client, organizationID, projectID)
	if err != nil {
		return err
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	if err := c.client.Delete(ctx, cluster); err != nil {
		return errors.OAuth2ServerError("failed to delete cluster").WithError(err)
	}

	if err := c.deleteAllocation(ctx, cluster.Annotations[constants.AllocationAnnotation]); err != nil {
		return errors.OAuth2ServerError("failed to delete quota allocation").WithError(err)
	}

	return nil
}

// Update implements read/modify/write for the cluster.
func (c *Client) Update(ctx context.Context, organizationID, projectID, clusterID string, request *openapi.ComputeClusterWrite) error {
	namespace, err := common.ProjectNamespace(ctx, c.client, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	current, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	required, err := newGenerator(c.client, c.options, c.region, namespace.Name, organizationID, projectID, current).generate(ctx, request)
	if err != nil {
		return err
	}

	if err := conversion.UpdateObjectMetadata(required, current, common.IdentityMetadataMutator, metadataMutator); err != nil {
		return errors.OAuth2ServerError("failed to merge metadata").WithError(err)
	}

	// Experience has taught me that modifying caches by accident is a bad thing
	// so be extra safe and deep copy the existing resource.
	updated := current.DeepCopy()
	updated.Labels = required.Labels
	updated.Annotations = required.Annotations
	updated.Spec = required.Spec

	if err := conversion.LogUpdate(ctx, current, updated); err != nil {
		return errors.OAuth2ServerError("failed to log update").WithError(err)
	}

	// TODO: allocations should be reverted if the patch was rejected.
	if err := c.updateAllocation(ctx, updated); err != nil {
		return errors.OAuth2ServerError("failed to update quota allocation").WithError(err)
	}

	if err := c.client.Patch(ctx, updated, client.MergeFrom(current)); err != nil {
		return errors.OAuth2ServerError("failed to patch cluster").WithError(err)
	}

	return nil
}

// Evict is pretty complicated, we need to delete the requested servers from the
// region service, and update the cluster's pools to remove those instances so they don't
// just get recreated instantly, and also update the quota allocations.  Now, if you naively
// deleted the servers, that would trigger a reconcile and a potential replacement before
// we've had a chance to update the cluster.  If you updated the cluster then you've got
// yourself a problem where we have no control over what's deleted.  So what we do is...
// Pause cluster reconciliation, kill the requested servers, update the allocations, update
// the cluster and unpause it.  Ideally everything would be a nice atomic transaction, but
// you cannot do that with Kubernetes...
func (c *Client) Evict(ctx context.Context, organizationID, projectID, clusterID string, request *openapi.EvictionWrite) error {
	namespace, err := common.New(c.client).ProjectNamespace(ctx, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	// Lookup the servers and ensure they all exist...
	servers, err := c.region.Servers(ctx, organizationID, cluster)
	if err != nil {
		return errors.OAuth2ServerError("failed to list servers").WithError(err)
	}

	servers = slices.DeleteFunc(servers, func(server regionapi.ServerRead) bool {
		return !slices.Contains(request.MachineIDs, server.Metadata.Id)
	})

	if len(servers) != len(request.MachineIDs) {
		return errors.OAuth2InvalidRequest("requested machine ID not found")
	}

	clusterToUpdate := cluster.DeepCopy()
	clusterToUpdate.Spec.Pause = true

	// Do the pause...
	if err := c.client.Patch(ctx, clusterToUpdate, client.MergeFrom(cluster)); err != nil {
		return errors.OAuth2ServerError("failed to patch cluster").WithError(err)
	}

	clusterToUnpause := clusterToUpdate.DeepCopy()

	var evictionErr error

	// If an error occurs during actual eviction, we will not persist the updated replica count back to the CRD.
	// Instead, the reconciler will detect the missing servers and recreate them, ensuring that server availability is maintained.
	// The evictionErr will then be returned in the API response to inform the caller of the failure.
	// This approach prevents failed evictions from corrupting replica counts while allowing automatic recovery.
	if err := c.evictServers(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], clusterToUpdate, servers); err != nil {
		clusterToUpdate = clusterToUnpause.DeepCopy()
		evictionErr = err
	}

	clusterToUpdate.Spec.Pause = false

	if err := c.client.Patch(ctx, clusterToUpdate, client.MergeFrom(clusterToUnpause)); err != nil {
		return errors.OAuth2ServerError("failed to patch cluster").WithError(err)
	}

	return evictionErr
}

func (c *Client) evictServers(ctx context.Context, organizationID, projectID, identityID string, resource *unikornv1.ComputeCluster, servers []regionapi.ServerRead) error {
	for i := range servers {
		server := &servers[i]

		poolName, err := util.GetWorkloadPoolTag(server.Metadata.Tags)
		if err != nil {
			return errors.OAuth2ServerError("failed to lookup server pool name")
		}

		pool, ok := resource.GetWorkloadPool(poolName)
		if !ok {
			return errors.OAuth2ServerError("failed to lookup server pool")
		}

		pool.Replicas--
	}

	for _, server := range servers {
		if err := c.region.DeleteServer(ctx, organizationID, projectID, identityID, server.Metadata.Id); err != nil {
			return err
		}
	}

	if err := c.updateAllocation(ctx, resource); err != nil {
		return errors.OAuth2ServerError("failed to update quota allocation").WithError(err)
	}

	return nil
}

func (c *Client) HardRebootMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	namespace, err := common.New(c.client).ProjectNamespace(ctx, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	if err := c.region.HardRebootServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID); err != nil {
		return errors.OAuth2ServerError("failed to hard reboot machine").WithError(err)
	}

	return nil
}

func (c *Client) SoftRebootMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	namespace, err := common.New(c.client).ProjectNamespace(ctx, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	if err := c.region.SoftRebootServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID); err != nil {
		return errors.OAuth2ServerError("failed to soft reboot machine").WithError(err)
	}

	return nil
}

func (c *Client) StartMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	namespace, err := common.New(c.client).ProjectNamespace(ctx, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	if err := c.region.StartServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID); err != nil {
		return errors.OAuth2ServerError("failed to start machine").WithError(err)
	}

	return nil
}

func (c *Client) StopMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	namespace, err := common.New(c.client).ProjectNamespace(ctx, organizationID, projectID)
	if err != nil {
		return err
	}

	if namespace.DeletionTimestamp != nil {
		return errors.OAuth2InvalidRequest("compute cluster is being deleted")
	}

	cluster, err := c.get(ctx, namespace.Name, clusterID)
	if err != nil {
		return err
	}

	if err := c.region.StopServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID); err != nil {
		return errors.OAuth2ServerError("failed to stop machine").WithError(err)
	}

	return nil
}
