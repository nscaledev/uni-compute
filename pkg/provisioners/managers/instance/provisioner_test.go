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
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

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
	current := &regionapi.ServerV2Read{
		Spec: provisioner.GenerateServerUpdateRequest().Spec,
	}

	updated, err := provisioner.CreateOrUpdateServer(t.Context(), nil, current)

	require.NoError(t, err)
	assert.Same(t, current, updated)
}

func TestNeedsRebuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		current  *regionapi.ServerV2Spec
		desired  *regionapi.ServerV2Spec
		expected bool
	}{
		{
			name: "same spec",
			current: &regionapi.ServerV2Spec{
				FlavorId: "flavor-1",
				ImageId:  "image-1",
			},
			desired: &regionapi.ServerV2Spec{
				FlavorId: "flavor-1",
				ImageId:  "image-1",
			},
			expected: false,
		},
		{
			name: "flavor change",
			current: &regionapi.ServerV2Spec{
				FlavorId: "flavor-1",
				ImageId:  "image-1",
			},
			desired: &regionapi.ServerV2Spec{
				FlavorId: "flavor-2",
				ImageId:  "image-1",
			},
			expected: true,
		},
		{
			name: "image change",
			current: &regionapi.ServerV2Spec{
				FlavorId: "flavor-1",
				ImageId:  "image-1",
			},
			desired: &regionapi.ServerV2Spec{
				FlavorId: "flavor-1",
				ImageId:  "image-2",
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
