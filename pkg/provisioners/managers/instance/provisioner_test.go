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
	"github.com/unikorn-cloud/core/pkg/provisioners"
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
	testServerID  = "f3a4b5c6-d7e8-4f90-a123-3c4d5e6f7081"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCreateOrUpdateServerUpdatesOnImageOnlyDrift(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	currentSpec := request.Spec
	currentSpec.ImageId = idstest.MustParseImageID(testImageID2)

	var (
		deleteCalled bool
		putCalled    bool
	)

	doer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodDelete:
			deleteCalled = true
		case http.MethodPut:
			putCalled = true
		}

		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)

		response := regionapi.ServerV2Response{
			Metadata: coreapi.ProjectScopedResourceReadMetadata{
				Id:   testServerID,
				Name: request.Metadata.Name,
			},
			Spec: request.Spec,
		}
		body, err := json.Marshal(response)
		require.NoError(t, err)

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})

	region, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(doer))
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:   testServerID,
			Name: request.Metadata.Name,
		},
		Spec: currentSpec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), region, current)

	require.NoError(t, err)
	assert.NotNil(t, updated)
	assert.True(t, putCalled)
	assert.False(t, deleteCalled)
}

func TestCreateOrUpdateServerDeletesAndYieldsOnFlavorDrift(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	// Flavor drift is the recreate trigger: the controller must delete the
	// backing server and yield rather than update it in place.
	currentSpec := request.Spec
	currentSpec.FlavorId = idstest.MustParseFlavorID(testFlavorID2)

	var (
		deleteCalled bool
		putCalled    bool
	)

	doer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodDelete:
			deleteCalled = true
		case http.MethodPut:
			putCalled = true
		}

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})

	region, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(doer))
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:   testServerID,
			Name: request.Metadata.Name,
		},
		Spec: currentSpec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), region, current)

	require.ErrorIs(t, err, provisioners.ErrYield)
	assert.Nil(t, updated)
	assert.True(t, deleteCalled)
	assert.False(t, putCalled)
}

func TestCreateOrUpdateServerDeletesAndYieldsOnSSHCertificateAuthorityDrift(t *testing.T) {
	t.Parallel()

	// The instance's desired CA differs from the CA region reports for the live
	// server. Since region's update body carries no CA field, the only way to
	// apply the change is to delete and recreate the backing server, exactly like
	// flavor drift.
	provisioner := newProvisionerForTest(ptr.To("ssh-ca-2"))
	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	var (
		deleteCalled bool
		putCalled    bool
	)

	doer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodDelete:
			deleteCalled = true
		case http.MethodPut:
			putCalled = true
		}

		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})

	region, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(doer))
	require.NoError(t, err)

	// Same spec, but the live server reports a different CA than the instance
	// desires.
	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:   testServerID,
			Name: request.Metadata.Name,
		},
		Spec: request.Spec,
		Status: regionapi.ServerV2Status{
			SshCertificateAuthorityId: ptr.To("ssh-ca-1"),
		},
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), region, current)

	require.ErrorIs(t, err, provisioners.ErrYield)
	assert.Nil(t, updated)
	assert.True(t, deleteCalled)
	assert.False(t, putCalled)
}

func TestCreateOrUpdateServerDoesNotRecreateOnUserDataDrift(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	request, err := provisioner.GenerateServerUpdateRequest()
	require.NoError(t, err)

	// DELIBERATE, FINAL decision: userData drift must NOT trigger a rebuild OR a
	// delete/recreate — it is applied in place. This follows upstream commit
	// db299db ("Don't Rebuild Servers for User Data"): recreating a running
	// server just because its userData changed is destructive and surprising.
	// Only image drift rebuilds in place; only flavor drift deletes/recreates.
	// This test pins the no-recreate half of that contract: it asserts that
	// userData-only drift issues NO DELETE. Do not "fix" this back to recreate.
	currentSpec := request.Spec
	currentSpec.UserData = ptr.To([]byte("#cloud-config\nusers: [changed]\n"))

	var (
		deleteCalled bool
		putCalled    bool
	)

	doer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.Method {
		case http.MethodDelete:
			deleteCalled = true
		case http.MethodPut:
			putCalled = true
		}

		// userData drift falls through to the in-place update (PUT), never a
		// DELETE/recreate.
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v2/servers/"+testServerID, r.URL.Path)

		response := regionapi.ServerV2Response{
			Metadata: coreapi.ProjectScopedResourceReadMetadata{
				Id:   testServerID,
				Name: request.Metadata.Name,
			},
			Spec: request.Spec,
		}
		body, err := json.Marshal(response)
		require.NoError(t, err)

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})

	region, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(doer))
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:   testServerID,
			Name: request.Metadata.Name,
		},
		Spec: currentSpec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), region, current)

	require.NoError(t, err)
	assert.NotNil(t, updated)
	// The key assertion: userData drift does NOT delete/recreate the server.
	assert.False(t, deleteCalled)
	assert.True(t, putCalled)
}

func TestNeedsRecreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		current   *regionapi.ServerV2Read
		desired   *regionapi.ServerV2Update
		desiredCA *string
		expected  bool
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
			name: "image only change does not recreate",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID2),
				},
			},
			expected: false,
		},
		{
			// DELIBERATE, FINAL decision: userData drift must NOT be a recreate
			// trigger. This follows upstream commit db299db ("Don't Rebuild
			// Servers for User Data") — rebuilding/recreating a server on
			// userData change is destructive and surprising, so userData is
			// applied in place instead. needsRecreateSpec compares FlavorId only;
			// do NOT add UserData here to "fix" this — that would reintroduce the
			// rejected recreate-on-userData behaviour. This case pins that: if
			// UserData were added to the recreate trigger, expected==false fails.
			name: "user data only change does not recreate",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
					UserData: ptr.To([]byte("#cloud-config\nusers: []\n")),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
					UserData: ptr.To([]byte("#cloud-config\nusers: [changed]\n")),
				},
			},
			expected: false,
		},
		{
			// SSH CA drift is replacement-worthy: region's update body has no CA
			// field, so a changed CA can only reach the backing server through a
			// delete/recreate. The live CA is read from region's status.
			name: "ssh ca change recreates",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
				Status: regionapi.ServerV2Status{
					SshCertificateAuthorityId: ptr.To("ssh-ca-1"),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desiredCA: ptr.To("ssh-ca-2"),
			expected:  true,
		},
		{
			name: "ssh ca set from unset recreates",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desiredCA: ptr.To("ssh-ca-1"),
			expected:  true,
		},
		{
			name: "ssh ca unset from set recreates",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
				Status: regionapi.ServerV2Status{
					SshCertificateAuthorityId: ptr.To("ssh-ca-1"),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desiredCA: nil,
			expected:  true,
		},
		{
			name: "ssh ca unchanged does not recreate",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
				Status: regionapi.ServerV2Status{
					SshCertificateAuthorityId: ptr.To("ssh-ca-1"),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desiredCA: ptr.To("ssh-ca-1"),
			expected:  false,
		},
		{
			// Both sides have no CA at all (nil status, nil desired). Normalising
			// nil to "" means there is no drift, so this must not recreate.
			name: "ssh ca both unset does not recreate",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
			},
			desiredCA: nil,
			expected:  false,
		},
		{
			// A combined image+CA update must still recreate: image-only drift
			// rebuilds in place, but the CA change forces delete/recreate so the
			// new CA actually reaches the backing server.
			name: "image and ssh ca change recreates",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID),
				},
				Status: regionapi.ServerV2Status{
					SshCertificateAuthorityId: ptr.To("ssh-ca-1"),
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{Name: "test-instance"},
				Spec: regionapi.ServerV2Spec{
					FlavorId: idstest.MustParseFlavorID(testFlavorID),
					ImageId:  idstest.MustParseImageID(testImageID2),
				},
			},
			desiredCA: ptr.To("ssh-ca-2"),
			expected:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, instance.NeedsRecreate(test.current, test.desired, test.desiredCA))
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
	require.NotNil(t, instanceObject.Status.PowerState)
	assert.Equal(t, "Running", string(*instanceObject.Status.PowerState))
}

// TestConvertPowerStateRoundTrip verifies every known region phase round-trips
// from the regionapi enum (API surface) to the regionv1 enum (CR surface).
// Unknown phases must return nil — see the comment in convertPowerState for
// the rationale.
func TestConvertPowerStateRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *regionapi.InstanceLifecyclePhase
		want *regionv1.InstanceLifecyclePhase
	}{
		{name: "nil input", in: nil, want: nil},
		{name: "pending", in: ptr.To(regionapi.InstanceLifecyclePhasePending), want: ptr.To(regionv1.InstanceLifecyclePhasePending)},
		{name: "queued", in: ptr.To(regionapi.InstanceLifecyclePhaseQueued), want: ptr.To(regionv1.InstanceLifecyclePhaseQueued)},
		{name: "building", in: ptr.To(regionapi.InstanceLifecyclePhaseBuilding), want: ptr.To(regionv1.InstanceLifecyclePhaseBuilding)},
		{name: "running", in: ptr.To(regionapi.InstanceLifecyclePhaseRunning), want: ptr.To(regionv1.InstanceLifecyclePhaseRunning)},
		{name: "stopping", in: ptr.To(regionapi.InstanceLifecyclePhaseStopping), want: ptr.To(regionv1.InstanceLifecyclePhaseStopping)},
		{name: "stopped", in: ptr.To(regionapi.InstanceLifecyclePhaseStopped), want: ptr.To(regionv1.InstanceLifecyclePhaseStopped)},
		{name: "unknown future phase falls through to nil", in: ptr.To(regionapi.InstanceLifecyclePhase("FutureUnknown")), want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := instance.ConvertPowerState(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}
