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
	"fmt"
	"reflect"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	"github.com/unikorn-cloud/core/pkg/errors"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// serverPoolSet maps the server name to its API resource.
type serverPoolSet map[string]*regionapi.ServerRead

// add adds a server to the set and raises an error if one already exists.
func (s serverPoolSet) add(serverName string, server *regionapi.ServerRead) error {
	if _, ok := s[serverName]; ok {
		return fmt.Errorf("%w: server %s already exists in set", ErrConsistency, serverName)
	}

	s[serverName] = server

	return nil
}

// newServerSet returns a new set of servers indexed by pool and by name.
func (p *Provisioner) newServerSet(ctx context.Context, servers regionapi.ServersRead) (serverPoolSet, error) {
	log := log.FromContext(ctx)

	result := serverPoolSet{}

	for i := range servers {
		server := &servers[i]

		if err := result.add(server.Metadata.Name, server); err != nil {
			return nil, err
		}
	}

	log.Info("reading existing servers for cluster", "servers", result)

	return result, nil
}

// getSecurityGroupForPool returns the security group for a pool.  It assumes the main provisioner
// has waited until all security groups are ready before proceeding.
func generateSecurityGroup(pool *unikornv1.ComputeClusterWorkloadPoolSpec, securityGroups securityGroupSet) (*regionapi.ServerSecurityGroupList, error) {
	if !pool.HasFirewallRules() {
		//nolint:nilnil
		return nil, nil
	}

	securityGroup, ok := securityGroups[pool.Name]
	if !ok {
		return nil, fmt.Errorf("%w: security group for server pool %s not found", ErrConsistency, pool.Name)
	}

	result := &regionapi.ServerSecurityGroupList{
		regionapi.ServerSecurityGroup{
			Id: securityGroup.Metadata.Id,
		},
	}

	return result, nil
}

// generateUserData generates user data for a server request.
func generateUserData(pool *unikornv1.ComputeClusterWorkloadPoolSpec) *[]byte {
	if pool.UserData == nil {
		return nil
	}

	return &pool.UserData
}

// generateServer generates a server request for creation and updates.
func (p *Provisioner) generateServer(openstackIdentityStatus *openstackIdentityStatus, pool *unikornv1.ComputeClusterWorkloadPoolSpec, securityGroups securityGroupSet) (*regionapi.ServerWrite, error) {
	securityGroup, err := generateSecurityGroup(pool, securityGroups)
	if err != nil {
		return nil, err
	}

	request := &regionapi.ServerWrite{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        pool.Name + "-" + rand.String(6),
			Description: ptr.To("Server for cluster " + p.cluster.Name),
			Tags:        p.tags(pool),
		},
		Spec: regionapi.ServerSpec{
			FlavorId: pool.FlavorID,
			ImageId:  pool.ImageID,
			Networks: regionapi.ServerNetworkList{
				regionapi.ServerNetwork{
					Id: openstackIdentityStatus.NetworkID,
				},
			},
			PublicIPAllocation: &regionapi.ServerPublicIPAllocation{
				Enabled: pool.PublicIPAllocation != nil && pool.PublicIPAllocation.Enabled,
			},
			SecurityGroups: securityGroup,
			UserData:       generateUserData(pool),
		},
	}

	return request, nil
}

// needsUpdate compares both specifications and determines whether we need a resource update.
func needsUpdate(current *regionapi.ServerRead, requested *regionapi.ServerWrite) bool {
	return !reflect.DeepEqual(current.Spec, requested.Spec)
}

// needsRebuild compares the current and requested specifications to determine whether
// we should do an inplace update of the resource (where supported) or rebuild it from
// scratch.
func needsRebuild(current *regionapi.ServerRead, requested *regionapi.ServerWrite) bool {
	// TODO: flavors can usually be scaled up without losing data but this requires
	// a shutdown, resize, possible confirmation due to a cold migration, and then
	// a restart.
	if current.Spec.FlavorId != requested.Spec.FlavorId {
		return true
	}

	if current.Spec.ImageId != requested.Spec.ImageId {
		return true
	}

	// TODO: how to handle user data is as yet unknown.  Theoretically we can just
	// update it and it'll take effect on a reboot without having to lose data,
	// which is probably preferable.  Who is in charge of the reboot?  Or the user
	// may want to blow the machine away and reprovision from scratch.  This probably
	// needs user interaction eventually.
	if current.Spec.UserData != requested.Spec.UserData {
		return true
	}

	return false
}

