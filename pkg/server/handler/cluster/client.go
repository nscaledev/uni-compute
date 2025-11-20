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
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/spf13/pflag"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeconstants "github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/openapi"
	managerutil "github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	"github.com/unikorn-cloud/compute/pkg/server/handler/region"
	"github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/conversion"
	"github.com/unikorn-cloud/core/pkg/server/util"
	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	"github.com/unikorn-cloud/identity/pkg/handler/common"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
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
	identity identityclient.APIClientGetter

	// region is a client to access regions.
	region region.ClientGetterFunc
}

// NewClient returns a new client with required parameters.
func NewClient(client client.Client, namespace string, options *Options, identity identityclient.APIClientGetter, region region.ClientGetterFunc) *Client {
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
	tagSelector, err := util.DecodeTagSelectorParam(params.Tag)
	if err != nil {
		return nil, err
	}

	opts := []client.ListOption{
		&client.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				constants.OrganizationLabel: organizationID,
			}),
		},
	}

	var list unikornv1.ComputeClusterList
	if err := c.client.List(ctx, &list, opts...); err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve comptue clusters: %w", err).
			Prefixed()

		return nil, err
	}

	list.Items = slices.DeleteFunc(list.Items, func(resource unikornv1.ComputeCluster) bool {
		return !resource.Spec.Tags.ContainsAll(tagSelector)
	})

	slices.SortStableFunc(list.Items, func(a, b unikornv1.ComputeCluster) int {
		return strings.Compare(a.Name, b.Name)
	})

	return newGenerator(c.client, c.options, region.New(c.region), "", organizationID, "", nil).convertList(&list), nil
}

func (c *Client) Get(ctx context.Context, organizationID, projectID, clusterID string) (*openapi.ComputeClusterRead, error) {
	result, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return nil, err
	}

	return newGenerator(c.client, c.options, region.New(c.region), "", organizationID, "", nil).convert(result), nil
}

// get returns the cluster.
func (c *Client) get(ctx context.Context, organizationID, projectID, clusterID string) (*unikornv1.ComputeCluster, error) {
	key := client.ObjectKey{
		Namespace: c.namespace,
		Name:      clusterID,
	}

	var cluster unikornv1.ComputeCluster
	if err := c.client.Get(ctx, key, &cluster); err != nil {
		if kerrors.IsNotFound(err) {
			err = errorsv2.NewResourceMissingError("compute cluster").
				WithCause(err).
				Prefixed()

			return nil, err
		}

		err = errorsv2.NewInternalError().
			WithCausef("failed to retrieve compute cluster: %w", err).
			Prefixed()

		return nil, err
	}

	if err := util.AssertProjectOwnership(&cluster, organizationID, projectID); err != nil {
		return nil, err
	}

	return &cluster, nil
}

func (c *Client) generateAllocations(ctx context.Context, organizationID string, resource *unikornv1.ComputeCluster) (identityapi.ResourceAllocationList, error) {
	flavors, err := region.New(c.region).Flavors(ctx, organizationID, resource.Spec.RegionID)
	if err != nil {
		return nil, err
	}

	var serversCommitted int

	var gpusCommitted int

	// NOTE: the control plane is "free".
	for _, pool := range resource.Spec.WorkloadPools.Pools {
		serversMinimum := pool.Replicas

		serversCommitted += serversMinimum

		isTargetFlavor := func(f regionapi.Flavor) bool {
			return f.Metadata.Id == pool.FlavorID
		}

		index := slices.IndexFunc(flavors, isTargetFlavor)
		if index < 0 {
			// Return an internal error here, as the flavor should have been validated earlier.
			err = errorsv2.NewInternalError().
				WithSimpleCause("no matching flavor found when generating allocations").
				Prefixed()

			return nil, err
		}

		flavor := flavors[index]

		if flavor.Spec.Gpu != nil {
			gpusCommitted += serversMinimum * flavor.Spec.Gpu.PhysicalCount
		}
	}

	allocations := identityapi.ResourceAllocationList{
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
	}

	return allocations, nil
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

	regionAPIClient, err := region.New(c.region).Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesWithResponse(ctx, organizationID, projectID, request)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to create identity: %w", err).
			Prefixed()

		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.IdentityRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusCreated)
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

	regionAPIClient, err := region.New(c.region).Client(ctx)
	if err != nil {
		return nil, err
	}

	response, err := regionAPIClient.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDNetworksWithResponse(ctx, organizationID, projectID, identity.Metadata.Id, request)
	if err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to create network: %w", err).
			Prefixed()

		return nil, err
	}

	return coreapi.ParseJSONPointerResponse[regionapi.NetworkRead](response.HTTPResponse.Header, response.Body, response.StatusCode(), http.StatusCreated)
}

