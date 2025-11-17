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
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
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

	if server == nil {
		request := &regionapi.ServerV2Create{
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

		temp, err := p.createServer(ctx, region, request)
		if err != nil {
			return err
		}

		server = temp
	}

	p.instance.Status.PrivateIP = server.Status.PrivateIP
	p.instance.Status.PublicIP = server.Status.PublicIP

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

	return nil
}
