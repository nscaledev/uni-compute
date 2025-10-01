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

// Security groups are optional per workload pool, and there is at most one per pool.
// Security groups identify their owning pool by using resource tags.  This means
// security groups will only ever need to be created or deleted.
// Security group rules are identified by building a unique tuple from all their
// elements (direction, port range and allowed prefixes) and therefore will also
// only ever need to be created or deleted.

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/spjmurray/go-util/pkg/set"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// securityGroupSet contains a set of security groups, indexed by pool name.
type securityGroupSet map[string]*regionapi.SecurityGroupRead

// add adds a security group to the set and raises an error if one already exists.
func (s securityGroupSet) add(poolName string, securityGroup *regionapi.SecurityGroupRead) error {
	if _, ok := s[poolName]; ok {
		return fmt.Errorf("%w: security group for pool %s already exists", ErrConsistency, poolName)
	}

	s[poolName] = securityGroup

	return nil
}

// newSecurityGroupSet returns a set of security groups, indexed by pool name.
func (p *Provisioner) newSecurityGroupSet(ctx context.Context, client regionapi.ClientWithResponsesInterface) (securityGroupSet, error) {
	log := log.FromContext(ctx)

	securityGroups, err := p.listSecurityGroups(ctx, client)
	if err != nil {
		return nil, err
	}

	result := securityGroupSet{}

	for i := range securityGroups {
		securityGroup := &securityGroups[i]

		poolName, err := util.GetWorkloadPoolTag(securityGroup.Metadata.Tags)
		if err != nil {
			return nil, err
		}

		if err := result.add(poolName, securityGroup); err != nil {
			return nil, err
		}
	}

	log.V(1).Info("reading existing security groups for cluster", "securityGroups", result)

	return result, nil
}

// securityGroupName generates a unique security group name from the cluster and pool.
func (p *Provisioner) securityGroupName(pool *unikornv1.ComputeClusterWorkloadPoolSpec) string {
	return fmt.Sprintf("%s-%s", p.cluster.Name, pool.Name)
}

// generateSecurityGroupRule generates a single security group rule.
func generateRequiredSecurityGroupRule(in *unikornv1.FirewallRule, prefix unikornv1core.IPv4Prefix) *regionapi.SecurityGroupRule {
	rule := &regionapi.SecurityGroupRule{
		Direction: regionapi.NetworkDirection(in.Direction),
		Protocol:  regionapi.NetworkProtocol(in.Protocol),
		Cidr:      prefix.String(),
	}

	// TODO: Smell code.  I think the region controller should be responsible
	// for managing CIDR handling.
	if in.PortMax != nil {
		rule.Port.Range = &regionapi.SecurityGroupRulePortRange{
			Start: in.Port,
			End:   *in.PortMax,
		}
	} else {
		rule.Port.Number = &in.Port
	}

	return rule
}

// generateRequiredSecurityGroupRules creates all the security group rules we require based on
// the input specification.  It essentially translates from our simple user facing API to that
// employed by the region controller.
func generateRequiredSecurityGroupRules(pool *unikornv1.ComputeClusterWorkloadPoolSpec) []regionapi.SecurityGroupRule {
	out := make([]regionapi.SecurityGroupRule, 0, len(pool.Firewall))

	for i := range pool.Firewall {
		for _, prefix := range pool.Firewall[i].Prefixes {
			rule := generateRequiredSecurityGroupRule(&pool.Firewall[i], prefix)

			out = append(out, *rule)
		}
	}

	return out
}

// generateSecurityGroup creates a new security group request.
func (p *Provisioner) generateSecurityGroup(pool *unikornv1.ComputeClusterWorkloadPoolSpec) *regionapi.SecurityGroupWrite {
	return &regionapi.SecurityGroupWrite{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        p.securityGroupName(pool),
			Description: ptr.To("Security group for cluster " + p.cluster.Name),
			Tags:        p.tags(pool),
		},
		Spec: regionapi.SecurityGroupSpec{
			Rules: generateRequiredSecurityGroupRules(pool),
		},
	}
}

// securityGroupCreateSet defines all security groups that should exist.
type securityGroupCreateSet map[string]*regionapi.SecurityGroupWrite

// add adds a security group to the set and raises an error if one already exists.
func (s securityGroupCreateSet) add(poolName string, securityGroup *regionapi.SecurityGroupWrite) error {
	if _, ok := s[poolName]; ok {
		return fmt.Errorf("%w: security group for pool %s already", ErrConsistency, poolName)
	}

	s[poolName] = securityGroup

	return nil
}

// generateSecurityGroupCreateSet creates a set of all security groups that need to exist.
func (p *Provisioner) generateSecurityGroupCreateSet() (securityGroupCreateSet, error) {
	out := securityGroupCreateSet{}

	for i := range p.cluster.Spec.WorkloadPools.Pools {
		pool := &p.cluster.Spec.WorkloadPools.Pools[i]

		if !pool.HasFirewallRules() {
			continue
		}

		if err := out.add(pool.Name, p.generateSecurityGroup(pool)); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// scheduleServerGroups determines what needs to be created/updated/deleted.
func scheduleServerGroups(current securityGroupSet, requested securityGroupCreateSet) (set.Set[string], set.Set[string], set.Set[string]) {
	currentNames := set.New[string](slices.Collect(maps.Keys(current))...)
	requestedNames := set.New[string](slices.Collect(maps.Keys(requested))...)

	return requestedNames.Difference(currentNames), currentNames.Intersection(requestedNames), currentNames.Difference(requestedNames)
}

// reconcileSecurityGroups iterates through all pools and ensures any server groups that
// are required exist.
func (p *Provisioner) reconcileSecurityGroups(ctx context.Context, client regionapi.ClientWithResponsesInterface, securityGroups securityGroupSet) error {
	log := log.FromContext(ctx)

	required, err := p.generateSecurityGroupCreateSet()
	if err != nil {
		return err
	}

	create, update, remove := scheduleServerGroups(securityGroups, required)

	for poolName := range create.All() {
		request := required[poolName]

		log.Info("creating security group", "pool", poolName, "name", request.Metadata.Name)

		securityGroup, err := p.createSecurityGroup(ctx, client, request)
		if err != nil {
			return err
		}

		if err := securityGroups.add(poolName, securityGroup); err != nil {
			return err
		}
	}

	for poolName := range update.All() {
		currentSecurityGroup := securityGroups[poolName]
		requiredSecurityGroup := required[poolName]

		// TODO: metadata e.g. tags etc.
		if reflect.DeepEqual(currentSecurityGroup.Spec, requiredSecurityGroup.Spec) {
			continue
		}

		if _, err := p.updateSecurityGroup(ctx, client, currentSecurityGroup.Metadata.Id, requiredSecurityGroup); err != nil {
			return err
		}
	}

	for poolName := range remove.All() {
		securityGroup := securityGroups[poolName]

		log.Info("deleting security group", "pool", poolName, "id", securityGroup.Metadata.Id, "name", securityGroup.Metadata.Name)

		if err := p.deleteSecurityGroup(ctx, client, securityGroup.Metadata.Id); err != nil {
			return err
		}
	}

	return nil
}
