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

package instance

import (
	"context"
	"reflect"

	"github.com/spf13/pflag"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	"github.com/unikorn-cloud/compute/pkg/provisioners/managers/cluster/util"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionclient "github.com/unikorn-cloud/region/pkg/client"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"
)

// Options allows access to CLI options in the provisioner.
type Options struct {
	// identityOptions allow the identity host and CA to be set.
	identityOptions *identityclient.Options
	// regionOptions allows the region host and CA to be set.
	regionOptions *regionclient.Options
	// clientOptions give access to client certificate information as
	// we need to talk to identity to get a token, and then to region
	// to ensure cloud identities and networks are provisioned, as well
	// as deptovisioning them.
	clientOptions coreclient.HTTPClientOptions
}

func (o *Options) AddFlags(f *pflag.FlagSet) {
	if o.identityOptions == nil {
		o.identityOptions = identityclient.NewOptions()
	}

	if o.regionOptions == nil {
		o.regionOptions = regionclient.NewOptions()
	}

	o.identityOptions.AddFlags(f)
	o.regionOptions.AddFlags(f)
	o.clientOptions.AddFlags(f)
}

// Provisioner encapsulates control plane provisioning.
type Provisioner struct {
	provisioners.Metadata

	// instance is the compute instance we're provisioning.
	instance unikornv1.ComputeInstance

	// options are documented for the type.
	options *Options
}

// New returns a new initialized provisioner object.
func New(options manager.ControllerOptions) provisioners.ManagerProvisioner {
	o, _ := options.(*Options)

	return &Provisioner{
		options: o,
	}
}

// Ensure the ManagerProvisioner interface is implemented.
var _ provisioners.ManagerProvisioner = &Provisioner{}

func (p *Provisioner) Object() unikornv1core.ManagableResourceInterface {
	return &p.instance
}

func (p *Provisioner) identityClient(ctx context.Context) (identityapi.ClientWithResponsesInterface, error) {
	client, err := coreclient.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	token, err := identityclient.NewTokenIssuer(client, p.options.identityOptions, &p.options.clientOptions, constants.ServiceDescriptor()).Issue(ctx)
	if err != nil {
		return nil, err
	}

	return identityclient.New(client, p.options.identityOptions, &p.options.clientOptions).ControllerClient(ctx, token, &p.instance)
}

func (p *Provisioner) generateServerNetworking() *regionapi.ServerV2Networking {
	in := p.instance.Spec.Networking

	if in == nil {
		return nil
	}

	var out regionapi.ServerV2Networking

	if len(in.SecurityGroupIDs) > 0 {
		out.SecurityGroups = &in.SecurityGroupIDs
	}

	if in.PublicIP {
		out.PublicIP = &in.PublicIP
	}

	if len(in.AllowedSourceAddresses) > 0 {
		temp := make([]string, len(in.AllowedSourceAddresses))

		for i := range in.AllowedSourceAddresses {
			temp[i] = in.AllowedSourceAddresses[i].String()
		}

		out.AllowedSourceAddresses = &temp
	}

	if !reflect.ValueOf(out).IsZero() {
		return &out
	}

	return nil
}

func (p *Provisioner) generateUserData() *[]byte {
	if len(p.instance.Spec.UserData) == 0 {
		return nil
	}

	return &p.instance.Spec.UserData
}

func (p *Provisioner) generateServerCreateRequest() *regionapi.ServerV2Create {
	return &regionapi.ServerV2Create{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        p.instance.Labels[coreconstants.NameLabel],
			Description: ptr.To("Server for instance" + p.instance.Name),
			Tags: &coreapi.TagList{
				{
					Name:  constants.InstanceLabel,
					Value: p.instance.Name,
				},
			},
		},
		Spec: regionapi.ServerV2CreateSpec{
			NetworkId:  p.instance.Labels[regionconstants.NetworkLabel],
			FlavorId:   p.instance.Spec.FlavorID,
			ImageId:    p.instance.Spec.ImageID,
			Networking: p.generateServerNetworking(),
			UserData:   p.generateUserData(),
		},
	}
}

