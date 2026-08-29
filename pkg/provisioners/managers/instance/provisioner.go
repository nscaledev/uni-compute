/*
Copyright 2024-2025 the Unikorn Authors.
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
	"github.com/unikorn-cloud/core/pkg/provisioninglog"
	identityclient "github.com/unikorn-cloud/identity/pkg/client"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionclient "github.com/unikorn-cloud/region/pkg/client"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionids "github.com/unikorn-cloud/region/pkg/ids"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
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

	return identityclient.New(client, p.options.identityOptions, &p.options.clientOptions).ControllerClient(ctx, &p.instance)
}

func (p *Provisioner) generateServerNetworking() (*regionapi.ServerV2Networking, error) {
	in := p.instance.Spec.Networking

	if in == nil {
		//nolint:nilnil
		return nil, nil
	}

	var out regionapi.ServerV2Networking

	if len(in.SecurityGroupIDs) > 0 {
		// region v1.19.0 types the security group ID list; parse the stored string
		// IDs to typed IDs, failing closed on a malformed value (matching the
		// network/flavor/image ID handling).
		securityGroupIDs := make(regionapi.ServerV2SecurityGroupIDList, len(in.SecurityGroupIDs))

		for i, id := range in.SecurityGroupIDs {
			securityGroupID, err := regionids.ParseSecurityGroupID(id)
			if err != nil {
				return nil, err
			}

			securityGroupIDs[i] = securityGroupID
		}

		out.SecurityGroups = &securityGroupIDs
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
		return &out, nil
	}

	//nolint:nilnil
	return nil, nil
}

func (p *Provisioner) generateUserData() *[]byte {
	if len(p.instance.Spec.UserData) == 0 {
		return nil
	}

	return &p.instance.Spec.UserData
}

func (p *Provisioner) generateServerCreateRequest() (*regionapi.ServerV2Create, error) {
	// The network, flavor, image and SSH CA IDs are read from the instance's labels
	// and spec (strings); parse them to the typed IDs the region API expects, failing
	// closed on a malformed value.
	networkID, err := regionids.ParseNetworkID(p.instance.Labels[regionconstants.NetworkLabel])
	if err != nil {
		return nil, err
	}

	flavorID, err := regionids.ParseFlavorID(p.instance.Spec.FlavorID)
	if err != nil {
		return nil, err
	}

	imageID, err := regionids.ParseImageID(p.instance.Spec.ImageID)
	if err != nil {
		return nil, err
	}

	networking, err := p.generateServerNetworking()
	if err != nil {
		return nil, err
	}

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
			NetworkId:  networkID,
			FlavorId:   flavorID,
			ImageId:    imageID,
			Networking: networking,
			// region's server-spec sshCertificateAuthorityId is string-typed, so the
			// stored CRD value (also a string) passes through unparsed.
			SshCertificateAuthorityId: p.instance.Spec.SSHCertificateAuthorityID,
			UserData:                  p.generateUserData(),
		},
	}, nil
}

func (p *Provisioner) generateServerUpdateRequest() (*regionapi.ServerV2Update, error) {
	// The flavor and image IDs are read from the instance's spec (strings); parse them
	// to the typed IDs the region API expects, failing closed on a malformed value.
	flavorID, err := regionids.ParseFlavorID(p.instance.Spec.FlavorID)
	if err != nil {
		return nil, err
	}

	imageID, err := regionids.ParseImageID(p.instance.Spec.ImageID)
	if err != nil {
		return nil, err
	}

	networking, err := p.generateServerNetworking()
	if err != nil {
		return nil, err
	}

	return &regionapi.ServerV2Update{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        p.instance.Labels[coreconstants.NameLabel],
			Description: ptr.To("Server for instance " + p.instance.Name),
			Tags: &coreapi.TagList{
				{
					Name:  constants.InstanceLabel,
					Value: p.instance.Name,
				},
			},
		},
		Spec: regionapi.ServerV2Spec{
			FlavorId:   flavorID,
			ImageId:    imageID,
			Networking: networking,
			UserData:   p.generateUserData(),
		},
	}, nil
}

func needsRecreateSpec(a, b *regionapi.ServerV2Spec) bool {
	// Problematically, the region controller doesn't have access to the server's
	// flavor (due to a more recent microversion returning metadata, not the ID)
	// so spotting this change is complex and fragile.
	return a.FlavorId != b.FlavorId
}

// sshCertificateAuthorityDrift reports whether the backing server's SSH
// certificate authority differs from the instance's desired one. Both a nil
// pointer and an empty string mean "no CA", so they are normalised before
// comparison; this catches set→unset, unset→set and changed alike.
func sshCertificateAuthorityDrift(current, desired *string) bool {
	return ptr.Deref(current, "") != ptr.Deref(desired, "")
}

func needsRecreate(current *regionapi.ServerV2Read, desired *regionapi.ServerV2Update, desiredSSHCertificateAuthorityID *string) bool {
	// The SSH CA is a create-only field on region: ServerV2Spec (the update body)
	// carries no CA, so an in-place PUT can never change it. The only way to push a
	// changed CA to the backing server is to delete and recreate it, mirroring
	// flavor drift. The live CA is read back from region's status, not its spec.
	return needsRecreateSpec(&current.Spec, &desired.Spec) ||
		sshCertificateAuthorityDrift(current.Status.SshCertificateAuthorityId, desiredSSHCertificateAuthorityID)
}

func (p *Provisioner) createOrUpdateServer(ctx context.Context, region regionapi.ClientWithResponsesInterface, server *regionapi.ServerV2Read) (*regionapi.ServerV2Read, error) {
	if server == nil {
		request, err := p.generateServerCreateRequest()
		if err != nil {
			return nil, err
		}

		return p.createServer(ctx, region, request)
	}

	request, err := p.generateServerUpdateRequest()
	if err != nil {
		return nil, err
	}

	// The server ID comes from the region read model (a string); parse it to the
	// typed ID for the region API calls, failing closed on a malformed value. It is
	// only needed on the recreate/update paths, not the no-op (specs equal) path.
	if needsRecreate(server, request, p.instance.Spec.SSHCertificateAuthorityID) {
		serverID, err := regionids.ParseServerID(server.Metadata.Id)
		if err != nil {
			return nil, err
		}

		if err := p.deleteServer(ctx, region, serverID); err != nil {
			return nil, provisioners.ErrYield
		}

		return nil, provisioners.ErrYield
	}

	if reflect.DeepEqual(server.Spec, request.Spec) {
		return server, nil
	}

	// Remaining drift is non-recreate (image, userData, networking): it flows through
	// region's in-place update PUT rather than a delete/recreate. For an image change
	// region translates that PUT into an in-place Nova rebuild of the backing server.
	serverID, err := regionids.ParseServerID(server.Metadata.Id)
	if err != nil {
		return nil, err
	}

	return p.updateServer(ctx, region, serverID, request)
}

// activeConditionReason maps the backing server's API power state onto region's
// lifecycle reason vocabulary, which the instance's Active condition mirrors.
//
// A nil or unrecognised phase returns ok=false: the server has not reported a
// lifecycle state yet (or reports a future one this build does not know), so the
// caller leaves the Active condition untouched rather than inventing a state -
// mirroring the region server, whose Active condition is simply absent until
// first observed. The handler-side projection in
// pkg/server/handler/instance/client.go uses the same fall-through convention;
// keep them in lockstep.
func activeConditionReason(in *regionapi.InstanceLifecyclePhase) (regionv1.ActiveConditionReason, bool) {
	if in == nil {
		return "", false
	}

	switch *in {
	case regionapi.InstanceLifecyclePhasePending:
		return regionv1.ActiveConditionReasonPending, true
	case regionapi.InstanceLifecyclePhaseQueued:
		return regionv1.ActiveConditionReasonQueued, true
	case regionapi.InstanceLifecyclePhaseBuilding:
		return regionv1.ActiveConditionReasonBuilding, true
	case regionapi.InstanceLifecyclePhaseRebuilding:
		return regionv1.ActiveConditionReasonRebuilding, true
	case regionapi.InstanceLifecyclePhaseRunning:
		return regionv1.ActiveConditionReasonRunning, true
	case regionapi.InstanceLifecyclePhaseStopping:
		return regionv1.ActiveConditionReasonStopping, true
	case regionapi.InstanceLifecyclePhaseStopped:
		return regionv1.ActiveConditionReasonStopped, true
	case regionapi.InstanceLifecyclePhaseError:
		return regionv1.ActiveConditionReasonError, true
	}

	return "", false
}

func convertHealthStatusCondition(in coreapi.ResourceHealthStatus) (corev1.ConditionStatus, unikornv1core.HealthConditionReason, string) {
	switch in {
	case coreapi.ResourceHealthStatusUnknown:
		return corev1.ConditionFalse, unikornv1core.ConditionReasonUnknown, "health unknown"
	case coreapi.ResourceHealthStatusHealthy:
		return corev1.ConditionTrue, unikornv1core.ConditionReasonHealthy, "healthy"
	case coreapi.ResourceHealthStatusDegraded:
		return corev1.ConditionFalse, unikornv1core.ConditionReasonDegraded, "degraded"
	case coreapi.ResourceHealthStatusError:
		// The backing server reports an error health status; the instance's health
		// axis is a Healthy/Degraded/Unknown verdict, so an errored server is
		// degraded here. (A terminal failure, once propagated on the provisioning
		// axis, is a separate concern from this health verdict.)
		return corev1.ConditionFalse, unikornv1core.ConditionReasonDegraded, "error"
	}

	return corev1.ConditionFalse, unikornv1core.ConditionReasonUnknown, "health unknown"
}

func (p *Provisioner) updateInstanceStatus(server *regionapi.ServerV2Response) {
	p.instance.Status.PrivateIP = server.Status.PrivateIP
	p.instance.Status.PublicIP = server.Status.PublicIP
	p.instance.Status.MACAddress = server.Status.MacAddress

	// Mirror the backing server's lifecycle/power state onto the Active condition.
	if reason, ok := activeConditionReason(server.Status.PowerState); ok {
		p.instance.SetActiveCondition(reason)
	}
}

// instanceActiveReason returns the instance's current Active-condition reason, or
// empty if the condition is absent (not yet observed).
func instanceActiveReason(r unikornv1core.StatusConditionReader) regionv1.ActiveConditionReason {
	active, err := unikornv1.GetActiveCondition(r)
	if err != nil {
		return ""
	}

	return active.Reason
}

// logLifecycleTransition emits the instance's Active-condition transition to the
// structured lifecycle stream (msg == "lifecycle"), at parity with the
// provisioning stream. It is edge-triggered: it emits only when the mirrored
// lifecycle reason actually changed from previousReason, so a poll that observes
// no change stays silent.
func (p *Provisioner) logLifecycleTransition(ctx context.Context, previousReason regionv1.ActiveConditionReason) {
	active, err := unikornv1.GetActiveCondition(&p.instance)
	if err != nil || active.Reason == previousReason {
		return
	}

	cli, err := coreclient.FromContext(ctx)
	if err != nil {
		return
	}

	provisioninglog.Emit(ctx, cli.Scheme(), &p.instance, provisioninglog.StreamLifecycle,
		string(active.Status), string(active.Reason), active.Message)
}

func shouldLogUnhealthyServerTransition(serverHealth coreapi.ResourceHealthStatus, previousHealthReason unikornv1core.HealthConditionReason) bool {
	var reason unikornv1core.HealthConditionReason

	switch serverHealth {
	// Degraded and Error both project to the Degraded health reason (the instance
	// health axis has no Errored), so a Degraded<->Error change is not a distinct
	// transition here; the first move into unhealthy still logs.
	case coreapi.ResourceHealthStatusDegraded, coreapi.ResourceHealthStatusError:
		reason = unikornv1core.ConditionReasonDegraded
	case coreapi.ResourceHealthStatusHealthy, coreapi.ResourceHealthStatusUnknown:
		return false
	}

	return previousHealthReason != reason
}

func healthConditionReason(r unikornv1core.StatusConditionReader) unikornv1core.HealthConditionReason {
	condition, err := unikornv1core.GetHealthyCondition(r)
	if err != nil {
		return ""
	}

	return condition.Reason
}

func (p *Provisioner) logUnhealthyServer(ctx context.Context, server *regionapi.ServerV2Response, previousHealthReason unikornv1core.HealthConditionReason) {
	if !shouldLogUnhealthyServerTransition(server.Metadata.HealthStatus, previousHealthReason) {
		return
	}

	powerState := "<nil>"
	if server.Status.PowerState != nil {
		powerState = string(*server.Status.PowerState)
	}

	log.FromContext(ctx).Info("backing server reported unhealthy status",
		"name", p.instance.Name,
		"namespace", p.instance.Namespace,
		"serverID", server.Metadata.Id,
		"serverName", server.Metadata.Name,
		"serverHealthStatus", server.Metadata.HealthStatus,
		"serverProvisioningStatus", server.Metadata.ProvisioningStatus,
		"serverPowerState", powerState,
		"regionID", server.Status.RegionId,
		"networkID", server.Status.NetworkId,
		"imageID", server.Spec.ImageId,
		"flavorID", server.Spec.FlavorId,
	)
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

	previousHealthReason := healthConditionReason(&p.instance)
	healthStatus, healthReason, healthMessage := convertHealthStatusCondition(server.Metadata.HealthStatus)
	p.instance.SetHealthCondition(healthStatus, healthReason, healthMessage)

	p.logUnhealthyServer(ctx, server, previousHealthReason)

	previousActiveReason := instanceActiveReason(&p.instance)
	p.updateInstanceStatus(server)
	p.logLifecycleTransition(ctx, previousActiveReason)

	if server.Metadata.ProvisioningStatus != coreapi.ResourceProvisioningStatusProvisioned {
		return serverProvisioningError(server)
	}

	return nil
}

// serverProvisioningError reconstructs a typed provisioning error from the backing
// region server's read metadata, so the instance's own Available condition — and
// thus its API provisioningStatusDetail — carries the server's reason and user-safe
// message through to the end user (e.g. "waiting on network", "provider create
// failed"). The disposition is taken from the coarse provisioningStatus (error is
// terminal, anything else still in flight); the reason and message come from the
// provisioningStatusDetail. With no detail we fall back to a bare yield.
func serverProvisioningError(server *regionapi.ServerV2Response) error {
	detail := server.Metadata.ProvisioningStatusDetail
	if detail == nil {
		// The backing server exists (we just created it) but has not reported its
		// provisioning state yet — an instance is a thin wrapper over its server, so
		// that is a dependency wait. Use the generic core reason (compute does not
		// mint provisioning reasons) with a compute-authored message; the server is
		// private to the user, so name the concept, not its internal ID.
		return provisioners.Yield(unikornv1core.ConditionReasonDependencyNotReady, "waiting for the backing server")
	}

	reason := unikornv1core.ProvisioningConditionReason(detail.Reason)

	if server.Metadata.ProvisioningStatus == coreapi.ResourceProvisioningStatusError {
		return provisioners.Terminal(reason, detail.Message)
	}

	return provisioners.Yield(reason, detail.Message)
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
		serverID, err := regionids.ParseServerID(server.Metadata.Id)
		if err != nil {
			return err
		}

		if err := p.deleteServer(ctx, region, serverID); err != nil {
			return err
		}

		return provisioners.ErrYield
	}

	cli, err := coreclient.FromContext(ctx)
	if err != nil {
		return err
	}

	api, err := p.identityClient(ctx)
	if err != nil {
		return err
	}

	if err := identityclient.NewAllocations(cli, api).Delete(ctx, &p.instance); err != nil {
		return err
	}

	return nil
}
