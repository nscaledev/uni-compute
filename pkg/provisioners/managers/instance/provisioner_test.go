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

package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	instance "github.com/unikorn-cloud/compute/pkg/provisioners/managers/instance"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	idstest "github.com/unikorn-cloud/region/pkg/ids/idstest"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	testNetworkID = "b059b3e6-9ae5-42b7-94b4-f42fb7a6baee"
	testFlavorID  = "c7568e2d-f9ab-453d-9a3a-51375f78426b"
	testImageID   = "a10e30e8-006a-48e6-a3c7-3c9416891f31"
	testFlavorID2 = "d1e2f3a4-b5c6-4d7e-8f90-1a2b3c4d5e6f"
	testImageID2  = "e2f3a4b5-c6d7-4e8f-9012-2b3c4d5e6f70"
)

func newProvisionerForTest(sshCertificateAuthorityID *string) *instance.Provisioner {
	return instance.NewProvisionerForTest(unikornv1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-1",
			Labels: map[string]string{
				coreconstants.NameLabel:      "test-instance",
				regionconstants.NetworkLabel: testNetworkID,
			},
		},
		Spec: unikornv1.ComputeInstanceSpec{
			MachineGeneric: unikornv1core.MachineGeneric{
				FlavorID: testFlavorID,
				ImageID:  testImageID,
			},
			Networking: &unikornv1.ComputeInstanceNetworking{
				PublicIP:         true,
				SecurityGroupIDs: []string{"7f9c3d2a-1b4e-4c6a-8d5f-2e1a3b4c5d6e"},
			},
			SSHCertificateAuthorityID: sshCertificateAuthorityID,
			UserData:                  []byte("#cloud-config\nusers: []\n"),
		},
	})
}

func TestGenerateServerCreateRequestIncludesSSHCertificateAuthority(t *testing.T) {
	t.Parallel()

	sshCertificateAuthorityID := "ssh-ca-1"
	provisioner := newProvisionerForTest(ptr.To(sshCertificateAuthorityID))

	request, err := provisioner.GenerateServerCreateRequest()
	require.NoError(t, err)

	require.NotNil(t, request.Spec.SshCertificateAuthorityId)
	assert.Equal(t, sshCertificateAuthorityID, *request.Spec.SshCertificateAuthorityId)
	assert.Equal(t, testNetworkID, request.Spec.NetworkId.String())
	assert.Equal(t, testFlavorID, request.Spec.FlavorId.String())
	assert.Equal(t, testImageID, request.Spec.ImageId.String())
	require.NotNil(t, request.Spec.Networking)
	require.NotNil(t, request.Metadata.Tags)
	assert.Equal(t, constants.InstanceLabel, (*request.Metadata.Tags)[0].Name)
}

func TestCreateOrUpdateServerIgnoresSSHCertificateAuthorityOnlyChange(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(ptr.To("ssh-ca-1"))

	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Name: request.Metadata.Name,
		},
		Spec: request.Spec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), nil, current)

	require.NoError(t, err)
	assert.Same(t, current, updated)
}

func TestNeedsRebuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		current  *regionapi.ServerV2Read
		desired  *regionapi.ServerV2Update
		expected bool
	}{
		{
			name: "same spec",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			expected: false,
		},
		{
			name: "flavor change",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID2),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			expected: true,
		},
		{
			name: "image change",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID2),
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, instance.NeedsRebuild(test.current, test.desired))
		})
	}
}

func TestUpdateInstanceStatusCopiesMACAddress(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	server := &regionapi.ServerV2Response{
		Status: regionapi.ServerV2Status{
			PrivateIP:  ptr.To("192.168.0.42"),
			PublicIP:   ptr.To("203.0.113.10"),
			MacAddress: ptr.To("fa:16:3e:12:34:56"),
			PowerState: ptr.To(regionapi.InstanceLifecyclePhaseRunning),
		},
	}

	provisioner.UpdateInstanceStatus(server)

	instanceObject, ok := provisioner.Object().(*unikornv1.ComputeInstance)
	require.True(t, ok)
	require.Equal(t, server.Status.PrivateIP, instanceObject.Status.PrivateIP)
	require.Equal(t, server.Status.PublicIP, instanceObject.Status.PublicIP)
	require.Equal(t, server.Status.MacAddress, instanceObject.Status.MACAddress)

	active, err := unikornv1.GetActiveCondition(instanceObject)
	require.NoError(t, err)
	assert.Equal(t, regionv1.ActiveConditionReasonRunning, active.Reason)
}

// TestActiveConditionReason verifies every known backing-server power state maps
// to region's lifecycle reason vocabulary (which the instance's Active condition
// mirrors). A nil or unrecognised phase returns ok=false so the caller leaves the
// condition untouched rather than inventing a state.
func TestActiveConditionReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *regionapi.InstanceLifecyclePhase
		want regionv1.ActiveConditionReason
		ok   bool
	}{
		{name: "nil input", in: nil, want: "", ok: false},
		{name: "pending", in: ptr.To(regionapi.InstanceLifecyclePhasePending), want: regionv1.ActiveConditionReasonPending, ok: true},
		{name: "queued", in: ptr.To(regionapi.InstanceLifecyclePhaseQueued), want: regionv1.ActiveConditionReasonQueued, ok: true},
		{name: "building", in: ptr.To(regionapi.InstanceLifecyclePhaseBuilding), want: regionv1.ActiveConditionReasonBuilding, ok: true},
		{name: "running", in: ptr.To(regionapi.InstanceLifecyclePhaseRunning), want: regionv1.ActiveConditionReasonRunning, ok: true},
		{name: "stopping", in: ptr.To(regionapi.InstanceLifecyclePhaseStopping), want: regionv1.ActiveConditionReasonStopping, ok: true},
		{name: "stopped", in: ptr.To(regionapi.InstanceLifecyclePhaseStopped), want: regionv1.ActiveConditionReasonStopped, ok: true},
		{name: "error", in: ptr.To(regionapi.InstanceLifecyclePhaseError), want: regionv1.ActiveConditionReasonError, ok: true},
		{name: "unknown future phase returns ok=false", in: ptr.To(regionapi.InstanceLifecyclePhase("FutureUnknown")), want: "", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := instance.ActiveConditionReason(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
