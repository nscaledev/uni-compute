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
					Expect(region).To(HaveKey("metadata"))
					Expect(region).To(HaveKey("spec"))
					metadata := region["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
					Expect(metadata).To(HaveKey("id"))
					Expect(metadata).To(HaveKey("name"))
					Expect(metadata["id"]).NotTo(BeEmpty())
					Expect(metadata["name"]).NotTo(BeEmpty())
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
					Expect(flavor).To(HaveKey("metadata"))
					Expect(flavor).To(HaveKey("spec"))
					metadata := flavor["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
					Expect(metadata).To(HaveKey("id"))
					Expect(metadata).To(HaveKey("name"))
					Expect(metadata["id"]).NotTo(BeEmpty())
					Expect(metadata["name"]).NotTo(BeEmpty())
				}
				GinkgoWriter.Printf("Found %d flavors\n", len(flavors))
			})

			It("should return all available images for the region", func() {
				images, err := client.ListImages(ctx, config.OrgID, config.RegionID)
				Expect(err).NotTo(HaveOccurred())
				Expect(images).NotTo(BeEmpty())
				for _, image := range images {
					Expect(image).To(HaveKey("metadata"))
					Expect(image).To(HaveKey("spec"))
					metadata := image["metadata"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
					spec := image["spec"].(map[string]interface{})         //nolint:forcetypeassert // safe: API response structure
					Expect(metadata).To(HaveKey("id"))
					Expect(metadata).To(HaveKey("name"))
					Expect(metadata["id"]).NotTo(BeEmpty())
					Expect(metadata["name"]).NotTo(BeEmpty())
					Expect(spec).To(HaveKey("os"))
					os := spec["os"].(map[string]interface{}) //nolint:forcetypeassert // safe: API response structure
					Expect(os).To(HaveKey("distro"))
					Expect(os).To(HaveKey("version"))
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
