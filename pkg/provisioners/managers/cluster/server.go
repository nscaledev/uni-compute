/*
Copyright 2024 the Unikorn Authors.

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
	"slices"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeprovisioners "github.com/unikorn-cloud/compute/pkg/provisioners"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"
)

func (p *Provisioner) reconcileServers(ctx context.Context, client regionapi.ClientWithResponsesInterface, pool *unikornv1.ComputeClusterWorkloadPoolsPoolSpec, servers computeprovisioners.WorkloadPoolProvisionedServerSet, securitygroups computeprovisioners.WorkloadPoolProvisionedSecurityGroupSet, options *computeprovisioners.ClusterOpenstackOptions) error {
	provisionedServers := servers[pool.Name]

	toDelete, toCreate := p.serverReconciliationList(provisionedServers, pool)

	for _, name := range toDelete {
		if err := p.deleteServer(ctx, client, provisionedServers[name].Metadata.Id); err != nil {
			return err
		}
	}

	for _, name := range toCreate {
		if err := p.createServer(ctx, client, name, *options.ProviderNetwork.NetworkID, pool, securitygroups[pool.Name]); err != nil {
			return err
		}
	}

	return nil
}

func (p *Provisioner) deleteServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, id string) error {
	resp, err := client.DeleteApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersServerIDWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], id)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusAccepted && resp.StatusCode() != http.StatusNotFound {
		return fmt.Errorf("%w: server DELETE expected 202 got %d", coreerrors.ErrAPIStatus, resp.StatusCode())
	}

	return nil
}

func (p *Provisioner) createServer(ctx context.Context, client regionapi.ClientWithResponsesInterface, name, networkID string, pool *unikornv1.ComputeClusterWorkloadPoolsPoolSpec, securitygroup *regionapi.SecurityGroupRead) error {
	publicIPAllocationEnabled := false
	if pool.PublicIPAllocation != nil {
		publicIPAllocationEnabled = pool.PublicIPAllocation.Enabled
	}

	var securitygroups *regionapi.ServerSecurityGroupList
	if securitygroup != nil {
		securitygroups = &regionapi.ServerSecurityGroupList{
			regionapi.ServerSecurityGroup{
				Id: securitygroup.Metadata.Id,
			},
		}
	}

	request := regionapi.ServerWrite{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        name,
			Description: ptr.To("Server for cluster " + p.cluster.Name),
			Tags:        p.tags(pool),
		},
		Spec: regionapi.ServerWriteSpec{
			FlavorId: *pool.FlavorID,
			Image: regionapi.ServerImage{
				Id: pool.ImageID,
			},
			Networks: regionapi.ServerNetworkList{
				regionapi.ServerNetwork{
					Id: networkID,
				},
			},
			PublicIPAllocation: &regionapi.ServerPublicIPAllocation{
				Enabled: publicIPAllocationEnabled,
			},
			SecurityGroups: securitygroups,
		},
	}

	resp, err := client.PostApiV1OrganizationsOrganizationIDProjectsProjectIDIdentitiesIdentityIDServersWithResponse(
		ctx, p.cluster.Labels[coreconstants.OrganizationLabel], p.cluster.Labels[coreconstants.ProjectLabel], p.cluster.Annotations[coreconstants.IdentityAnnotation], request)
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusCreated {
		return fmt.Errorf("%w: server POST expected 201 got %d", coreerrors.ErrAPIStatus, resp.StatusCode())
	}

	return nil
}

func (p *Provisioner) serverName(pool *unikornv1.ComputeClusterWorkloadPoolsPoolSpec, replicaIndex int) string {
	// naive implementation to create a server name based on the pool name and replica index
	return fmt.Sprintf("%s-%d", pool.Name, replicaIndex)
}

// serverReconciliationList compares the provisioned servers with the desired servers and returns the name of the servers to delete and to create.
func (p *Provisioner) serverReconciliationList(provisioned computeprovisioners.ProvisionedServerSet, desired *unikornv1.ComputeClusterWorkloadPoolsPoolSpec) ([]string, []string) {
	toDelete, toCreate := []string{}, []string{}
	desiredSet := make(map[string]struct{})

	// build a set of desired server names based on the replica count
	for i := range *desired.Replicas {
		desiredSet[p.serverName(desired, i)] = struct{}{}
	}

	// find servers to delete
	for name := range provisioned {
		if _, exists := desiredSet[name]; !exists {
			toDelete = append(toDelete, name)
		}
	}

	// find servers to create
	for name := range desiredSet {
		if _, exists := provisioned[name]; !exists {
			toCreate = append(toCreate, name)
		}
	}

	return toDelete, toCreate
}

func (p *Provisioner) getServers(ctx context.Context, client regionapi.ClientWithResponsesInterface) (*regionapi.ServersResponse, error) {
	response, err := client.GetApiV1OrganizationsOrganizationIDServersWithResponse(ctx, p.cluster.Labels[coreconstants.OrganizationLabel])
	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: servers GET expected 200 got %d", coreerrors.ErrAPIStatus, response.StatusCode())
	}

	// Filter out servers that aren't from this cluster.
	result := slices.DeleteFunc(*response.JSON200, func(server regionapi.ServerRead) bool {
		return p.filterComputeCluster(server.Metadata.Tags)
	})

	return &result, nil
}

func (p *Provisioner) filterComputeCluster(tags *coreapi.TagList) bool {
	if tags == nil {
		return true
	}

	index := slices.IndexFunc(*tags, func(tag coreapi.Tag) bool {
		return tag.Name == coreconstants.ComputeClusterLabel && tag.Value == p.cluster.Name
	})

	return index < 0
}

func (p *Provisioner) getProvisionedServerSet(ctx context.Context, client regionapi.ClientWithResponsesInterface) (computeprovisioners.WorkloadPoolProvisionedServerSet, error) {
	servers, err := p.getServers(ctx, client)
	if err != nil {
		return nil, err
	}

	result := make(computeprovisioners.WorkloadPoolProvisionedServerSet)

	for _, server := range *servers {
		// find the workload pool tag
		index := slices.IndexFunc(*server.Metadata.Tags, func(tag coreapi.Tag) bool {
			return tag.Name == WorkloadPoolLabel
		})

		if index < 0 {
			continue
		}

		poolName := (*server.Metadata.Tags)[index].Value
		if _, exists := result[poolName]; !exists {
			result[poolName] = make(computeprovisioners.ProvisionedServerSet)
		}

		result[poolName][server.Metadata.Name] = server
	}

	return result, nil
}
