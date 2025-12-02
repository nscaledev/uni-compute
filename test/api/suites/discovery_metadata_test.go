/*
Copyright 2024-2025 the Unikorn Authors.

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
)

var _ = Describe("Discovery and Metadata", func() {
	Context("When querying available regions", func() {
		Describe("Given valid organization access", func() {
			It("should return all available regions", func() {
				regions, err := client.ListRegions(ctx, config.OrgID)
				Expect(err).NotTo(HaveOccurred())
				Expect(regions).NotTo(BeEmpty())
				for _, region := range regions {
					Expect(region.Metadata).NotTo(BeNil())
					Expect(region.Spec).NotTo(BeNil())
					Expect(region.Metadata.Id).NotTo(BeEmpty())
					Expect(region.Metadata.Name).NotTo(BeEmpty())
				}
				GinkgoWriter.Printf("Found %d regions\n", len(regions))
			})
		})

		Describe("Given invalid parameters", func() {
			It("should reject requests with invalid organization IDs", func() {
				regions, err := client.ListRegions(ctx, "ABC123")
				Expect(err).To(HaveOccurred())
				Expect(regions).To(BeEmpty())
			})
		})
	})

	Context("When listing flavors and images", func() {
		Describe("Given valid region and organization", func() {
			It("should return all available flavors for the region", func() {
				flavors, err := client.ListFlavors(ctx, config.OrgID, config.RegionID)
				Expect(err).NotTo(HaveOccurred())
				Expect(flavors).NotTo(BeEmpty())
				for _, flavor := range flavors {
					Expect(flavor.Metadata).NotTo(BeNil())
					Expect(flavor.Spec).NotTo(BeNil())
					Expect(flavor.Metadata.Id).NotTo(BeEmpty())
					Expect(flavor.Metadata.Name).NotTo(BeEmpty())
				}
				GinkgoWriter.Printf("Found %d flavors\n", len(flavors))
			})

			It("should return all available images for the region", func() {
				images, err := client.ListImages(ctx, config.OrgID, config.RegionID)
				Expect(err).NotTo(HaveOccurred())
				Expect(images).NotTo(BeEmpty())
				for _, image := range images {
					Expect(image.Metadata).NotTo(BeNil())
					Expect(image.Spec).NotTo(BeNil())
					Expect(image.Metadata.Id).NotTo(BeEmpty())
					Expect(image.Metadata.Name).NotTo(BeEmpty())
					Expect(image.Spec.Os).NotTo(BeNil())
					Expect(image.Spec.Os.Distro).NotTo(BeEmpty())
					Expect(image.Spec.Os.Version).NotTo(BeEmpty())
				}
				GinkgoWriter.Printf("Found %d images\n", len(images))
			})

			It("should handle regions with no available flavors", func() {
				flavors, err := client.ListFlavors(ctx, config.OrgID, "123456789")
				Expect(flavors).To(BeEmpty())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("server error"))
				Expect(err.Error()).To(ContainSubstring("status: 500"))
			})

			It("should handle regions with no available images", func() {
				images, err := client.ListImages(ctx, config.OrgID, "123456789")
				Expect(images).To(BeEmpty())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("server error"))
				Expect(err.Error()).To(ContainSubstring("status: 500"))
				GinkgoWriter.Printf("Expected error for invalid region: %v\n", err)
			})
		})
	})
})
