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
	"go.uber.org/mock/gomock"

	computev1 "github.com/unikorn-cloud/compute/pkg/apis/unikorn/v1alpha1"
	computeapi "github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/pkg/server/handler/instance"
	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreerrors "github.com/unikorn-cloud/core/pkg/server/errors"
	identityids "github.com/unikorn-cloud/identity/pkg/ids"
	identityapi "github.com/unikorn-cloud/identity/pkg/openapi"
	identitymock "github.com/unikorn-cloud/identity/pkg/openapi/mock"
	"github.com/unikorn-cloud/identity/pkg/rbac"
	regionv1 "github.com/unikorn-cloud/region/pkg/apis/unikorn/v1alpha1"
	regionconstants "github.com/unikorn-cloud/region/pkg/constants"
	idstest "github.com/unikorn-cloud/region/pkg/ids/idstest"
	regionapi "github.com/unikorn-cloud/region/pkg/openapi"
	regionuserdata "github.com/unikorn-cloud/region/pkg/userdata"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	organizationID       = "d4600d6e-e965-4b44-a808-84fb2fa36702"
	projectID            = "cae219d7-10e5-4601-8c2c-ee7e066b93ce"
	nonexistentProjectID = "f1e2d3c4-b5a6-4798-8a9b-0c1d2e3f4a5b"
	instanceFlavorID     = "c7568e2d-f9ab-453d-9a3a-51375f78426b"
)

// aclWithOrgScopeCreate grants compute:instances/Create at organization scope,
// so Create must verify the project via the identity API.
func aclWithOrgScopeCreate() *identityapi.Acl {
	return &identityapi.Acl{
		Organizations: &identityapi.AclOrganizationList{
			{
				Id: organizationID,
				Endpoints: &identityapi.AclEndpoints{
					{
						Name:       "compute:instances",
						Operations: identityapi.AclOperations{identityapi.Create},
					},
				},
			},
		},
	}
}

// minimalInstanceCreateRequest returns an InstanceCreate request body with
// the given organization and project IDs.
func minimalInstanceCreateRequest(orgID, projID string) *computeapi.InstanceCreate {
	return &computeapi.InstanceCreate{
		Metadata: coreapi.ResourceWriteMetadata{
			Name: "test-instance",
		},
		Spec: computeapi.InstanceCreateSpec{
			OrganizationId: identityids.MustParseOrganizationID(orgID),
			ProjectId:      identityids.MustParseProjectID(projID),
		},
	}
}

// TestInstanceCreateRBACOrgScopedProjectNotFound verifies that Create returns a
// 404 Not Found when the caller has org-scoped ACL but supplies a project ID
// that does not exist.
func TestInstanceCreateRBACOrgScopedProjectNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	mockIdentity := identitymock.NewMockClientWithResponsesInterface(ctrl)
	mockIdentity.EXPECT().
		GetApiV1OrganizationsOrganizationIDProjectsProjectIDWithResponse(gomock.Any(), identityids.MustParseOrganizationID(organizationID), identityids.MustParseProjectID(nonexistentProjectID)).
		Return(&identityapi.GetApiV1OrganizationsOrganizationIDProjectsProjectIDResponse{
			HTTPResponse: &http.Response{StatusCode: http.StatusNotFound},
		}, nil)

	c := instance.NewClient(nil, "", mockIdentity, nil)

	ctx := rbac.NewContext(t.Context(), aclWithOrgScopeCreate())

	_, err := c.Create(ctx, minimalInstanceCreateRequest(organizationID, nonexistentProjectID))

	require.Error(t, err)
	require.True(t, coreerrors.IsHTTPNotFound(err), "expected 404 not found, got: %v", err)
}

func flavorWithGPU(count int) *regionapi.Flavor {
	return &regionapi.Flavor{
		Spec: regionapi.FlavorSpec{
			Gpu: &regionapi.GpuSpec{
				PhysicalCount: count,
			},
		},
	}
}

func flavorWithoutGPU() *regionapi.Flavor {
	return &regionapi.Flavor{Spec: regionapi.FlavorSpec{}}
}

func allocationKind(list identityapi.ResourceAllocationList, kind string) (int, bool) {
	for _, a := range list {
		if a.Kind == kind {
			return a.Committed, true
		}
	}

	return 0, false
}

// TestGenerateAllocationNoPublicIP verifies that generateAllocation includes
// floatingips with committed=0 when publicIP is false.
func TestGenerateAllocationNoPublicIP(t *testing.T) {
	t.Parallel()

	c := instance.NewClient(nil, "", nil, nil)
	alloc := c.GenerateAllocation(flavorWithGPU(2), false)

	committed, ok := allocationKind(alloc, "floatingips")
	assert.True(t, ok, "floatingips entry should be present")
	assert.Equal(t, 0, committed)

	committed, ok = allocationKind(alloc, "gpus")
	assert.True(t, ok, "gpus entry should be present")
	assert.Equal(t, 2, committed)

	_, ok = allocationKind(alloc, "servers")
	assert.True(t, ok, "servers entry should be present")
}

