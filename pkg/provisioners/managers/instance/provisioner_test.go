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
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newProvisionerForTest(sshCertificateAuthorityID *string) *instance.Provisioner {
	return instance.NewProvisionerForTest(unikornv1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-1",
			Labels: map[string]string{
				coreconstants.NameLabel:      "test-instance",
				regionconstants.NetworkLabel: "network-1",
			},
		},
		Spec: unikornv1.ComputeInstanceSpec{
			MachineGeneric: unikornv1core.MachineGeneric{
				FlavorID: "flavor-1",
				ImageID:  "image-1",
			},
			Networking: &unikornv1.ComputeInstanceNetworking{
				PublicIP:         true,
				SecurityGroupIDs: []string{"security-group-1"},
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

	request := provisioner.GenerateServerCreateRequest()

	require.NotNil(t, request.Spec.SshCertificateAuthorityId)
	assert.Equal(t, sshCertificateAuthorityID, *request.Spec.SshCertificateAuthorityId)
	assert.Equal(t, "network-1", request.Spec.NetworkId)
	assert.Equal(t, "flavor-1", request.Spec.FlavorId)
	assert.Equal(t, "image-1", request.Spec.ImageId)
	require.NotNil(t, request.Spec.Networking)
	require.NotNil(t, request.Metadata.Tags)
	assert.Equal(t, constants.InstanceLabel, (*request.Metadata.Tags)[0].Name)
}

func TestCreateOrUpdateServerIgnoresSSHCertificateAuthorityOnlyChange(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(ptr.To("ssh-ca-1"))
	request := provisioner.GenerateServerUpdateRequest()
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

func TestCreateOrUpdateServerDeletesAndYieldsOnNameChangeWithSameSpec(t *testing.T) {
	t.Parallel()

	provisioner := newProvisionerForTest(nil)
	request := provisioner.GenerateServerUpdateRequest()

	var deleteCalled bool

	doer := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v2/servers/server-1", r.URL.Path)

		deleteCalled = true

		return &http.Response{
			StatusCode: http.StatusAccepted,
			Body:       io.NopCloser(http.NoBody),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})

	region, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(doer))
	require.NoError(t, err)

	current := &regionapi.ServerV2Read{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			Id:   "server-1",
			Name: "test-instance-old",
		},
		Spec: request.Spec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), region, current)

	require.ErrorIs(t, err, provisioners.ErrYield)
	assert.Nil(t, updated)
	assert.True(t, deleteCalled)
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
					FlavorId: "flavor-1",
					ImageId:  "image-1",
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: "flavor-1",
					ImageId:  "image-1",
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
					FlavorId: "flavor-1",
					ImageId:  "image-1",
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: "flavor-2",
					ImageId:  "image-1",
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
					FlavorId: "flavor-1",
					ImageId:  "image-1",
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: "flavor-1",
					ImageId:  "image-2",
				},
			},
			expected: true,
		},
		{
			name: "name change",
			current: &regionapi.ServerV2Read{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					Name: "test-instance",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: "flavor-1",
					ImageId:  "image-1",
				},
			},
			desired: &regionapi.ServerV2Update{
				Metadata: coreapi.ResourceWriteMetadata{
					Name: "test-instance-renamed",
				},
				Spec: regionapi.ServerV2Spec{
					FlavorId: "flavor-1",
					ImageId:  "image-1",
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
	require.NotNil(t, instanceObject.Status.PowerState)
	assert.Equal(t, "Running", string(*instanceObject.Status.PowerState))
}
