//go:build integration

/*
Copyright 2025 the Unikorn Authors.
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

//nolint:testpackage,revive // test package in suites is standard for these tests
package suites

import (
	"encoding/json"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	coreclient "github.com/unikorn-cloud/core/pkg/testing/client"
	regionopenapi "github.com/unikorn-cloud/region/pkg/openapi"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/test/api"
)

// nonExistentInstanceID is a syntactically valid UUID that does not correspond
// to any instance. Instance IDs are validated as UUIDs at the OpenAPI schema
// layer, so a non-UUID value would be rejected with 400 before reaching the
// handler; using a valid UUID exercises the genuine not-found (404) path.
const nonExistentInstanceID = "00000000-0000-0000-0000-000000000000"

var _ = Describe("Instance Operations", func() {
	Context("When listing instances", func() {
		Describe("Given a provisioned instance", func() {
			var instanceID string

			BeforeEach(func() {
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())
				instanceID = iID
				GinkgoWriter.Printf("Using instance %s for list test\n", instanceID)
			})

			It("should return the provisioned instance in the list", func() {
				instances, err := client.ListInstances(ctx, config.OrgID, config.ProjectID)
				Expect(err).NotTo(HaveOccurred())
				Expect(instances).NotTo(BeEmpty())

				instanceIDs := make([]string, len(instances))
				for i, inst := range instances {
					instanceIDs[i] = inst.Metadata.Id
				}
				Expect(instanceIDs).To(ContainElement(instanceID),
					"provisioned instance %s should appear in list", instanceID)

				for _, inst := range instances {
					if inst.Metadata.Id == instanceID {
						Expect(inst.Metadata.ProvisioningStatus).To(Equal(coreapi.ResourceProvisioningStatusProvisioned))
						break
					}
				}

				GinkgoWriter.Printf("Found instance %s in list with status provisioned\n", instanceID)
			})
		})

		Describe("Given an audit token", func() {
			It("should list instances as read-only", func() {
				if config.AuditToken == "" {
					Skip("AUDIT_AUTH_TOKEN or SERVICE_TOKEN_PRIVATE_AUDIT must be set by integration fixtures")
				}

				auditConfig := *config
				auditConfig.AuthToken = config.AuditToken
				auditClient := api.NewAPIClientWithConfig(&auditConfig)

				instances, err := auditClient.ListInstances(ctx, config.OrgID, config.ProjectID)
				Expect(err).NotTo(HaveOccurred())
				Expect(instances).NotTo(BeEmpty())
			})
		})

		Describe("Given a public-admin token for a private organization", func() {
			It("should not expose private organization instances", func() {
				if config.PublicAdminToken == "" {
					Skip("PUBLIC_ADMIN_AUTH_TOKEN or SERVICE_TOKEN_PRIVATE_PUBLIC_ADMIN must be set by integration fixtures")
				}

				publicAdminConfig := *config
				publicAdminConfig.AuthToken = config.PublicAdminToken
				publicAdminClient := api.NewAPIClientWithConfig(&publicAdminConfig)

				path := api.NewEndpoints().ListInstances(config.OrgID, config.ProjectID)
				resp, respBody, err := publicAdminClient.DoRequest(ctx, http.MethodGet, path, nil, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.StatusCode).To(BeElementOf(http.StatusOK, http.StatusForbidden))

				if resp.StatusCode == http.StatusOK {
					var instances []openapi.InstanceRead
					Expect(json.Unmarshal(respBody, &instances)).To(Succeed())
					Expect(instances).To(BeEmpty())
				}
			})
		})
	})

	Context("When updating an instance", func() {
		Describe("Given a provisioned instance", func() {
			var instance openapi.InstanceRead

			BeforeEach(func() {
				var iID string
				instance, iID = api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())
				GinkgoWriter.Printf("Using instance %s for update test\n", iID)
			})

			It("should update the instance description and persist it", func() {
				updatedDescription := "updated description for test"

				updateReq := openapi.InstanceUpdate{
					Metadata: coreapi.ResourceWriteMetadata{
						Name:        instance.Metadata.Name,
						Description: &updatedDescription,
					},
					Spec: instance.Spec,
				}

				updated, err := client.UpdateInstance(ctx, instance.Metadata.Id, updateReq)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Metadata.Description).NotTo(BeNil())
				Expect(*updated.Metadata.Description).To(Equal(updatedDescription))

				retrieved, err := client.GetInstance(ctx, instance.Metadata.Id)
				Expect(err).NotTo(HaveOccurred())
				Expect(retrieved.Metadata.Description).NotTo(BeNil())
				Expect(*retrieved.Metadata.Description).To(Equal(updatedDescription),
					"updated description should persist in subsequent GET")

				GinkgoWriter.Printf("Instance %s description updated and verified via GET\n", instance.Metadata.Id)
			})

			It("should reject a rename with 422 Unprocessable Content", func() {
				updateReq := openapi.InstanceUpdate{
					Metadata: coreapi.ResourceWriteMetadata{
						Name: instance.Metadata.Name + "-renamed",
					},
					Spec: instance.Spec,
				}

				status, err := client.UpdateInstanceRawStatus(ctx, instance.Metadata.Id, updateReq)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusUnprocessableEntity),
					"rename attempt must return 422 Unprocessable Content")

				GinkgoWriter.Printf("Instance %s rename correctly rejected with 422\n", instance.Metadata.Id)
			})
		})
	})

	Context("When deleting an instance", func() {
		Describe("Given a provisioned instance", func() {
			var instanceID string

			BeforeEach(func() {
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())
				instanceID = iID
				GinkgoWriter.Printf("Using instance %s for delete lifecycle test\n", instanceID)
			})

			It("should successfully delete and return not found on subsequent get", func() {
				status, err := client.DeleteInstanceWithStatus(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusAccepted),
					"DELETE should return 202 Accepted, got %d", status)

				Eventually(func() error {
					_, err := client.GetInstance(ctx, instanceID)
					return err
				}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(MatchError(coreclient.ErrResourceNotFound))

				GinkgoWriter.Printf("Instance %s confirmed deleted\n", instanceID)
			})
		})
	})

	Context("When creating an instance", func() {
		Context("from a custom image", func() {
			var (
				regionClient  *api.RegionAPIClient
				customImageID string
			)

			BeforeEach(func() {
				var err error

				regionClient, err = api.NewRegionClient("")
				Expect(err).NotTo(HaveOccurred(), "Failed to create region client")

				image, err := regionClient.CreateImage(ctx, config.OrgID, config.RegionID,
					api.NewImagePayload().WithSoftwareVersions(map[string]string{
						"kubernetes": "v1.33.0",
					}).Build())
				Expect(err).NotTo(HaveOccurred(), "Failed to create custom image")
				Expect(image.Metadata.Id).NotTo(BeEmpty())

				customImageID = image.Metadata.Id
				GinkgoWriter.Printf("Created custom image: %s\n", customImageID)

				DeferCleanup(func() {
					GinkgoWriter.Printf("Cleaning up custom image: %s\n", customImageID)
					Expect(regionClient.DeleteImage(ctx, config.OrgID, config.RegionID, customImageID)).
						To(Succeed(), "Failed to delete custom image %s", customImageID)
				})

				api.WaitForImageReady(regionClient, ctx, config, customImageID)

				images, err := client.ListImages(ctx, config.OrgID, config.RegionID)
				Expect(err).NotTo(HaveOccurred(), "Failed to list Compute catalog images")
				Expect(images).NotTo(ContainElement(WithTransform(func(image regionopenapi.Image) string {
					return image.Metadata.Id
				}, Equal(customImageID))), "software-versioned image must remain absent from the Compute catalog")
			})

			It("should launch and update an instance successfully", func() {
				_, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().WithImageID(customImageID).Build())

				GinkgoWriter.Printf("Launched instance %s from custom image %s\n", instanceID, customImageID)

				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				instance, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				updatedDescription := "updated without changing the region-only image"
				updated, err := client.UpdateInstance(ctx, instanceID, openapi.InstanceUpdate{
					Metadata: coreapi.ResourceWriteMetadata{
						Name:        instance.Metadata.Name,
						Description: &updatedDescription,
					},
					Spec: instance.Spec,
				})
				Expect(err).NotTo(HaveOccurred(), "Failed to update instance with unchanged region-only image")
				Expect(updated.Spec.ImageId.String()).To(Equal(customImageID))
			})
		})

		Context("from a snapshot image", func() {
			var (
				regionClient    *api.RegionAPIClient
				snapshotImageID string
			)

			BeforeEach(func() {
				var err error

				_, sourceInstanceID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				api.WaitForInstanceNetworkIdentity(client, ctx, config, sourceInstanceID)
				api.WaitForInstanceActive(client, ctx, config, sourceInstanceID)

				GinkgoWriter.Printf("Taking snapshot of instance %s\n", sourceInstanceID)

				image, err := client.SnapshotInstance(ctx, sourceInstanceID, "snapshot-for-launch-test")
				Expect(err).NotTo(HaveOccurred(), "Failed to take snapshot")
				Expect(image).NotTo(BeNil(), "Snapshot image should not be nil")

				snapshotImageID = image.Metadata.Id

				regionClient, err = api.NewRegionClient("")
				Expect(err).NotTo(HaveOccurred(), "Failed to create region client")

				DeferCleanup(func() {
					GinkgoWriter.Printf("Cleaning up snapshot image: %s\n", snapshotImageID)
					Expect(regionClient.DeleteImage(ctx, config.OrgID, config.RegionID, snapshotImageID)).
						To(Succeed(), "Failed to delete snapshot image %s", snapshotImageID)
				})

				api.WaitForImageReady(regionClient, ctx, config, snapshotImageID)
			})

			It("should launch an instance successfully", func() {
				_, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().WithImageID(snapshotImageID).Build())

				GinkgoWriter.Printf("Launched instance %s from snapshot image %s\n", instanceID, snapshotImageID)

				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)
			})
		})

		Describe("Given an invalid payload", func() {
			It("should return bad request for missing required fields", func() {
				_, err := client.CreateInstance(ctx, openapi.InstanceCreate{})

				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(ContainSubstring("400")), "Error should indicate HTTP 400 Bad Request")
				Expect(err).To(MatchError(ContainSubstring("invalid_request")), "Error should indicate schema validation failure")
			})
		})

		Describe("Given an instance already exists with the same name on the same network", func() {
			It("should reject a duplicate create with 409 Conflict", func() {
				payload := api.NewInstancePayload().Build()

				first, err := client.CreateInstance(ctx, payload)
				Expect(err).NotTo(HaveOccurred())

				DeferCleanup(func() {
					GinkgoWriter.Printf("Cleaning up first instance %s\n", first.Metadata.Id)
					Expect(client.DeleteInstance(ctx, first.Metadata.Id)).To(Succeed())
				})

				GinkgoWriter.Printf("Created first instance %s with name %q; attempting duplicate create\n",
					first.Metadata.Id, payload.Metadata.Name)

				status, err := client.CreateInstanceRawStatus(ctx, payload)
				Expect(err).NotTo(HaveOccurred())
				Expect(status).To(Equal(http.StatusConflict),
					"duplicate create on same network must return 409 Conflict")

				GinkgoWriter.Printf("Duplicate create correctly rejected with 409\n")
			})
		})

	})

	Context("When retrieving console output for an instance", func() {
		Describe("Given a valid instance exists", Ordered, func() {
			var instanceID string

			BeforeAll(func() {
				// Create a single instance shared across all console output specs.
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				instanceID = iID

				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				GinkgoWriter.Printf("Using instance %s for console output tests\n", instanceID)
			})

			It("should successfully get console output for instance", func() {
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, instanceID, nil)
				Expect(err).NotTo(HaveOccurred(), "Should successfully retrieve console output (HTTP 200)")
				Expect(consoleOutput).NotTo(BeNil(), "Console output should not be nil")
				Expect(consoleOutput.Contents).NotTo(BeNil(), "Console output should have Contents field")
				GinkgoWriter.Printf("Successfully retrieved console output for instance %s (contents length: %d)\n",
					instanceID, len(consoleOutput.Contents))
			})

			It("should successfully get console output with length parameter", func() {
				length := 100
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, instanceID, &length)
				Expect(err).NotTo(HaveOccurred(), "Should successfully retrieve console output with length parameter (HTTP 200)")
				Expect(consoleOutput).NotTo(BeNil(), "Console output should not be nil")

				// Verify response structure
				Expect(consoleOutput.Contents).NotTo(BeNil(), "Console output should have Contents field")
				GinkgoWriter.Printf("Successfully retrieved console output with length=%d for instance %s (contents length: %d)\n",
					length, instanceID, len(consoleOutput.Contents))
			})

			It("should handle different length values correctly", func() {
				testCases := []int{50, 100, 500, 1000}

				for _, length := range testCases {
					consoleOutput, err := client.GetInstanceConsoleOutput(ctx, instanceID, &length)
					Expect(err).NotTo(HaveOccurred(), "Should successfully retrieve console output with length=%d (HTTP 200)", length)
					Expect(consoleOutput).NotTo(BeNil(), "Console output should not be nil for length=%d", length)

					// Verify response structure
					Expect(consoleOutput.Contents).NotTo(BeNil(), "Console output should have Contents field")
					GinkgoWriter.Printf("Console output retrieved with length=%d (contents length: %d)\n",
						length, len(consoleOutput.Contents))
				}
			})
		})

		Describe("Given an invalid instance ID", func() {
			It("should return appropriate error for non-existent instance", func() {
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, nonExistentInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for non-existent instance")
				Expect(err).To(MatchError(ContainSubstring("404")), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})

			It("should return bad request for malformed instance ID with uppercase", func() {
				// Not a valid UUID: contains uppercase and is not hyphen-delimited hex.
				malformedInstanceID := "INVALID-UPPERCASE"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")
				Expect(err).To(MatchError(ContainSubstring("400")), "Error should indicate HTTP 400 Bad Request")

				GinkgoWriter.Printf("Expected HTTP 400 error for malformed instance ID (uppercase): %v\n", err)
			})

			It("should return bad request for malformed instance ID starting with hyphen", func() {
				// Not a valid UUID: leading hyphen.
				malformedInstanceID := "-invalid-start"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")
				Expect(err).To(MatchError(ContainSubstring("400")), "Error should indicate HTTP 400 Bad Request")

				GinkgoWriter.Printf("Expected HTTP 400 error for malformed instance ID (starts with hyphen): %v\n", err)
			})

			It("should return bad request for malformed instance ID ending with hyphen", func() {
				// Not a valid UUID: trailing hyphen.
				malformedInstanceID := "invalid-end-"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")
				Expect(err).To(MatchError(ContainSubstring("400")), "Error should indicate HTTP 400 Bad Request")

				GinkgoWriter.Printf("Expected HTTP 400 error for malformed instance ID (ends with hyphen): %v\n", err)
			})
		})
	})

	Context("When requesting a snapshot for an instance", func() {
		Describe("Given a valid instance exists", func() {
			var (
				instanceID string
			)

			BeforeEach(func() {
				// Create an instance to snapshot
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				instanceID = iID

				// Wait for network identity and running state so it can be snapshotted
				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				GinkgoWriter.Printf("Using instance %s for snapshot tests\n", instanceID)
			})

			It("should successfully request a snapshot for instance", func() {
				image, err := client.SnapshotInstance(ctx, instanceID, "snapshot-for-test")

				DeferCleanup(func() {
					if image != nil {
						GinkgoWriter.Printf("Cleaning up snapshot image %s\n", image.Metadata.Id)
						regionClient, err := api.NewRegionClient("")
						Expect(err).NotTo(HaveOccurred(), "Failed to create region client for snapshot image cleanup")
						Expect(regionClient.DeleteImage(ctx, config.OrgID, config.RegionID, image.Metadata.Id)).
							To(Succeed(), "Failed to delete snapshot image %s", image.Metadata.Id)
					}
				})

				Expect(err).NotTo(HaveOccurred(), "Should successfully request the snapshot (HTTP 201)")
				Expect(image).NotTo(BeNil(), "Image record in response should not be nil")
				Expect(image.Metadata.Name).To(Equal("snapshot-for-test"), "snapshot image should have name as given")

				GinkgoWriter.Printf("Successfully created snapshot image for instance %s (image ID: %s)\n",
					instanceID, image.Metadata.Id)
			})
		})

		Describe("Given an invalid instance ID", func() {
			It("should return appropriate error for non-existent instance", func() {
				image, err := client.SnapshotInstance(ctx, nonExistentInstanceID, "snapshot-image")

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(image).To(BeNil(), "Image response should be nil for non-existent instance")
				Expect(err).To(MatchError(ContainSubstring("404")), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})
		})
	})

	Context("When performing power operations on an instance", func() {
		Describe("Given a valid instance exists", func() {
			var instanceID string

			BeforeEach(func() {
				// Create an instance for power operation tests
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				instanceID = iID

				// Wait for network identity and running state before performing power operations
				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				GinkgoWriter.Printf("Using instance %s for power operations\n", instanceID)
			})

			It("should successfully stop a running instance", func() {
				GinkgoWriter.Printf("Stopping instance %s\n", instanceID)
				err := client.StopInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					instance, getErr := client.GetInstance(ctx, instanceID)
					if getErr != nil {
						GinkgoWriter.Printf("Error getting instance: %v\n", getErr)
						return "error"
					}

					if instance.Status.PowerState == nil {
						return "unknown"
					}

					status := string(*instance.Status.PowerState)
					GinkgoWriter.Printf("Instance %s power state: %s (waiting for Stopped)\n", instanceID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Stopped"))
			})

			It("should successfully start a stopped instance", func() {
				GinkgoWriter.Printf("Stopping instance %s\n", instanceID)
				err := client.StopInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					instance, getErr := client.GetInstance(ctx, instanceID)
					if getErr != nil {
						return "error"
					}

					if instance.Status.PowerState == nil {
						return "unknown"
					}

					status := string(*instance.Status.PowerState)
					GinkgoWriter.Printf("Instance %s power state: %s (waiting for Stopped)\n", instanceID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Stopped"))

				GinkgoWriter.Printf("Starting instance %s\n", instanceID)
				err = client.StartInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					instance, getErr := client.GetInstance(ctx, instanceID)
					if getErr != nil {
						return "error"
					}

					if instance.Status.PowerState == nil {
						return "unknown"
					}

					status := string(*instance.Status.PowerState)
					GinkgoWriter.Printf("Instance %s power state: %s (waiting for Running)\n", instanceID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})

			It("should successfully soft reboot a running instance", func() {
				GinkgoWriter.Printf("Soft rebooting instance %s\n", instanceID)
				err := client.RebootInstance(ctx, instanceID, false)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					instance, getErr := client.GetInstance(ctx, instanceID)
					if getErr != nil {
						return "error"
					}

					if instance.Status.PowerState == nil {
						return "unknown"
					}

					status := string(*instance.Status.PowerState)
					GinkgoWriter.Printf("Instance %s power state: %s (waiting for Running after soft reboot)\n", instanceID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})

			It("should successfully hard reboot a running instance", func() {
				GinkgoWriter.Printf("Hard rebooting instance %s\n", instanceID)
				err := client.RebootInstance(ctx, instanceID, true)
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() string {
					instance, getErr := client.GetInstance(ctx, instanceID)
					if getErr != nil {
						return "error"
					}

					if instance.Status.PowerState == nil {
						return "unknown"
					}

					status := string(*instance.Status.PowerState)
					GinkgoWriter.Printf("Instance %s power state: %s (waiting for Running after hard reboot)\n", instanceID, status)

					return status
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal("Running"))
			})
		})

		Describe("Given an invalid instance ID", func() {
			It("should return not found when stopping", func() {
				err := client.StopInstance(ctx, nonExistentInstanceID)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(err).To(MatchError(ContainSubstring("404")), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})

			It("should return not found when starting", func() {
				err := client.StartInstance(ctx, nonExistentInstanceID)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(err).To(MatchError(ContainSubstring("404")), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})

			It("should return not found when rebooting", func() {
				err := client.RebootInstance(ctx, nonExistentInstanceID, false)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(err).To(MatchError(ContainSubstring("404")), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})
		})
	})

	Context("When retrieving an instance", func() {
		Describe("Given a valid instance exists", func() {
			var instanceID string

			BeforeEach(func() {
				// Create an instance for retrieval tests
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				instanceID = iID
			})

			It("should return the instance with correct metadata", func() {
				instance, err := client.GetInstance(ctx, instanceID)

				Expect(err).NotTo(HaveOccurred())
				Expect(instance.Metadata.Id).To(Equal(instanceID))
				Expect(instance.Metadata.Name).NotTo(BeEmpty())
				GinkgoWriter.Printf("Successfully retrieved instance %s\n", instanceID)
			})
		})

		Describe("Given an invalid instance ID", func() {
			It("should return not found", func() {
				_, err := client.GetInstance(ctx, nonExistentInstanceID)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance")
				Expect(err).To(MatchError(coreclient.ErrResourceNotFound), "Error should indicate resource not found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})
		})
	})

})