// TestGenerateAllocationWithPublicIP verifies that generateAllocation includes
// floatingips with committed=1 when publicIP is true.
func TestGenerateAllocationWithPublicIP(t *testing.T) {
	t.Parallel()

	c := instance.NewClient(nil, "", nil, nil)
	alloc := c.GenerateAllocation(flavorWithoutGPU(), true)

	committed, ok := allocationKind(alloc, "floatingips")
	assert.True(t, ok, "floatingips entry should be present")
	assert.Equal(t, 1, committed)
}

func makeFlavorVM() *regionapi.Flavor {
	return &regionapi.Flavor{Spec: regionapi.FlavorSpec{}}
}

func makeFlavorBaremetal() *regionapi.Flavor {
	return &regionapi.Flavor{Spec: regionapi.FlavorSpec{Baremetal: ptr.To(true)}}
}

func makeImage(v regionapi.ImageVirtualization) *regionapi.Image {
	return &regionapi.Image{Spec: regionapi.ImageSpec{Virtualization: v}}
}

// TestValidateVirtualization covers all combinations of flavor type and image
// virtualization hint.
func TestValidateVirtualization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flavor      *regionapi.Flavor
		image       *regionapi.Image
		expectError bool
	}{
		{
			name:        "vm flavor with virtualized image",
			flavor:      makeFlavorVM(),
			image:       makeImage(regionapi.ImageVirtualizationVirtualized),
			expectError: false,
		},
		{
			name:        "vm flavor with any image",
			flavor:      makeFlavorVM(),
			image:       makeImage(regionapi.ImageVirtualizationAny),
			expectError: false,
		},
		{
			name:        "vm flavor with baremetal image",
			flavor:      makeFlavorVM(),
			image:       makeImage(regionapi.ImageVirtualizationBaremetal),
			expectError: true,
		},
		{
			name:        "baremetal flavor with baremetal image",
			flavor:      makeFlavorBaremetal(),
			image:       makeImage(regionapi.ImageVirtualizationBaremetal),
			expectError: false,
		},
		{
			name:        "baremetal flavor with any image",
			flavor:      makeFlavorBaremetal(),
			image:       makeImage(regionapi.ImageVirtualizationAny),
			expectError: false,
		},
		{
			name:        "baremetal flavor with virtualized image",
			flavor:      makeFlavorBaremetal(),
			image:       makeImage(regionapi.ImageVirtualizationVirtualized),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := instance.ValidateVirtualization(tc.flavor, tc.image)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetImageUsesReadyAvailableRegionImages(t *testing.T) {
	t.Parallel()

	const (
		regionID = "a73e9c26-af56-4562-8352-9512e0586f3b"
		imageID  = "c29b7e35-3181-4ba4-b3de-98afbb2ef6ac"
	)

	softwareVersions := regionapi.SoftwareVersions{
		"kubernetes": "v1.33.0",
	}
	images := []regionapi.Image{
		{
			Metadata: coreapi.StaticResourceMetadata{
				Id: imageID,
			},
			Spec: regionapi.ImageSpec{
				SoftwareVersions: &softwareVersions,
			},
			Status: regionapi.ImageStatus{
				State: regionapi.ImageStateReady,
			},
		},
	}

	body, err := json.Marshal(images)
	require.NoError(t, err)

	regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/v1/organizations/"+organizationID+"/regions/"+regionID+"/images", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})))
	require.NoError(t, err)

	client := instance.NewClient(nil, "", nil, regionClient)
	image, err := client.GetImage(
		t.Context(),
		identityids.MustParseOrganizationID(organizationID),
		idstest.MustParseRegionID(regionID),
		idstest.MustParseImageID(imageID),
	)
	require.NoError(t, err)
	require.NotNil(t, image)
	assert.Equal(t, imageID, image.Metadata.Id)
	assert.Equal(t, softwareVersions, *image.Spec.SoftwareVersions)
	assert.Equal(t, regionapi.ImageStateReady, image.Status.State)
}

type regionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f regionRoundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestValidateFlavorAndImageReadinessAndCompatibility(t *testing.T) {
	t.Parallel()

	softwareVersions := regionapi.SoftwareVersions{
		"kubernetes": "v1.33.0",
	}
	compatibleFlavor := regionapi.Flavor{
		Spec: regionapi.FlavorSpec{
			Architecture: regionapi.ArchitectureX8664,
			Disk:         20,
		},
	}
	compatibleImage := regionapi.Image{
		Spec: regionapi.ImageSpec{
			Architecture:     regionapi.ArchitectureX8664,
			SizeGiB:          10,
			SoftwareVersions: &softwareVersions,
			Virtualization:   regionapi.ImageVirtualizationVirtualized,
		},
		Status: regionapi.ImageStatus{
			State: regionapi.ImageStateReady,
		},
	}

	tests := []struct {
		name        string
		mutate      func(*regionapi.Flavor, *regionapi.Image)
		expectError bool
	}{
		{
			name: "compatible",
		},
		{
			name: "not ready",
			mutate: func(_ *regionapi.Flavor, image *regionapi.Image) {
				image.Status.State = regionapi.ImageStateCreating
			},
			expectError: true,
		},
		{
			name: "architecture mismatch",
			mutate: func(_ *regionapi.Flavor, image *regionapi.Image) {
				image.Spec.Architecture = regionapi.ArchitectureAarch64
			},
			expectError: true,
		},
		{
			name: "disk too small",
			mutate: func(flavor *regionapi.Flavor, _ *regionapi.Image) {
				flavor.Spec.Disk = 9
			},
			expectError: true,
		},
		{
			name: "virtualization mismatch",
			mutate: func(_ *regionapi.Flavor, image *regionapi.Image) {
				image.Spec.Virtualization = regionapi.ImageVirtualizationBaremetal
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			flavor := compatibleFlavor
			image := compatibleImage

			if tc.mutate != nil {
				tc.mutate(&flavor, &image)
			}

			err := instance.ValidateFlavorAndImage(&flavor, &image)
			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestInstanceCreateRBACNoPermissions verifies that Create returns a forbidden
// error when the caller has no relevant permissions.
func TestInstanceCreateRBACNoPermissions(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	mockIdentity := identitymock.NewMockClientWithResponsesInterface(ctrl)
	// No EXPECT calls — the identity API must not be contacted.

	c := instance.NewClient(nil, "", mockIdentity, nil)

	ctx := rbac.NewContext(t.Context(), &identityapi.Acl{})

	_, err := c.Create(ctx, minimalInstanceCreateRequest(organizationID, projectID))

	require.Error(t, err)
	require.True(t, coreerrors.IsForbidden(err), "expected forbidden, got: %v", err)
}

func TestConvertReturnsMACAddress(t *testing.T) {
	t.Parallel()

	resource := &computev1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "instance-1",
			Labels: map[string]string{
				coreconstants.OrganizationLabel: organizationID,
				coreconstants.ProjectLabel:      projectID,
				regionconstants.RegionLabel:     "region-1",
				regionconstants.NetworkLabel:    "network-1",
			},
		},
		Spec: computev1.ComputeInstanceSpec{
			MachineGeneric: unikornv1core.MachineGeneric{
				FlavorID: "c7568e2d-f9ab-453d-9a3a-51375f78426b",
				ImageID:  "a10e30e8-006a-48e6-a3c7-3c9416891f31",
			},
		},
		Status: computev1.ComputeInstanceStatus{
			PrivateIP:  ptr.To("192.168.0.42"),
			PublicIP:   ptr.To("203.0.113.10"),
			MACAddress: ptr.To("fa:16:3e:12:34:56"),
		},
	}

	result, err := instance.Convert(resource)
	require.NoError(t, err)

	require.NotNil(t, result)
	require.Equal(t, resource.Status.PrivateIP, result.Status.PrivateIP)
	require.Equal(t, resource.Status.PublicIP, result.Status.PublicIP)
	require.Equal(t, resource.Status.MACAddress, result.Status.MacAddress)
}

// TestValidateUserData pins the shared boundary contract this service relies
// on: the create path delegates to region's handler helper, and these cases
// document what the instances API accepts and rejects.
func TestValidateUserData(t *testing.T) {
	t.Parallel()

	validCloudConfig := []byte("#cloud-config\nusers: []\n")
	validMultipart := []byte("Content-Type: multipart/mixed; boundary=\"BOUNDARY\"\r\nMIME-Version: 1.0\r\n\r\n--BOUNDARY\r\nContent-Type: text/x-shellscript\r\n\r\n#!/bin/sh\necho hi\r\n--BOUNDARY--\r\n")
	validMultipartLowerCase := []byte("content-type: multipart/mixed; boundary=\"BOUNDARY\"\r\nmime-version: 1.0\r\n\r\n--BOUNDARY\r\nContent-Type: text/x-shellscript\r\n\r\n#!/bin/sh\necho hi\r\n--BOUNDARY--\r\n")
	validScript := []byte("#!/bin/sh\necho hi\n")
	gzipData := []byte{0x1f, 0x8b, 0x08}
	invalid := []byte("plain text")

	tests := []struct {
		name      string
		managed   bool
		userData  *[]byte
		wantError bool
	}{
		{name: "no ssh ca no user data", managed: false, userData: nil, wantError: false},
		{name: "no ssh ca empty user data", managed: false, userData: ptr.To([]byte{}), wantError: false},
		{name: "no ssh ca cloud config", managed: false, userData: &validCloudConfig, wantError: false},
		{name: "no ssh ca multipart", managed: false, userData: &validMultipart, wantError: false},
		{name: "no ssh ca script", managed: false, userData: &validScript, wantError: false},
		// Gzip user-data is passed to the platform unmodified when no managed
		// augmentation occurs, so it must not be rejected at the boundary.
		{name: "no ssh ca gzip", managed: false, userData: &gzipData, wantError: false},
		{name: "no ssh ca unrecognized", managed: false, userData: &invalid, wantError: true},
		{name: "no user data", managed: true, userData: nil, wantError: false},
		{name: "empty user data", managed: true, userData: ptr.To([]byte{}), wantError: false},
		{name: "cloud config", managed: true, userData: &validCloudConfig, wantError: false},
		{name: "multipart", managed: true, userData: &validMultipart, wantError: false},
		{name: "multipart lowercase", managed: true, userData: &validMultipartLowerCase, wantError: false},
		{name: "script", managed: true, userData: &validScript, wantError: false},
		{name: "gzip", managed: true, userData: &gzipData, wantError: true},
		{name: "unrecognized", managed: true, userData: &invalid, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := regionuserdata.Validate(tc.userData, tc.managed)

			if tc.wantError {
				require.Error(t, err)
				require.ErrorContains(t, err, "userData must be a recognized cloud-init format")

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateUserDataSurfacesParserReason(t *testing.T) {
	t.Parallel()

	err := regionuserdata.Validate(ptr.To([]byte("plain text")), false)

	require.Error(t, err)
	require.ErrorContains(t, err, "userData must be a recognized cloud-init format: unsupported userData format")
	// The internal consistency-error sentinel must be stripped from the caller-facing message.
	require.NotContains(t, err.Error(), "consistency error")
}

func TestValidateUserDataForManagedAugmentation(t *testing.T) {
	t.Parallel()

	sshCAID := idstest.MustParseSSHCertificateAuthorityID("f1e2d3c4-b5a6-4798-8a9b-0c1d2e3f4a5b")
	gzipData := []byte{0x1f, 0x8b, 0x08}
	invalid := []byte("plain text")
	validCloudConfig := []byte("#cloud-config\nusers: []\n")

	tests := []struct {
		name      string
		sshCAID   *regionapi.SshCertificateAuthorityId
		userData  *[]byte
		wantError bool
	}{
		// Updates must not re-validate user-data without a CA: it is only consumed
		// at initial bootstrap, and re-validating would block updates of instances
		// whose user-data predates create-time validation.
		{name: "no ssh ca unrecognized", sshCAID: nil, userData: &invalid, wantError: false},
		{name: "no ssh ca gzip", sshCAID: nil, userData: &gzipData, wantError: false},
		{name: "no user data", sshCAID: &sshCAID, userData: nil, wantError: false},
		{name: "cloud config", sshCAID: &sshCAID, userData: &validCloudConfig, wantError: false},
		{name: "gzip", sshCAID: &sshCAID, userData: &gzipData, wantError: true},
		{name: "unrecognized", sshCAID: &sshCAID, userData: &invalid, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := instance.ValidateUserDataForManagedAugmentation(tc.sshCAID, tc.userData)

			if tc.wantError {
				require.Error(t, err)
				require.ErrorContains(t, err, "userData must be a recognized cloud-init format")

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateSSHCertificateAuthorityScope(t *testing.T) {
	t.Parallel()

	err := instance.ValidateSSHCertificateAuthorityScope(&regionapi.SshCertificateAuthorityV2Response{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			OrganizationId: organizationID,
			ProjectId:      projectID,
		},
	}, identityids.MustParseOrganizationID(organizationID), identityids.MustParseProjectID(projectID))
	require.NoError(t, err)
}

func TestValidateSSHCertificateAuthorityScopeProjectMismatch(t *testing.T) {
	t.Parallel()

	err := instance.ValidateSSHCertificateAuthorityScope(&regionapi.SshCertificateAuthorityV2Response{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			OrganizationId: organizationID,
			ProjectId:      "different-project",
		},
	}, identityids.MustParseOrganizationID(organizationID), identityids.MustParseProjectID(projectID))
	require.Error(t, err)
	require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
}

func TestValidateSSHCertificateAuthorityScopeOrganizationMismatch(t *testing.T) {
	t.Parallel()

	err := instance.ValidateSSHCertificateAuthorityScope(&regionapi.SshCertificateAuthorityV2Response{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			OrganizationId: "different-organization",
			ProjectId:      projectID,
		},
	}, identityids.MustParseOrganizationID(organizationID), identityids.MustParseProjectID(projectID))
	require.Error(t, err)
	require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
}

func TestValidateSecurityGroupNetwork(t *testing.T) {
	t.Parallel()

	networkID := idstest.MustParseNetworkID("a1b2c3d4-e5f6-4789-8abc-def012345678")

	err := instance.ValidateSecurityGroupNetwork(&regionapi.SecurityGroupV2Read{
		Status: regionapi.SecurityGroupV2Status{
			NetworkId: networkID.String(),
		},
	}, networkID)
	require.NoError(t, err)
}

func TestValidateSecurityGroupNetworkMismatch(t *testing.T) {
	t.Parallel()

	err := instance.ValidateSecurityGroupNetwork(&regionapi.SecurityGroupV2Read{
		Status: regionapi.SecurityGroupV2Status{
			NetworkId: "11111111-1111-4111-a111-111111111111",
		},
	}, idstest.MustParseNetworkID("a1b2c3d4-e5f6-4789-8abc-def012345678"))
	require.Error(t, err)
	require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
}

func TestValidateVolumes(t *testing.T) {
	t.Parallel()

	const (
		regionID            = "a73e9c26-af56-4562-8352-9512e0586f3b"
		otherRegionID       = "22222222-2222-4222-8222-222222222222"
		volumeID            = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID           = "a1b2c3d4-e5f6-4789-8abc-def012345678"
		otherNetworkID      = "11111111-1111-4111-a111-111111111111"
		otherOrganizationID = "33333333-3333-4333-8333-333333333333"
		otherProjectID      = "44444444-4444-4444-8444-444444444444"
	)

	tests := []struct {
		name           string
		organizationID string
		projectID      string
		regionID       string
		networkID      string
	}{
		{name: "organization mismatch", organizationID: otherOrganizationID, projectID: projectID, regionID: regionID, networkID: networkID},
		{name: "project mismatch", organizationID: organizationID, projectID: otherProjectID, regionID: regionID, networkID: networkID},
		{name: "region mismatch", organizationID: organizationID, projectID: projectID, regionID: otherRegionID, networkID: networkID},
		{name: "network mismatch", organizationID: organizationID, projectID: projectID, regionID: regionID, networkID: otherNetworkID},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(regionapi.VolumeV2Response{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					OrganizationId: test.organizationID,
					ProjectId:      test.projectID,
				},
				Spec: regionapi.VolumeV2Spec{NetworkId: idstest.MustParseNetworkID(test.networkID)},
				Status: regionapi.VolumeV2Status{
					RegionId: idstest.MustParseRegionID(test.regionID),
				},
			})
			require.NoError(t, err)

			regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				require.Equal(t, "/api/v2/volumes/"+volumeID, r.URL.Path)

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})))
			require.NoError(t, err)

			volumes := computeapi.InstanceVolumeList{idstest.MustParseVolumeID(volumeID)}
			err = instance.NewClient(nil, "", nil, regionClient).ValidateVolumes(
				t.Context(),
				&volumes,
				nil,
				identityids.MustParseOrganizationID(organizationID),
				identityids.MustParseProjectID(projectID),
				idstest.MustParseRegionID(regionID),
				idstest.MustParseNetworkID(networkID),
				idstest.MustParseFlavorID(instanceFlavorID),
			)

			require.Error(t, err)
			require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
		})
	}
}

func TestValidateVolumesRejectsDuplicates(t *testing.T) {
	t.Parallel()

	id := idstest.MustParseVolumeID("f5c1ccbf-cbdd-49fd-b5b9-90902464851f")
	volumes := computeapi.InstanceVolumeList{id, id}
	err := instance.NewClient(nil, "", nil, nil).ValidateVolumes(
		t.Context(),
		&volumes,
		nil,
		identityids.MustParseOrganizationID(organizationID),
		identityids.MustParseProjectID(projectID),
		idstest.MustParseRegionID("a73e9c26-af56-4562-8352-9512e0586f3b"),
		idstest.MustParseNetworkID("a1b2c3d4-e5f6-4789-8abc-def012345678"),
		idstest.MustParseFlavorID(instanceFlavorID),
	)

	require.Error(t, err)
	require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
}

func TestValidateVolumesHidesForbiddenReferences(t *testing.T) {
	t.Parallel()

	const (
		regionID  = "a73e9c26-af56-4562-8352-9512e0586f3b"
		volumeID  = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID = "a1b2c3d4-e5f6-4789-8abc-def012345678"
	)

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			body, err := json.Marshal(coreapi.Error{
				Error:            coreapi.NotFound,
				ErrorDescription: "resource not found",
			})
			require.NoError(t, err)

			regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})))
			require.NoError(t, err)

			volumes := computeapi.InstanceVolumeList{idstest.MustParseVolumeID(volumeID)}
			err = instance.NewClient(nil, "", nil, regionClient).ValidateVolumes(
				t.Context(),
				&volumes,
				nil,
				identityids.MustParseOrganizationID(organizationID),
				identityids.MustParseProjectID(projectID),
				idstest.MustParseRegionID(regionID),
				idstest.MustParseNetworkID(networkID),
				idstest.MustParseFlavorID(instanceFlavorID),
			)

			require.Error(t, err)
			require.True(t, coreerrors.IsHTTPNotFound(err), "expected 404, got: %v", err)
		})
	}
}