// deleteServerWrapper wraps up common server deletion handling as it's called from
// multiple different places.
func (p *Provisioner) deleteServerWrapper(ctx context.Context, client regionapi.ClientWithResponsesInterface, servers serverPoolSet, name string) error {
	log := log.FromContext(ctx)

	server := servers[name]

	log.Info("deleting server", "id", server.Metadata.Id, "name", name)

	if err := p.deleteServer(ctx, client, server.Metadata.Id); err != nil {
		return err
	}

	server.Metadata.ProvisioningStatus = coreapi.ResourceProvisioningStatusDeprovisioning

	return nil
}

// reconcileServers creates/updates/deletes all servers for the cluster.
//
//nolint:cyclop,gocognit
func (p *Provisioner) reconcileServers(ctx context.Context, client regionapi.ClientWithResponsesInterface, servers serverPoolSet, securitygroups securityGroupSet, openstackIdentityStatus *openstackIdentityStatus) error {
	log := log.FromContext(ctx)

	// Algorithm:
	// * Names are generated, and thus unpredictable, so we cannot rely on this to
	//   map current servers to required ones.
	// * Instead we go through our existing servers and:
	//   * Ignore any marked as being deleted
	//   * Delete any that don't have a pool.
	//   * Delete any that exceed the number seen for a particular pool
	//   * Delete any that don't match the specification and cannot be updated
	//   * Update those that can be updated online
	//   * Of those that weren't ignored or deleted, we tally them up based on pool.
	// * If any pools don't contain the number of servers that are requested:
	//   * Create new servers to fill the gaps
	poolCounts := map[string]int{}

	// Handle deletes and updates...
	for serverName, server := range servers {
		// Ignore deleting instances.
		if server.Metadata.DeletionTime != nil {
			continue
		}

		poolName, err := util.GetWorkloadPoolTag(server.Metadata.Tags)
		if err != nil {
			return err
		}

		pool, ok := p.cluster.GetWorkloadPool(poolName)
		if !ok {
			if err := p.deleteServerWrapper(ctx, client, servers, serverName); err != nil {
				return err
			}

			continue
		}

		// Delete any servers surplus to requirements.
		if poolCounts[poolName] >= pool.Replicas {
			if err := p.deleteServerWrapper(ctx, client, servers, serverName); err != nil {
				return err
			}

			continue
		}

		// Generate the required specification.
		required, err := p.generateServer(openstackIdentityStatus, pool, securitygroups)
		if err != nil {
			return err
		}

		// If something has changed, we need to do something.
		if needsUpdate(server, required) {
			// Delete machines whose image/flavor/etc. have altered and
			// require rebuilding.
			if needsRebuild(server, required) {
				if err := p.deleteServerWrapper(ctx, client, servers, serverName); err != nil {
					return err
				}

				continue
			}

			// Otherwise update the existing servers networking/etc. that can
			// be modified at runtime.
			log.Info("updating server", "name", serverName)

			// Preserve the existing name, this translates to a host name
			// and should not change.
			required.Metadata.Name = serverName

			updated, err := p.updateServer(ctx, client, server.Metadata.Id, required)
			if err != nil {
				return err
			}

			// Important we fall through after this to do the accounting.
			servers[serverName] = updated
		}

		poolCounts[poolName]++
	}

	// Finally for each pool, scale up any instances that are missing.
	for i := range p.cluster.Spec.WorkloadPools.Pools {
		pool := &p.cluster.Spec.WorkloadPools.Pools[i]

		if poolCounts[pool.Name] > pool.Replicas {
			return fmt.Errorf("%w: observed pool size larger than required", errors.ErrConsistency)
		}

		for i := poolCounts[pool.Name]; i < pool.Replicas; i++ {
			required, err := p.generateServer(openstackIdentityStatus, pool, securitygroups)
			if err != nil {
				return err
			}

			log.Info("creating server", "name", required.Metadata.Name)

			server, err := p.createServer(ctx, client, required)
			if err != nil {
				return err
			}

			if err := servers.add(required.Metadata.Name, server); err != nil {
				return err
			}
		}
	}

	return nil
}
