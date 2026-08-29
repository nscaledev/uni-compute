/*
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
	"context"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
)

func NewProvisionerForTest(instance unikornv1.ComputeInstance) *Provisioner {
	return &Provisioner{
		instance: instance,
	}
}

func (p *Provisioner) GenerateServerCreateRequest() (*regionapi.ServerV2Create, error) {
	return p.generateServerCreateRequest()
}

func (p *Provisioner) GenerateServerUpdateRequest() (*regionapi.ServerV2Update, error) {
	return p.generateServerUpdateRequest()
}

func NeedsRecreate(current *regionapi.ServerV2Read, desired *regionapi.ServerV2Update, desiredSSHCertificateAuthorityID *string) bool {
	return needsRecreate(current, desired, desiredSSHCertificateAuthorityID)
}

func (p *Provisioner) CreateOrUpdateServer(ctx context.Context, region regionapi.ClientWithResponsesInterface, server *regionapi.ServerV2Read) (*regionapi.ServerV2Read, error) {
	return p.createOrUpdateServer(ctx, region, server)
}

func (p *Provisioner) UpdateInstanceStatus(server *regionapi.ServerV2Response) {
	p.updateInstanceStatus(server)
}

func ActiveConditionReason(in *regionapi.InstanceLifecyclePhase) (regionv1.ActiveConditionReason, bool) {
	return activeConditionReason(in)
}