func TestValidateVolumesRejectsNewAttachedVolume(t *testing.T) {
	t.Parallel()

	const (
		regionID      = "a73e9c26-af56-4562-8352-9512e0586f3b"
		volumeID      = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID     = "a1b2c3d4-e5f6-4789-8abc-def012345678"
		volumeClassID = "44444444-4444-4444-8444-444444444444"
	)

	body, err := json.Marshal(map[string]any{
		"metadata": coreapi.ProjectScopedResourceReadMetadata{
			OrganizationId: organizationID,
			ProjectId:      projectID,
		},
		"spec": map[string]any{
			"networkId":     idstest.MustParseNetworkID(networkID),
			"volumeClassId": volumeClassID,
		},
		"status": map[string]any{
			"regionId":   idstest.MustParseRegionID(regionID),
			"attachedAt": metav1.Now(),
		},
	})
	require.NoError(t, err)

	for _, test := range []struct {
		name           string
		currentVolumes []computev1.ComputeInstanceVolumeSpec
		wantError      bool
	}{
		{name: "new volume", wantError: true},
		{name: "already desired", currentVolumes: []computev1.ComputeInstanceVolumeSpec{{ID: volumeID}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			classesBody, err := json.Marshal(regionapi.VolumeClassListV2Response{{
				Metadata: coreapi.StaticResourceMetadata{Id: volumeClassID},
				Spec: regionapi.VolumeClassV2Spec{
					SupportedFlavorIds: &[]regionapi.FlavorId{idstest.MustParseFlavorID(instanceFlavorID)},
				},
			}})
			require.NoError(t, err)

			regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				responseBody := body
				if r.URL.Path == "/api/v2/volumeclasses" {
					responseBody = classesBody
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(responseBody)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})))
			require.NoError(t, err)

			volumes := computeapi.InstanceVolumeList{idstest.MustParseVolumeID(volumeID)}
			err = instance.NewClient(nil, "", nil, regionClient).ValidateVolumes(
				t.Context(),
				&volumes,
				test.currentVolumes,
				identityids.MustParseOrganizationID(organizationID),
				identityids.MustParseProjectID(projectID),
				idstest.MustParseRegionID(regionID),
				idstest.MustParseNetworkID(networkID),
				idstest.MustParseFlavorID(instanceFlavorID),
			)

			if test.wantError {
				require.Error(t, err)
				require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateVolumesRejectsDeprovisioningVolume(t *testing.T) {
	t.Parallel()

	const (
		regionID  = "a73e9c26-af56-4562-8352-9512e0586f3b"
		volumeID  = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID = "a1b2c3d4-e5f6-4789-8abc-def012345678"
	)

	body, err := json.Marshal(regionapi.VolumeV2Response{
		Metadata: coreapi.ProjectScopedResourceReadMetadata{
			OrganizationId:     organizationID,
			ProjectId:          projectID,
			ProvisioningStatus: coreapi.ResourceProvisioningStatusDeprovisioning,
		},
		Spec: regionapi.VolumeV2Spec{NetworkId: idstest.MustParseNetworkID(networkID)},
		Status: regionapi.VolumeV2Status{
			RegionId: idstest.MustParseRegionID(regionID),
		},
	})
	require.NoError(t, err)

	regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})))
	require.NoError(t, err)

	volumes := computeapi.InstanceVolumeList{idstest.MustParseVolumeID(volumeID)}
	err = instance.NewClient(nil, "", nil, regionClient).ValidateVolumes(
		t.Context(),
		&volumes,
		nil,
		identityids.MustParseOrganizationID(organizationID),
		identityids.MustParseProjectID(projectID),
		idstest.MustParseRegionID(regionID),
		idstest.MustParseNetworkID(networkID),
		idstest.MustParseFlavorID(instanceFlavorID),
	)

	require.Error(t, err)
	require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
}

