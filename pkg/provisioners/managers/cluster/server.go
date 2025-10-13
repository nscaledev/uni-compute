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
	"maps"
	"reflect"
	"slices"
	"strings"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/errors"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// serverSet maps the server name to its API resource.
type serverSet map[string]*regionapi.ServerRead

// add adds a server to the set and raises an error if one already exists.
func (s serverSet) add(serverName string, server *regionapi.ServerRead) error {
	if _, ok := s[serverName]; ok {
		return fmt.Errorf("%w: server %s already exists in set", errors.ErrConsistency, serverName)
	}

	s[serverName] = server

	return nil
}

// selectDeletionCandidate picks an arbitrary server to delete after first
// searching for preferred options.
func (s serverSet) selectDeletionCandidate(preferredIDs []string) *regionapi.ServerRead {
	servers := slices.Collect(maps.Values(s))

	for _, id := range preferredIDs {
		matchesID := func(server *regionapi.ServerRead) bool {
			return server.Metadata.Id == id
		}

		if index := slices.IndexFunc(servers, matchesID); index >= 0 {
			return servers[index]
		}
	}

	return servers[0]
}

// newServerSet returns a new set of servers indexed by pool and by name.
func newServerSet(ctx context.Context, servers regionapi.ServersRead) (serverSet, error) {
	log := log.FromContext(ctx)

	result := serverSet{}

	for i := range servers {
		server := &servers[i]

		if err := result.add(server.Metadata.Name, server); err != nil {
			return nil, err
		}
	}

	log.V(1).Info("reading existing servers for cluster", "servers", result)

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
		return nil, fmt.Errorf("%w: security group for server pool %s not found", errors.ErrConsistency, pool.Name)
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
func needsRebuild(ctx context.Context, current *regionapi.ServerRead, requested *regionapi.ServerWrite) bool {
	log := log.FromContext(ctx)

	// TODO: flavors can usually be scaled up without losing data but this requires
	// a shutdown, resize, possible confirmation due to a cold migration, and then
	// a restart.
	if current.Spec.FlavorId != requested.Spec.FlavorId {
		log.Info("server rebuild required due to flavor change", "id", current.Metadata.Id, "desiredState", requested.Spec.FlavorId, "currentState", current.Spec.FlavorId)
		return true
	}

	if current.Spec.ImageId != requested.Spec.ImageId {
		log.Info("server rebuild required due to image change", "id", current.Metadata.Id, "desiredState", requested.Spec.ImageId, "currentState", current.Spec.ImageId)
		return true
	}

	return false
}

// deleteServerWrapper wraps up common server deletion handling as it's called from
// multiple different places.
func (p *Provisioner) deleteServerWrapper(ctx context.Context, client regionapi.ClientWithResponsesInterface, server *regionapi.ServerRead) error {
	log := log.FromContext(ctx)

	log.Info("deleting server", "id", server.Metadata.Id, "name", server.Metadata.Name)

	if err := p.deleteServer(ctx, client, server.Metadata.Id); err != nil {
		return err
	}

	server.Metadata.ProvisioningStatus = coreapi.ResourceProvisioningStatusDeprovisioning

	return nil
}

// serverPoolSet organizes servers by pool so we can better reason about
// the number of replicas in that pool.
type serverPoolSet map[string]serverSet

func newServerPoolSet(servers serverSet) (serverPoolSet, error) {
	s := serverPoolSet{}

	for name, server := range servers {
		if server.Metadata.DeletionTime != nil {
			continue
		}

		pool, err := util.GetWorkloadPoolTag(server.Metadata.Tags)
		if err != nil {
			return nil, err
		}

		if _, ok := s[pool]; !ok {
			s[pool] = serverSet{}
		}

		s[pool][name] = server
	}

	return s, nil
}

// getPreferredDeletionIDs gets a set of servers that we should delete as a priority.
// This is set by the API during eviction while scaling down the pools in a single
// atomic operation.
func (p *Provisioner) getPreferredDeletionIDs() []string {
	var out []string

	if t, ok := p.cluster.Annotations[constants.ServerDeletionHintAnnotation]; ok {
		out = strings.Split(t, ",")
	}

	return out
}

// reconcileServers creates/updates/deletes all servers for the cluster.
//
//nolint:cyclop,gocognit
func (p *Provisioner) reconcileServers(ctx context.Context, client regionapi.ClientWithResponsesInterface, servers serverSet, securityGroups securityGroupSet, openstackIdentityStatus *openstackIdentityStatus) error {
	log := log.FromContext(ctx)

	serverPoolSet, err := newServerPoolSet(servers)
	if err != nil {
		return err
	}

	preferredDeletionIDs := p.getPreferredDeletionIDs()

	// Handle deletions and updates.
	for poolName, serverSet := range serverPoolSet {
		// Pool doesn't exist, delete all.
		pool, ok := p.cluster.GetWorkloadPool(poolName)
		if !ok {
			for _, server := range serverSet {
				log.Info("deleting server with an unknown pool", "id", server.Metadata.Id, "pool", poolName)

				if err := p.deleteServerWrapper(ctx, client, server); err != nil {
					return err
				}
			}

			delete(serverPoolSet, poolName)

			continue
		}

		// Scale down.
		for len(serverSet) > pool.Replicas {
			server := serverSet.selectDeletionCandidate(p.getPreferredDeletionIDs())

			log.Info("deleting server due to scale down", "id", server.Metadata.Id, "pool", poolName)

			if err := p.deleteServerWrapper(ctx, client, server); err != nil {
				return err
			}

			delete(serverSet, server.Metadata.Name)
		}

		// Rebuilds and updates.
		for serverName, server := range serverSet {
			required, err := p.generateServer(openstackIdentityStatus, pool, securityGroups)
			if err != nil {
				return err
			}

			if !needsUpdate(server, required) {
				continue
			}

			if needsRebuild(ctx, server, required) {
				log.Info("deleting server due to rebuild", "id", server.Metadata.Id, "pool", poolName)

				if err := p.deleteServerWrapper(ctx, client, server); err != nil {
					return err
				}

				delete(serverSet, server.Metadata.Name)

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

			serverSet[serverName] = updated
		}
	}

	if len(preferredDeletionIDs) > 0 {
		delete(p.cluster.Annotations, constants.ServerDeletionHintAnnotation)

		cli, err := coreclient.FromContext(ctx)
		if err != nil {
			return err
		}

		if err := cli.Update(ctx, &p.cluster); err != nil {
			return err
		}
	}

	// Finally for each pool, scale up any instances that are missing.
	for i := range p.cluster.Spec.WorkloadPools.Pools {
		pool := &p.cluster.Spec.WorkloadPools.Pools[i]

		creations := pool.Replicas

		if serverPool, ok := serverPoolSet[pool.Name]; ok {
			creations = pool.Replicas - len(serverPool)
		}

		if creations < 0 {
			return fmt.Errorf("%w: observed pool size larger than required", errors.ErrConsistency)
		}

		for range creations {
			required, err := p.generateServer(openstackIdentityStatus, pool, securityGroups)
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