func (c *Client) applyCloudSpecificConfiguration(ctx context.Context, organizationID, projectID string, identity *regionapi.IdentityRead, cluster *unikornv1.ComputeCluster) error {
	// Save the identity ID for later cleanup.
	if cluster.Annotations == nil {
		cluster.Annotations = map[string]string{}
	}

	cluster.Annotations[constants.IdentityAnnotation] = identity.Metadata.Id

	// Provision a network for nodes to attach to.
	network, err := c.createNetworkOpenstack(ctx, organizationID, projectID, cluster, identity)
	if err != nil {
		return err
	}

	cluster.Labels[constants.NetworkLabel] = network.Metadata.Id

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

	required.SetAnnotations(req)

	req = required.GetLabels()
	if req == nil {
		req = map[string]string{}
	}

	cur = current.GetLabels()

	// Preserve the network.
	if v, ok := cur[constants.NetworkLabel]; ok {
		req[constants.NetworkLabel] = v
	}

	required.SetLabels(req)

	return nil
}

// Create creates the implicit cluster identified by the JTW claims.
func (c *Client) Create(ctx context.Context, organizationID, projectID string, request *openapi.ComputeClusterWrite) (*openapi.ComputeClusterRead, error) {
	cluster, err := newGenerator(c.client, c.options, region.New(c.region), c.namespace, organizationID, projectID, nil).generate(ctx, request)
	if err != nil {
		return nil, err
	}

	// TODO: identities should be deleted on error beyond this point!
	identity, err := c.createIdentity(ctx, organizationID, projectID, request.Spec.RegionId, cluster.Name)
	if err != nil {
		return nil, err
	}

	allocations, err := c.generateAllocations(ctx, organizationID, cluster)
	if err != nil {
		return nil, err
	}

	if err := identityclient.NewAllocations(c.client, c.identity).Create(ctx, cluster, allocations); err != nil {
		return nil, err
	}

	if err := c.applyCloudSpecificConfiguration(ctx, organizationID, projectID, identity, cluster); err != nil {
		return nil, err
	}

	if err := c.client.Create(ctx, cluster); err != nil {
		err = errorsv2.NewInternalError().
			WithCausef("failed to create compute cluster: %w", err).
			Prefixed()

		return nil, err
	}

	return newGenerator(c.client, c.options, region.New(c.region), "", organizationID, "", nil).convert(cluster), nil
}

// Delete deletes the implicit cluster identified by the JTW claims.
func (c *Client) Delete(ctx context.Context, organizationID, projectID, clusterID string) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return nil
	}

	if err := c.client.Delete(ctx, cluster); err != nil {
		if kerrors.IsNotFound(err) {
			return errorsv2.NewResourceMissingError("compute cluster").
				WithCause(err).
				Prefixed()
		}

		return errorsv2.NewInternalError().
			WithCausef("failed to delete compute cluster: %w", err).
			Prefixed()
	}

	return nil
}

// Update implements read/modify/write for the cluster.
func (c *Client) Update(ctx context.Context, organizationID, projectID, clusterID string, request *openapi.ComputeClusterWrite) error {
	current, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if current.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	required, err := newGenerator(c.client, c.options, region.New(c.region), c.namespace, organizationID, projectID, current).generate(ctx, request)
	if err != nil {
		return err
	}

	if err := conversion.UpdateObjectMetadata(required, current, common.IdentityMetadataMutator, metadataMutator); err != nil {
		return err
	}

	// Experience has taught me that modifying caches by accident is a bad thing
	// so be extra safe and deep copy the existing resource.
	updated := current.DeepCopy()
	updated.Labels = required.Labels
	updated.Annotations = required.Annotations
	updated.Spec = required.Spec

	if err := conversion.LogUpdate(ctx, current, updated); err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to log compute cluster update: %w", err).
			Prefixed()
	}

	allocations, err := c.generateAllocations(ctx, organizationID, updated)
	if err != nil {
		return err
	}

	if err := identityclient.NewAllocations(c.client, c.identity).Update(ctx, updated, allocations); err != nil {
		return err
	}

	if err := c.client.Patch(ctx, updated, client.MergeFrom(current)); err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to patch compute cluster: %w", err).
			Prefixed()
	}

	return nil
}