func TestValidateVolumesChecksVolumeClassFlavorAllowlist(t *testing.T) {
	t.Parallel()

	const (
		regionID      = "a73e9c26-af56-4562-8352-9512e0586f3b"
		volumeID      = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID     = "a1b2c3d4-e5f6-4789-8abc-def012345678"
		volumeClassID = "44444444-4444-4444-8444-444444444444"
	)

	allowed := []regionapi.FlavorId{idstest.MustParseFlavorID(instanceFlavorID)}
	disallowed := []regionapi.FlavorId{idstest.MustParseFlavorID("55555555-5555-4555-8555-555555555555")}
	empty := []regionapi.FlavorId{}
	requested := computeapi.InstanceVolumeList{idstest.MustParseVolumeID(volumeID)}

	for _, test := range []struct {
		name               string
		volumes            *computeapi.InstanceVolumeList
		currentVolumes     []computev1.ComputeInstanceVolumeSpec
		supportedFlavorIDs *[]regionapi.FlavorId
		advertisedClassID  string
		wantError          bool
		wantErrorContains  string
	}{
		{name: "allowed", volumes: &requested, supportedFlavorIDs: &allowed, advertisedClassID: volumeClassID},
		{name: "unsupported", volumes: &requested, supportedFlavorIDs: &disallowed, advertisedClassID: volumeClassID, wantError: true, wantErrorContains: "volume class does not support the server flavor"},
		{name: "omitted skips validation", currentVolumes: []computev1.ComputeInstanceVolumeSpec{{ID: volumeID}}, supportedFlavorIDs: &disallowed, advertisedClassID: volumeClassID},
		{name: "empty allowlist", volumes: &requested, supportedFlavorIDs: &empty, advertisedClassID: volumeClassID, wantError: true, wantErrorContains: "volume class does not support the server flavor"},
		{name: "omitted allowlist", volumes: &requested, advertisedClassID: volumeClassID, wantError: true, wantErrorContains: "volume class does not support the server flavor"},
		{name: "class absent from inventory", volumes: &requested, advertisedClassID: "66666666-6666-4666-8666-666666666666", wantError: true, wantErrorContains: volumeClassID},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			volumeBody, err := json.Marshal(regionapi.VolumeV2Response{
				Metadata: coreapi.ProjectScopedResourceReadMetadata{
					OrganizationId: organizationID,
					ProjectId:      projectID,
				},
				Spec: regionapi.VolumeV2Spec{
					NetworkId:     idstest.MustParseNetworkID(networkID),
					VolumeClassId: volumeClassID,
				},
				Status: regionapi.VolumeV2Status{RegionId: idstest.MustParseRegionID(regionID)},
			})
			require.NoError(t, err)

			classesBody, err := json.Marshal(regionapi.VolumeClassListV2Response{{
				Metadata: coreapi.StaticResourceMetadata{Id: test.advertisedClassID},
				Spec: regionapi.VolumeClassV2Spec{
					RegionId:           idstest.MustParseRegionID(regionID),
					SupportedFlavorIds: test.supportedFlavorIDs,
				},
			}})
			require.NoError(t, err)

			regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				responseBody := volumeBody

				if r.URL.Path == "/api/v2/volumeclasses" {
					require.Equal(t, []string{regionID}, r.URL.Query()["regionID"])

					responseBody = classesBody
				}

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(responseBody)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})))
			require.NoError(t, err)

			err = instance.NewClient(nil, "", nil, regionClient).ValidateVolumes(
				t.Context(),
				test.volumes,
				test.currentVolumes,
				identityids.MustParseOrganizationID(organizationID),
				identityids.MustParseProjectID(projectID),
				idstest.MustParseRegionID(regionID),
				idstest.MustParseNetworkID(networkID),
				idstest.MustParseFlavorID(instanceFlavorID),
			)

			if test.wantError {
				require.Error(t, err)
				require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
				assert.Contains(t, err.Error(), test.wantErrorContains)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateUpdateVolumesChecksRetainedVolumesAfterFlavorChange(t *testing.T) {
	t.Parallel()

	const (
		regionID        = "a73e9c26-af56-4562-8352-9512e0586f3b"
		volumeID        = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		networkID       = "a1b2c3d4-e5f6-4789-8abc-def012345678"
		volumeClassID   = "44444444-4444-4444-8444-444444444444"
		oldFlavorID     = "55555555-5555-4555-8555-555555555555"
		allowedFlavorID = "66666666-6666-4666-8666-666666666666"
	)

	t.Run("unchanged flavor skips validation", func(t *testing.T) {
		t.Parallel()

		err := instance.NewClient(nil, "", nil, nil).ValidateUpdateVolumes(
			t.Context(),
			nil,
			[]computev1.ComputeInstanceVolumeSpec{{ID: volumeID}},
			instanceFlavorID,
			identityids.MustParseOrganizationID(organizationID),
			identityids.MustParseProjectID(projectID),
			idstest.MustParseRegionID(regionID),
			idstest.MustParseNetworkID(networkID),
			idstest.MustParseFlavorID(instanceFlavorID),
		)
		require.NoError(t, err)
	})

	t.Run("changed flavor rejects incompatible retained volume", func(t *testing.T) {
		t.Parallel()

		volumeBody, err := json.Marshal(regionapi.VolumeV2Response{
			Spec: regionapi.VolumeV2Spec{VolumeClassId: volumeClassID},
		})
		require.NoError(t, err)

		classesBody, err := json.Marshal(regionapi.VolumeClassListV2Response{{
			Metadata: coreapi.StaticResourceMetadata{Id: volumeClassID},
			Spec: regionapi.VolumeClassV2Spec{
				SupportedFlavorIds: &[]regionapi.FlavorId{idstest.MustParseFlavorID(allowedFlavorID)},
			},
		}})
		require.NoError(t, err)

		regionClient, err := regionapi.NewClientWithResponses("http://region.example", regionapi.WithHTTPClient(regionRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			responseBody := volumeBody
			if r.URL.Path == "/api/v2/volumeclasses" {
				responseBody = classesBody
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(responseBody)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		})))
		require.NoError(t, err)

		err = instance.NewClient(nil, "", nil, regionClient).ValidateUpdateVolumes(
			t.Context(),
			nil,
			[]computev1.ComputeInstanceVolumeSpec{{ID: volumeID}},
			oldFlavorID,
			identityids.MustParseOrganizationID(organizationID),
			identityids.MustParseProjectID(projectID),
			idstest.MustParseRegionID(regionID),
			idstest.MustParseNetworkID(networkID),
			idstest.MustParseFlavorID(instanceFlavorID),
		)

		require.Error(t, err)
		require.True(t, coreerrors.IsUnprocessableContent(err), "expected 422, got: %v", err)
	})
}

