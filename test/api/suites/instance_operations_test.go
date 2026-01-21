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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var _ = Describe("Instance Operations", func() {
	Context("When retrieving console output for an instance", func() {
		Describe("Given a valid instance exists", func() {
			var instanceID string

			BeforeEach(func() {
				// Create an instance for console output tests
				_, iID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().Build())

				instanceID = iID

				// Wait for instance to be running so console output is available
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
				invalidInstanceID := "non-existent-instance-12345"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, invalidInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance (expected HTTP 404)")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for non-existent instance")
				Expect(err.Error()).To(ContainSubstring("404"), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})

			It("should return error for malformed instance ID with uppercase", func() {
				// Invalid Kubernetes name: contains uppercase (violates spec requirement for lowercase)
				malformedInstanceID := "INVALID-UPPERCASE"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID (expected HTTP 400)")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")

				GinkgoWriter.Printf("Expected error for malformed instance ID (uppercase): %v\n", err)
			})

			It("should return error for malformed instance ID starting with hyphen", func() {
				// Invalid Kubernetes name: starts with hyphen (violates spec)
				malformedInstanceID := "-invalid-start"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID (expected HTTP 400)")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")

				GinkgoWriter.Printf("Expected error for malformed instance ID (starts with hyphen): %v\n", err)
			})

			It("should return error for malformed instance ID ending with hyphen", func() {
				// Invalid Kubernetes name: ends with hyphen (violates spec)
				malformedInstanceID := "invalid-end-"
				consoleOutput, err := client.GetInstanceConsoleOutput(ctx, malformedInstanceID, nil)

				Expect(err).To(HaveOccurred(), "Should return error for malformed instance ID (expected HTTP 400)")
				Expect(consoleOutput).To(BeNil(), "Console output should be nil for malformed instance ID")

				GinkgoWriter.Printf("Expected error for malformed instance ID (ends with hyphen): %v\n", err)
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

				// Wait for instance to be running so it can be snapshotted
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				GinkgoWriter.Printf("Using instance %s for snapshot tests\n", instanceID)
			})

			It("should successfully request a snapshot for instance", func() {
				image, err := client.SnapshotInstance(ctx, instanceID, "snapshot-for-test")

				DeferCleanup(func() {
					if image != nil {
						GinkgoWriter.Printf("Attempting to delete snapshot image %s\n", image.Metadata.Id)
						regionClient, err := api.NewRegionClient("") // let it get the base URL from config
						if err != nil {
							GinkgoWriter.Printf("Warning: Failed to create region client, to delete image %s: %v\n", image.Metadata.Id, err)
						}

						if err = regionClient.DeleteImage(ctx, config.OrgID, config.RegionID, image.Metadata.Id); err != nil {
							GinkgoWriter.Printf("Warning: Failed to delete image %s: %v\n", image.Metadata.Id, err)
						}
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
				invalidInstanceID := "non-existent-instance-12345"
				image, err := client.SnapshotInstance(ctx, invalidInstanceID, "snapshot-image")

				Expect(err).To(HaveOccurred(), "Should return error for non-existent instance (expected HTTP 404)")
				Expect(image).To(BeNil(), "Image response should be nil for non-existent instance")
				Expect(err.Error()).To(ContainSubstring("404"), "Error should indicate HTTP 404 Not Found")
				GinkgoWriter.Printf("Expected HTTP 404 error for non-existent instance: %v\n", err)
			})
		})
	})
})
