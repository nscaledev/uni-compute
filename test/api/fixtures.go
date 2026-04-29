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

//nolint:revive,staticcheck // dot imports are standard for Ginkgo/Gomega test code
package api

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	regionopenapi "github.com/unikorn-cloud/region/pkg/openapi"

	"k8s.io/utils/ptr"
)

// InstancePayloadBuilder builds instance payloads for testing using type-safe OpenAPI structs.
type InstancePayloadBuilder struct {
	instance openapi.InstanceCreate
	config   *TestConfig
}

// NewInstancePayload creates a new instance payload builder with defaults from config.
func NewInstancePayload() *InstancePayloadBuilder {
	config, err := LoadTestConfig()
	Expect(err).NotTo(HaveOccurred(), "Failed to load test configuration")

	timestamp := time.Now().Format("20060102-150405")
	uniqueName := fmt.Sprintf("testinstance-%s", timestamp)

	return &InstancePayloadBuilder{
		config: config,
		instance: openapi.InstanceCreate{
			Metadata: coreapi.ResourceWriteMetadata{
				Name:        uniqueName,
				Description: ptr.To("Test instance for API automation"),
			},
			Spec: openapi.InstanceCreateSpec{
				FlavorId:       config.FlavorID,
				ImageId:        config.ImageID,
				NetworkId:      config.NetworkID,
				OrganizationId: config.OrgID,
				ProjectId:      config.ProjectID,
			},
		},
	}
}

// WithName sets the instance name.
func (b *InstancePayloadBuilder) WithName(name string) *InstancePayloadBuilder {
	b.instance.Metadata.Name = name
	return b
}

// WithFlavorID sets the flavor ID.
func (b *InstancePayloadBuilder) WithFlavorID(flavorID string) *InstancePayloadBuilder {
	b.instance.Spec.FlavorId = flavorID
	return b
}

// WithImageID sets the image ID.
func (b *InstancePayloadBuilder) WithImageID(imageID string) *InstancePayloadBuilder {
	b.instance.Spec.ImageId = imageID
	return b
}

// WithNetworkID sets the network ID.
func (b *InstancePayloadBuilder) WithNetworkID(networkID string) *InstancePayloadBuilder {
	b.instance.Spec.NetworkId = networkID
	return b
}

// WithPublicIP enables public IP allocation for the instance.
func (b *InstancePayloadBuilder) WithPublicIP(enabled bool) *InstancePayloadBuilder {
	if b.instance.Spec.Networking == nil {
		b.instance.Spec.Networking = &openapi.InstanceNetworking{}
	}

	b.instance.Spec.Networking.PublicIP = ptr.To(enabled)

	return b
}

// WithSecurityGroups sets the security group IDs applied to the instance.
func (b *InstancePayloadBuilder) WithSecurityGroups(securityGroupIDs ...string) *InstancePayloadBuilder {
	if b.instance.Spec.Networking == nil {
		b.instance.Spec.Networking = &openapi.InstanceNetworking{}
	}

	b.instance.Spec.Networking.SecurityGroups = ptr.To(securityGroupIDs)

	return b
}

// WithSSHCertificateAuthorityID sets the SSH CA trust anchor for the instance.
func (b *InstancePayloadBuilder) WithSSHCertificateAuthorityID(id string) *InstancePayloadBuilder {
	b.instance.Spec.SshCertificateAuthorityId = ptr.To(id)

	return b
}

// Build returns the typed instance struct.
func (b *InstancePayloadBuilder) Build() openapi.InstanceCreate {
	return b.instance
}