func TestConvertVolumes(t *testing.T) {
	t.Parallel()

	const (
		volumeID        = "f5c1ccbf-cbdd-49fd-b5b9-90902464851f"
		removedVolumeID = "3a21348e-d20a-459c-a57f-d1b24c94ba7f"
	)

	device := "/dev/vdb"
	message := "detaching"

	resource := &computev1.ComputeInstance{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				coreconstants.OrganizationLabel: organizationID,
				coreconstants.ProjectLabel:      projectID,
				regionconstants.RegionLabel:     "region-1",
				regionconstants.NetworkLabel:    "network-1",
			},
		},
		Spec: computev1.ComputeInstanceSpec{
			MachineGeneric: unikornv1core.MachineGeneric{
				FlavorID: "c7568e2d-f9ab-453d-9a3a-51375f78426b",
				ImageID:  "a10e30e8-006a-48e6-a3c7-3c9416891f31",
			},
			Volumes: []computev1.ComputeInstanceVolumeSpec{{ID: volumeID}},
		},
		Status: computev1.ComputeInstanceStatus{
			Volumes: []computev1.ComputeInstanceVolumeStatus{
				{ID: volumeID, ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioned, Device: &device},
				{ID: removedVolumeID, ProvisioningStatus: coreapi.ResourceProvisioningStatusDeprovisioning, Message: message},
			},
		},
	}

	result, err := instance.Convert(resource)
	require.NoError(t, err)
	require.NotNil(t, result.Spec.Volumes)
	require.Len(t, *result.Spec.Volumes, 1)
	assert.Equal(t, volumeID, (*result.Spec.Volumes)[0].String())
	require.NotNil(t, result.Status.Volumes)
	assert.Equal(t, computeapi.InstanceVolumeStatusList{
		{Id: idstest.MustParseVolumeID(volumeID), ProvisioningStatus: coreapi.ResourceProvisioningStatusProvisioned, Device: &device},
		{Id: idstest.MustParseVolumeID(removedVolumeID), ProvisioningStatus: coreapi.ResourceProvisioningStatusDeprovisioning, Message: &message},
	}, *result.Status.Volumes)
}

