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
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	unikornv1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/compute/pkg/constants"
	instance "github.com/unikorn-cloud/compute/pkg/provisioners/managers/instance"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
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
	testVolumeID  = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
	testVolumeID2 = "3a21348e-d20a-459c-a57f-d1b24c94ba7f"
	testVolumeID3 = "880360b0-d975-439c-a4d1-4ea13946cb03"
	testVolumeID4 = "ea142ff6-d263-4971-9a56-cfd5c4b04840"
	testVolumeID5 = "dba84415-0fa2-46d2-8075-8199e2775d19"
	testServerID  = "10f9d1cf-bb76-4644-8b48-890ab083182d"
)

type regionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f regionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newRegionClient(t *testing.T, transport regionRoundTripFunc) regionapi.ClientWithResponsesInterface {
	t.Helper()

	client, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)

	return client
}

func volumeList(ids ...string) *regionapi.ServerV2VolumeList {
	result := make(regionapi.ServerV2VolumeList, len(ids))
	for i := range ids {
		result[i] = idstest.MustParseVolumeID(ids[i])
	}

	return &result
}

func instanceVolumeSpecs(ids ...string) []unikornv1.ComputeInstanceVolumeSpec {
	result := make([]unikornv1.ComputeInstanceVolumeSpec, len(ids))
	for i := range ids {
		result[i].ID = ids[i]
	}

	return result
}

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

func TestGenerateServerRequestsIncludeVolumes(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	instanceObject, ok := provisioner.Object().(*unikornv1.ComputeInstance)
	require.True(t, ok)

	instanceObject.Spec.Volumes = instanceVolumeSpecs(testVolumeID)

	create, err := provisioner.GenerateServerCreateRequest()
	require.NoError(t, err)
	require.NotNil(t, create.Spec.Volumes)
	require.Len(t, *create.Spec.Volumes, 1)
	assert.Equal(t, testVolumeID, (*create.Spec.Volumes)[0].String())

	update, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)
	require.NotNil(t, update.Spec.Volumes)
	require.Len(t, *update.Spec.Volumes, 1)
	assert.Equal(t, testVolumeID, (*update.Spec.Volumes)[0].String())
}

func TestGenerateServerUpdateRequestClearsVolumes(t *testing.T) {
	t.Parallel()

	request, err := newProvisionerForTest(nil).GenerateServerUpdateRequest()
	require.NoError(t, err)
	require.NotNil(t, request.Spec.Volumes)
	assert.Empty(t, *request.Spec.Volumes)
}

func TestCreateOrUpdateServerNoopWithNoVolumes(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Name: request.Metadata.Name,
		},
		Spec: request.Spec,
	}
	current.Spec.Volumes = nil

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), nil, current)
	require.NoError(t, err)
	assert.Same(t, current, updated)
}

func TestCreateOrUpdateServerPropagatesVolumeChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		currentVolumes *regionapi.ServerV2VolumeList
		desiredVolumes []string
		create         bool
		attempts       int
		expectedCalls  int
	}{
		{name: "create multiple", desiredVolumes: []string{testVolumeID, testVolumeID2}, create: true, attempts: 1, expectedCalls: 1},
		{name: "add", desiredVolumes: []string{testVolumeID}, attempts: 1, expectedCalls: 1},
		{name: "remove", currentVolumes: volumeList(testVolumeID), desiredVolumes: []string{}, attempts: 1, expectedCalls: 1},
		{name: "replace and retry", currentVolumes: volumeList(testVolumeID), desiredVolumes: []string{testVolumeID2}, attempts: 2, expectedCalls: 2},
		{name: "no-op", currentVolumes: volumeList(testVolumeID), desiredVolumes: []string{testVolumeID}, attempts: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var received [][]string

			regionClient := newRegionClient(t, func(r *http.Request) (*http.Response, error) {
				if test.create {
					assert.Equal(t, http.MethodPost, r.Method)
					assert.Equal(t, "/api/v2/servers", r.URL.Path)
				} else {
					assert.Equal(t, http.MethodPut, r.Method)
					assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)
				}

				var request struct {
					Spec struct {
						Volumes *[]string `json:"volumes"`
					} `json:"spec"`
				}

				require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				require.NotNil(t, request.Spec.Volumes)
				received = append(received, *request.Spec.Volumes)

				status := http.StatusAccepted
				if test.create {
					status = http.StatusCreated
				}

				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(bytes.NewBufferString("{}")),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})

			provisioner := newProvisionerForTest(nil)
			instanceObject, ok := provisioner.Object().(*unikornv1.ComputeInstance)
			require.True(t, ok)

			instanceObject.Spec.Volumes = instanceVolumeSpecs(test.desiredVolumes...)

			var current *regionapi.ServerV2Read

			if !test.create {
				request, err := provisioner.GenerateServerUpdateRequest()
				require.NoError(t, err)

				current = &regionapi.ServerV2Read{
					Metadata: coreapi.ProjectScopedResourceReadMetadata{Id: testServerID, Name: request.Metadata.Name},
					Spec:     request.Spec,
				}
				current.Spec.Volumes = test.currentVolumes
			}

			for range test.attempts {
				result, err := provisioner.CreateOrUpdateServer(t.Context(), regionClient, current)
				require.NoError(t, err)

				if test.expectedCalls == 0 {
					assert.Same(t, current, result)
				}
			}

			require.Len(t, received, test.expectedCalls)

			for _, volumes := range received {
				assert.Equal(t, test.desiredVolumes, volumes)
			}
		})
	}
}