// CreateInstanceWithCleanup creates an instance, waits for it to be provisioned, and schedules automatic cleanup.
func CreateInstanceWithCleanup(client *APIClient, ctx context.Context, config *TestConfig, payload openapi.InstanceCreate) (openapi.InstanceRead, string) {
	var instanceID string

	instanceName := payload.Metadata.Name

	// Schedule cleanup FIRST - ensures cleanup runs even if creation fails
	DeferCleanup(func() {
		if instanceID == "" {
			GinkgoWriter.Printf("No instance ID available for cleanup of %s\n", instanceName)
			return
		}

		GinkgoWriter.Printf("Cleaning up instance: %s\n", instanceID)
		Expect(client.DeleteInstance(ctx, instanceID)).To(Succeed())

		GinkgoWriter.Printf("Waiting for instance %s to be fully deleted\n", instanceID)
		Eventually(func() error {
			_, err := client.GetInstance(ctx, instanceID)
			return err
		}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(HaveOccurred())
	})

	instance, err := client.CreateInstance(ctx, payload)
	if err != nil {
		Fail(fmt.Sprintf("Failed to create instance: %v", err))
	}

	instanceID = instance.Metadata.Id

	GinkgoWriter.Printf("Created instance with ID: %s\n", instanceID)

	// Wait for instance to be provisioned
	Eventually(func() string {
		updatedInstance, getErr := client.GetInstance(ctx, instanceID)
		if getErr != nil {
			return "error"
		}

		provisioningStatus := string(updatedInstance.Metadata.ProvisioningStatus)

		if provisioningStatus == "error" {
			Fail(fmt.Sprintf("Instance %s entered error state during provisioning", instanceID))
		}

		return provisioningStatus
	}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(Equal("provisioned"))

	return instance, instanceID
}

// WaitForInstanceActive waits for an instance to reach active/running power state.
func WaitForInstanceActive(client *APIClient, ctx context.Context, config *TestConfig, instanceID string) {
	Eventually(func() string {
		instance, err := client.GetInstance(ctx, instanceID)
		if err != nil {
			GinkgoWriter.Printf("Error getting instance: %v\n", err)
			return "error"
		}

		if instance.Status.PowerState == nil {
			GinkgoWriter.Printf("Instance %s power state is nil (waiting for initialization)\n", instanceID)
			return "unknown"
		}

		powerState := string(*instance.Status.PowerState)
		GinkgoWriter.Printf("Instance %s power state: %s (waiting for Running)\n", instanceID, powerState)

		return powerState
	}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(Equal("Running"))

	GinkgoWriter.Printf("Instance %s is running\n", instanceID)
}

func hasStringValue(value *string) bool {
	return value != nil && *value != ""
}

func publicIPRequested(instance openapi.InstanceRead) bool {
	return instance.Spec.Networking != nil &&
		instance.Spec.Networking.PublicIP != nil &&
		*instance.Spec.Networking.PublicIP
}

func readyNetworkIdentity(instance openapi.InstanceRead, instanceID string) (string, bool) {
	if !hasStringValue(instance.Status.PrivateIP) {
		GinkgoWriter.Printf("Instance %s has no private IP yet\n", instanceID)
		return "", false
	}

	if !hasStringValue(instance.Status.MacAddress) {
		GinkgoWriter.Printf("Instance %s has no MAC address yet\n", instanceID)
		return "", false
	}

	if publicIPRequested(instance) {
		if !hasStringValue(instance.Status.PublicIP) {
			GinkgoWriter.Printf("Instance %s has no public IP yet\n", instanceID)
			return "", false
		}

		return *instance.Status.PublicIP, true
	}

	return "", true
}

// WaitForInstanceNetworkIdentity waits for an instance to persist its network identity.
// privateIP and macAddress must always be present; publicIP is required only when requested.
func WaitForInstanceNetworkIdentity(client *APIClient, ctx context.Context, config *TestConfig, instanceID string) string {
	var publicIP string

	Eventually(func() string {
		instance, err := client.GetInstance(ctx, instanceID)
		if err != nil {
			GinkgoWriter.Printf("Error getting instance: %v\n", err)
			return ""
		}

		var ready bool
		publicIP, ready = readyNetworkIdentity(instance, instanceID)
		if !ready {
			return ""
		}

		GinkgoWriter.Printf(
			"Instance %s network identity ready: private IP=%s, MAC=%s, public IP=%s\n",
			instanceID,
			*instance.Status.PrivateIP,
			*instance.Status.MacAddress,
			publicIP,
		)

		return *instance.Status.PrivateIP
	}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).ShouldNot(BeEmpty())

	return publicIP
}

// WaitForTCPPort waits for a TCP port to become reachable.
func WaitForTCPPort(address string, timeout time.Duration) {
	Eventually(func() error {
		conn, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			return err
		}

		if closeErr := conn.Close(); closeErr != nil {
			return closeErr
		}

		return nil
	}).WithTimeout(timeout).WithPolling(5 * time.Second).Should(Succeed())
}

// CreateSSHOpenSecurityGroupWithCleanup creates an ingress TCP/22 security group and schedules cleanup.
func CreateSSHOpenSecurityGroupWithCleanup(regionClient *RegionAPIClient, ctx context.Context, config *TestConfig) string {
	request := regionopenapi.SecurityGroupV2Create{
		Metadata: coreapi.ResourceWriteMetadata{
			Name: fmt.Sprintf("ssh-test-sg-%s", uuid.NewString()[:8]),
		},
		Spec: regionopenapi.SecurityGroupV2CreateSpec{
			NetworkId: config.NetworkID,
			Rules: regionopenapi.SecurityGroupRuleV2List{
				{
					Direction: regionopenapi.NetworkDirectionIngress,
					Protocol:  regionopenapi.NetworkProtocolTcp,
					Port:      ptr.To(22),
				},
			},
		},
	}

	securityGroup, err := regionClient.CreateSecurityGroup(ctx, request)
	Expect(err).NotTo(HaveOccurred(), "Failed to create SSH security group")
	Expect(securityGroup.Metadata.Id).NotTo(BeEmpty())

	securityGroupID := securityGroup.Metadata.Id

	DeferCleanup(func() {
		GinkgoWriter.Printf("Cleaning up SSH security group: %s\n", securityGroupID)
		Expect(regionClient.DeleteSecurityGroup(ctx, securityGroupID)).To(Succeed())
	})

	return securityGroupID
}

// CreateSSHCertificateAuthorityWithCleanup creates an SSH CA and schedules cleanup.
func CreateSSHCertificateAuthorityWithCleanup(regionClient *RegionAPIClient, ctx context.Context, config *TestConfig, publicKey string) string {
	request := regionopenapi.SshCertificateAuthorityV2Create{
		Metadata: coreapi.ResourceWriteMetadata{
			Name: fmt.Sprintf("ssh-test-ca-%s", uuid.NewString()[:8]),
		},
		Spec: regionopenapi.SshCertificateAuthorityV2CreateSpec{
			OrganizationId: config.OrgID,
			ProjectId:      config.ProjectID,
			PublicKey:      publicKey,
		},
	}

	authority, err := regionClient.CreateSSHCertificateAuthority(ctx, request)
	Expect(err).NotTo(HaveOccurred(), "Failed to create SSH certificate authority")
	Expect(authority.Metadata.Id).NotTo(BeEmpty())

	authorityID := authority.Metadata.Id

	DeferCleanup(func() {
		GinkgoWriter.Printf("Cleaning up SSH certificate authority: %s\n", authorityID)
		Expect(regionClient.DeleteSSHCertificateAuthority(ctx, authorityID)).To(Succeed())
	})

	return authorityID
}

// ImagePayloadBuilder builds ImageCreate payloads for testing.
type ImagePayloadBuilder struct {
	image regionopenapi.ImageCreate
}

// NewImagePayload creates a builder with sensible defaults.
func NewImagePayload() *ImagePayloadBuilder {
	timestamp := time.Now().Format("20060102-150405")

	return &ImagePayloadBuilder{
		image: regionopenapi.ImageCreate{
			Metadata: coreapi.ResourceWriteMetadata{
				Name: fmt.Sprintf("ginkgo-test-image-%s", timestamp),
			},
			Spec: regionopenapi.ImageCreateSpec{
				Architecture:   regionopenapi.ArchitectureX8664,
				Uri:            "https://s3.glo1.nscale.com/qa-test-automation-bucket/images/cirros-0.6.3-x86_64-disk.raw",
				Virtualization: regionopenapi.ImageVirtualizationVirtualized,
				Os: regionopenapi.ImageOS{
					Codename: ptr.To("cirros"),
					Distro:   regionopenapi.OsDistroUbuntu,
					Family:   regionopenapi.OsFamilyDebian,
					Kernel:   regionopenapi.OsKernelLinux,
					Version:  "0.6.3",
				},
			},
		},
	}
}

// WithName overrides the image name.
func (b *ImagePayloadBuilder) WithName(name string) *ImagePayloadBuilder {
	b.image.Metadata.Name = name
	return b
}

// Build returns the typed ImageCreate struct.
func (b *ImagePayloadBuilder) Build() regionopenapi.ImageCreate {
	return b.image
}

// WaitForImageReady polls until a custom image reaches the ready state in the region.
// Uses a 1-hour timeout to accommodate image download and import times.
func WaitForImageReady(regionClient *RegionAPIClient, ctx context.Context, config *TestConfig, imageID string) {
	GinkgoWriter.Printf("Waiting for image %s to be ready\n", imageID)

	Eventually(func() bool {
		images, err := regionClient.ListImages(ctx, config.OrgID, config.RegionID)
		Expect(err).NotTo(HaveOccurred())

		for _, image := range images {
			if image.Metadata.Id == imageID {
				GinkgoWriter.Printf("Image %s state: %s\n", imageID, image.Status.State)
				return image.Status.State == regionopenapi.ImageStateReady
			}
		}

		GinkgoWriter.Printf("Image %s not yet visible in list\n", imageID)

		return false
	}).WithTimeout(time.Hour).WithPolling(15 * time.Second).Should(BeTrue())
}
