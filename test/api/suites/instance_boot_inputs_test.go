//go:build integration

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

//nolint:testpackage,revive // test package in suites is standard for these tests
package suites

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreapi "github.com/unikorn-cloud/core/pkg/openapi"
	idstest "github.com/unikorn-cloud/region/pkg/ids/idstest"
	regionopenapi "github.com/unikorn-cloud/region/pkg/openapi"

	"github.com/unikorn-cloud/compute/pkg/openapi"
	"github.com/unikorn-cloud/compute/test/api"

	"k8s.io/utils/ptr"
)

const (
	// kernelBanner starts each boot's output on the serial console.
	kernelBanner = "Linux version"
	// cloudInitFinished is printed to the console once cloud-init has run.
	cloudInitFinished = "finished at"
)

// markerUserData returns a script that prints "boot-inputs <name>" to the
// serial console. The quoting keeps that text out of the script itself.
func markerUserData(name string) ([]byte, string) {
	return []byte(fmt.Sprintf("#!/bin/sh\necho \"boot-inputs\" \"%s\" > /dev/console\n", name)), "boot-inputs " + name
}

// updateFromRead copies an instance read into an update, as a client that
// round-trips the spec would.
func updateFromRead(instance openapi.InstanceRead) openapi.InstanceUpdate {
	return openapi.InstanceUpdate{
		Metadata: coreapi.ResourceWriteMetadata{
			Name:        instance.Metadata.Name,
			Description: instance.Metadata.Description,
		},
		Spec: instance.Spec,
	}
}

// eventuallyLatestBoot waits for the console output of the instance's most
// recent boot to satisfy match.
func eventuallyLatestBoot(instanceID string, match func(Gomega, string)) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		output, err := client.GetInstanceConsoleOutput(ctx, instanceID, nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(output.Contents).To(ContainSubstring(kernelBanner), "the console must show the kernel's boot messages")

		match(g, output.Contents[strings.LastIndex(output.Contents, kernelBanner):])
	}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(Succeed())
}

// eventuallyRebuilt waits for the instance to report a rebuild, then to run
// again. A rebuild takes minutes, well beyond the polling interval.
func eventuallyRebuilt(instanceID string) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		instance, err := client.GetInstance(ctx, instanceID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(instance.Status.PowerState).To(HaveValue(Equal(regionopenapi.InstanceLifecyclePhaseRebuilding)))
	}).WithTimeout(config.TestTimeout).WithPolling(5 * time.Second).Should(Succeed())

	api.WaitForInstanceActive(client, ctx, config, instanceID)
}

var _ = Describe("Instance boot inputs", func() {
	Context("When an update omits user data", func() {
		Describe("Given a provisioned instance with user data", func() {
			It("keeps the stored user data", func() {
				original, _ := markerUserData("original")

				instance, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().WithUserData(original).Build())

				update := updateFromRead(instance)
				update.Metadata.Description = ptr.To("updated without user data")
				update.Spec.UserData = nil

				updated, err := client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Spec.UserData).To(HaveValue(Equal(original)))

				retrieved, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())
				Expect(retrieved.Spec.UserData).To(HaveValue(Equal(original)),
					"an update that omits user data must keep the stored value")
			})
		})
	})

	Context("When user data changes and the image is then changed", func() {
		Describe("Given a running instance with user data", func() {
			// The server only boots with its user data at create or rebuild, so
			// each image change shows which user data the server holds.
			It("rebuilds in place, booting with the replaced or cleared user data", func() {
				if config.RebuildImageID == "" {
					Skip("image change tests require TEST_REBUILD_IMAGE_ID")
				}

				original, originalMarker := markerUserData("original")
				replacement, replacementMarker := markerUserData("replacement")

				_, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
					api.NewInstancePayload().WithUserData(original).Build())

				api.WaitForInstanceNetworkIdentity(client, ctx, config, instanceID)
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				By("seeing the original user data run at first boot")
				eventuallyLatestBoot(instanceID, func(g Gomega, boot string) {
					g.Expect(boot).To(ContainSubstring(originalMarker))
				})

				instance, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				By("replacing the user data")
				update := updateFromRead(instance)
				update.Spec.UserData = &replacement

				_, err = client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				By("changing the image, and booting the same server with the replacement")
				instance, err = client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				update = updateFromRead(instance)
				update.Spec.ImageId = idstest.MustParseImageID(config.RebuildImageID)

				_, err = client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())

				eventuallyRebuilt(instanceID)
				eventuallyLatestBoot(instanceID, func(g Gomega, boot string) {
					g.Expect(boot).To(ContainSubstring(replacementMarker))
				})

				rebuilt, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rebuilt.Status.MacAddress).To(Equal(instance.Status.MacAddress), "a rebuild keeps the server's port")
				Expect(rebuilt.Status.PrivateIP).To(Equal(instance.Status.PrivateIP), "a rebuild keeps the server's address")

				By("clearing the user data")
				update = updateFromRead(rebuilt)
				update.Spec.UserData = &[]byte{}

				updated, err := client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Spec.UserData).To(Or(BeNil(), HaveValue(BeEmpty())))
				api.WaitForInstanceActive(client, ctx, config, instanceID)

				By("changing the image back, and booting with no user data")
				instance, err = client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				update = updateFromRead(instance)
				update.Spec.ImageId = idstest.MustParseImageID(config.ImageID)

				_, err = client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())

				eventuallyRebuilt(instanceID)
				// Every earlier boot printed a marker, so a finished boot
				// without one is the rebuilt server.
				eventuallyLatestBoot(instanceID, func(g Gomega, boot string) {
					g.Expect(boot).To(ContainSubstring(cloudInitFinished))
					g.Expect(boot).NotTo(ContainSubstring(replacementMarker))
					g.Expect(boot).NotTo(ContainSubstring(originalMarker))
				})
			})
		})
	})
})