// Evict is pretty complicated, we need to delete the requested servers from the
// region service, and update the cluster's pools to remove those instances so they don't
// just get recreated instantly.  What we do is scale down the cluster, but annotate it
// with a list of server IDs we'd like to delete.
//
//nolint:cyclop
func (c *Client) Evict(ctx context.Context, organizationID, projectID, clusterID string, request *openapi.EvictionWrite) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	if _, ok := cluster.Annotations[computeconstants.ServerDeletionHintAnnotation]; ok {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being evicted").
			WithErrorDescription("The compute cluster is being evicted.").
			Prefixed()
	}

	// Lookup the servers and ensure they all exist...
	servers, err := region.New(c.region).Servers(ctx, organizationID, cluster)
	if err != nil {
		return err
	}

	servers = slices.DeleteFunc(servers, func(server regionapi.ServerRead) bool {
		return server.Metadata.DeletionTime != nil || !slices.Contains(request.MachineIDs, server.Metadata.Id)
	})

	if len(servers) != len(request.MachineIDs) {
		return errorsv2.NewInvalidRequestError().
			WithSimpleCause("one or more servers could not be found").
			WithErrorDescription("One of the specified machine IDs is invalid or cannot be resolved.").
			Prefixed()
	}

	updated := cluster.DeepCopy()

	for i := range servers {
		server := &servers[i]

		poolName, err := managerutil.GetWorkloadPoolTag(server.Metadata.Tags)
		if err != nil {
			return errorsv2.NewInternalError().WithCause(err).Prefixed()
		}

		pool, ok := updated.GetWorkloadPool(poolName)
		if !ok {
			return errorsv2.NewInternalError().
				WithSimpleCausef("no workload pool %s found in compute cluster", poolName).
				Prefixed()
		}

		pool.Replicas--
	}

	if updated.Annotations == nil {
		updated.Annotations = map[string]string{}
	}

	updated.Annotations[computeconstants.ServerDeletionHintAnnotation] = strings.Join(request.MachineIDs, ",")

	allocations, err := c.generateAllocations(ctx, organizationID, updated)
	if err != nil {
		return err
	}

	if err := identityclient.NewAllocations(c.client, c.identity).Update(ctx, updated, allocations); err != nil {
		return err
	}

	if err := c.client.Patch(ctx, updated, client.MergeFrom(cluster)); err != nil {
		return errorsv2.NewInternalError().
			WithCausef("failed to patch compute cluster: %w", err).
			Prefixed()
	}

	return nil
}

func (c *Client) HardRebootMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	return region.New(c.region).HardRebootServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID)
}

func (c *Client) SoftRebootMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	return region.New(c.region).SoftRebootServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID)
}

func (c *Client) StartMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	return region.New(c.region).StartServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID)
}

func (c *Client) StopMachine(ctx context.Context, organizationID, projectID, clusterID, machineID string) error {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return err
	}

	if cluster.DeletionTimestamp != nil {
		return errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()
	}

	return region.New(c.region).StopServer(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID)
}

func (c *Client) CreateConsoleSession(ctx context.Context, organizationID, projectID, clusterID, machineID string) (*regionapi.ConsoleSessionResponse, error) {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return nil, err
	}

	if cluster.DeletionTimestamp != nil {
		err = errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()

		return nil, err
	}

	return region.New(c.region).CreateConsoleSession(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID)
}

func (c *Client) GetConsoleOutput(ctx context.Context, organizationID, projectID, clusterID, machineID string, params *openapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDClustersClusterIDMachinesMachineIDConsoleoutputParams) (*regionapi.ConsoleOutputResponse, error) {
	cluster, err := c.get(ctx, organizationID, projectID, clusterID)
	if err != nil {
		return nil, err
	}

	if cluster.DeletionTimestamp != nil {
		err = errorsv2.NewConflictError().
			WithSimpleCause("compute cluster is being deleted").
			WithErrorDescription("The compute cluster is being deleted and cannot be modified.").
			Prefixed()

		return nil, err
	}

	getConsoleOutputParams := &regionapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDConsoleoutputParams{
		Length: params.Length,
	}

	return region.New(c.region).GetConsoleOutput(ctx, organizationID, projectID, cluster.Annotations[constants.IdentityAnnotation], machineID, getConsoleOutputParams)
}