// TestInstancePowerState verifies the handler-side projection of the instance's
// Active condition (the lifecycle/power axis mirrored from the backing server)
// onto the regionapi phase enum returned to API clients. An absent condition or
// an unrecognised reason drops to nil, mirroring the provisioner-side ingest.
func TestInstancePowerState(t *testing.T) {
	t.Parallel()

	instanceWithReason := func(reason regionv1.ActiveConditionReason) *computev1.ComputeInstance {
		c := &computev1.ComputeInstance{}
		c.SetActiveCondition(reason)

		return c
	}

	tests := []struct {
		name string
		in   *computev1.ComputeInstance
		want *regionapi.InstanceLifecyclePhase
	}{
		{name: "absent condition", in: &computev1.ComputeInstance{}, want: nil},
		{name: "pending", in: instanceWithReason(regionv1.ActiveConditionReasonPending), want: ptr.To(regionapi.InstanceLifecyclePhasePending)},
		{name: "queued", in: instanceWithReason(regionv1.ActiveConditionReasonQueued), want: ptr.To(regionapi.InstanceLifecyclePhaseQueued)},
		{name: "building", in: instanceWithReason(regionv1.ActiveConditionReasonBuilding), want: ptr.To(regionapi.InstanceLifecyclePhaseBuilding)},
		{name: "running", in: instanceWithReason(regionv1.ActiveConditionReasonRunning), want: ptr.To(regionapi.InstanceLifecyclePhaseRunning)},
		{name: "stopping", in: instanceWithReason(regionv1.ActiveConditionReasonStopping), want: ptr.To(regionapi.InstanceLifecyclePhaseStopping)},
		{name: "stopped", in: instanceWithReason(regionv1.ActiveConditionReasonStopped), want: ptr.To(regionapi.InstanceLifecyclePhaseStopped)},
		{name: "error", in: instanceWithReason(regionv1.ActiveConditionReasonError), want: ptr.To(regionapi.InstanceLifecyclePhaseError)},
		{name: "unknown reason falls through to nil", in: instanceWithReason("FutureUnknown"), want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := instance.InstancePowerState(tc.in)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}

			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}