func (p *Provisioner) generateServerUpdateRequest() *regionapi.ServerV2Update {
	return &regionapi.ServerV2Update{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        p.instance.Labels[coreconstants.NameLabel],
			Description: ptr.To("Server for instance" + p.instance.Name),
			Tags: &coreapi.TagList{
				{
					Name:  constants.InstanceLabel,
					Value: p.instance.Name,
				},
			},
		},
		Spec: regionapi.ServerV2Spec{
			FlavorId:   p.instance.Spec.FlavorID,
			ImageId:    p.instance.Spec.ImageID,
			Networking: p.generateServerNetworking(),
			UserData:   p.generateUserData(),
		},
	}
}

func needsRebuild(a, b *regionapi.ServerV2Spec) bool {
	// Problematically, the region controller doesn't have access to the server's
	// flavor (due to a more recent microversion returning metadata, not the ID)
	// so spotting this change is complex and fragile.  Ideally we would also
	// capture when a live migration is possible to preserve server IPs and disks.
	if a.FlavorId != b.FlavorId {
		return true
	}

	if a.ImageId != b.ImageId {
		return true
	}

	return false
}

func (p *Provisioner) createOrUpdateServer(ctx context.Context, region regionapi.ClientWithResponsesInterface, server *regionapi.ServerV2Read) (*regionapi.ServerV2Read, error) {
	if server == nil {
		return p.createServer(ctx, region, p.generateServerCreateRequest())
	}

	request := p.generateServerUpdateRequest()

	if reflect.DeepEqual(server.Spec, request.Spec) {
		return server, nil
	}

	if needsRebuild(&server.Spec, &request.Spec) {
		if err := p.deleteServer(ctx, region, server.Metadata.Id); err != nil {
			return nil, provisioners.ErrYield
		}
	}

	return p.updateServer(ctx, region, server.Metadata.Id, request)
}

func convertPowerState(in *regionapi.InstanceLifecyclePhase) *regionv1.InstanceLifecyclePhase {
	if in == nil {
		return nil
	}

	//nolint:exhaustive
	switch *in {
	case regionapi.Running:
		return ptr.To(regionv1.InstanceLifecyclePhaseRunning)
	case regionapi.Stopping:
		return ptr.To(regionv1.InstanceLifecyclePhaseStopping)
	case regionapi.Stopped:
		return ptr.To(regionv1.InstanceLifecyclePhaseStopped)
	default:
		return ptr.To(regionv1.InstanceLifecyclePhasePending)
	}
}

// Provision implements the Provision interface.
func (p *Provisioner) Provision(ctx context.Context) error {
	region, err := p.getRegionClient(ctx)
	if err != nil {
		return err
	}

	server, err := p.getServer(ctx, region)
	if err != nil {
		return err
	}

	server, err = p.createOrUpdateServer(ctx, region, server)
	if err != nil {
		return err
	}

	healthStatus, healthReason, healthMessage := util.ConvertHealthStatusCondition(server.Metadata.HealthStatus)
	unikornv1core.UpdateCondition(&p.instance.Status.Conditions, unikornv1core.ConditionHealthy, healthStatus, healthReason, healthMessage)

	p.instance.Status.PrivateIP = server.Status.PrivateIP
	p.instance.Status.PublicIP = server.Status.PublicIP
	p.instance.Status.PowerState = convertPowerState(server.Status.PowerState)

	if server.Metadata.ProvisioningStatus != coreapi.ResourceProvisioningStatusProvisioned {
		return provisioners.ErrYield
	}

	return nil
}

// Deprovision implements the Provision interface.
func (p *Provisioner) Deprovision(ctx context.Context) error {
	region, err := p.getRegionClient(ctx)
	if err != nil {
		return err
	}

	server, err := p.getServer(ctx, region)
	if err != nil {
		return err
	}

	if server != nil {
		if err := p.deleteServer(ctx, region, server.Metadata.Id); err != nil {
			return err
		}

		return provisioners.ErrYield
	}

	cli, err := coreclient.FromContext(ctx)
	if err != nil {
		return err
	}

	if err := identityclient.NewAllocations(cli, p.identityClient).Delete(ctx, &p.instance); err != nil {
		return err
	}

	return nil
}