func TestCreateOrUpdateServerPropagatesRegionValidationError(t *testing.T) {
	t.Parallel()

	body, err := json.Marshal(coreapi.Error{
		Error:            coreapi.Conflict,
		ErrorDescription: "volume is already claimed",
	})
	require.NoError(t, err)

	regionClient := newRegionClient(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})

	provisioner := newProvisionerForTest(nil)
	instanceObject, ok := provisioner.Object().(*unikornv1.ComputeInstance)
	require.True(t, ok)

	instanceObject.Spec.Volumes = instanceVolumeSpecs(testVolumeID2)

	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{Id: testServerID, Name: request.Metadata.Name},
		Spec:     request.Spec,
	}
	current.Spec.Volumes = volumeList(testVolumeID)

	_, err = provisioner.CreateOrUpdateServer(t.Context(), regionClient, current)
	require.Error(t, err)
	require.True(t, coreerrors.IsConflict(err), "expected conflict, got: %v", err)
}

func TestDeleteServerDoesNotManageVolumes(t *testing.T) {
	t.Parallel()

	var requests int

	regionClient := newRegionClient(t, func(r *http.Request) (*http.Response, error) {
		requests++

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Request:    r,
		}, nil
	})

	err := newProvisionerForTest(nil).DeleteServer(t.Context(), regionClient, idstest.MustParseServerID(testServerID))
	require.NoError(t, err)
	assert.Equal(t, 1, requests)
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

func TestUpdateInstanceStatusProjectsVolumes(t *testing.T) {
	t.Parallel()

	device := "/dev/vdb"
	message := "attachment failed"
	provisioner := newProvisionerForTest(nil)
	instanceObject, ok := provisioner.Object().(*unikornv1.ComputeInstance)
	require.True(t, ok)

	instanceObject.Spec.Volumes = instanceVolumeSpecs(testVolumeID, testVolumeID2, testVolumeID5)
	server := &regionapi.ServerV2Response{
		Status: regionapi.ServerV2Status{
			Volumes: &regionapi.ServerV2VolumeStatusList{
				{Id: idstest.MustParseVolumeID(testVolumeID), ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioning},
				{Id: idstest.MustParseVolumeID(testVolumeID2), ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioned, Device: &device},
				{Id: idstest.MustParseVolumeID(testVolumeID3), ProvisioningStatus: coreapi.ResourceProvisioningStatusDeprovisioning},
				{Id: idstest.MustParseVolumeID(testVolumeID4), ProvisioningStatus: coreapi.ResourceProvisioningStatusError, Message: &message},
			},
		},
	}

	provisioner.UpdateInstanceStatus(server)

	assert.Equal(t, []unikornv1.ComputeInstanceVolumeStatus{
		{ID: testVolumeID, ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioning},
		{ID: testVolumeID2, ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioned, Device: &device},
		{ID: testVolumeID3, ProvisioningStatus: coreapi.ResourceProvisioningStatusDeprovisioning},
		{ID: testVolumeID4, ProvisioningStatus: coreapi.ResourceProvisioningStatusError, Message: message},
		{ID: testVolumeID5, ProvisioningStatus: coreapi.ResourceProvisioningStatusPending},
	}, instanceObject.Status.Volumes)
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
