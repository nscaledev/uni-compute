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
	// loginPrompt is printed by the serial console's getty once a boot has
	// brought the system up.
	loginPrompt = "login:"
	// userDataSettle is how long a boot is given after its login prompt to run
	// user data, before a missing marker counts: cloud-init's last stage can
	// run after the prompt appears.
	userDataSettle = 2 * time.Minute
)

// markerUserData returns a script that prints "boot-inputs <name>" to the
// first serial port, which the console output records whichever console the
// image makes /dev/console. The quoting keeps that text out of the script.
func markerUserData(name string) ([]byte, string) {
	return []byte(fmt.Sprintf("#!/bin/sh\necho \"boot-inputs\" \"%s\" > /dev/ttyS0\n", name)), "boot-inputs " + name
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

// latestBoot returns the console output of the instance's most recent boot,
// which starts at the last kernel banner.
func latestBoot(g Gomega, instanceID string) string {
	output, err := client.GetInstanceConsoleOutput(ctx, instanceID, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(output.Contents).To(ContainSubstring(kernelBanner), "the console must keep the latest boot's kernel banner")

	return output.Contents[strings.LastIndex(output.Contents, kernelBanner):]
}

// eventuallyLatestBootShows waits for the instance's most recent boot to
// print marker.
func eventuallyLatestBootShows(instanceID, marker string) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		g.Expect(latestBoot(g, instanceID)).To(ContainSubstring(marker))
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

	Context("When user data and the image change together", func() {
		Describe("Given a running instance with user data", func() {
			// Each update changes both, so it causes one rebuild whether or not
			// Region also rebuilds for a user-data change alone.
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
				eventuallyLatestBootShows(instanceID, originalMarker)

				instance, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				By("replacing the user data and changing the image")
				update := updateFromRead(instance)
				update.Spec.UserData = &replacement
				update.Spec.ImageId = idstest.MustParseImageID(config.RebuildImageID)

				_, err = client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())

				eventuallyRebuilt(instanceID)
				eventuallyLatestBootShows(instanceID, replacementMarker)

				rebuilt, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())
				Expect(rebuilt.Status.MacAddress).To(Equal(instance.Status.MacAddress), "a rebuild keeps the server's port")
				Expect(rebuilt.Status.PrivateIP).To(Equal(instance.Status.PrivateIP), "a rebuild keeps the server's address")

				By("clearing the user data and changing the image back")
				update = updateFromRead(rebuilt)
				update.Spec.UserData = &[]byte{}
				update.Spec.ImageId = idstest.MustParseImageID(config.ImageID)

				updated, err := client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())
				Expect(updated.Spec.UserData).To(Or(BeNil(), HaveValue(BeEmpty())))

				eventuallyRebuilt(instanceID)

				// Every earlier boot printed a marker, so a boot that reaches
				// its login prompt without one is the rebuilt server.
				noMarker := Not(Or(ContainSubstring(originalMarker), ContainSubstring(replacementMarker)))

				Eventually(func(g Gomega) {
					g.Expect(latestBoot(g, instanceID)).To(And(ContainSubstring(loginPrompt), noMarker))
				}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(Succeed())

				Consistently(func(g Gomega) {
					g.Expect(latestBoot(g, instanceID)).To(noMarker)
				}).WithTimeout(userDataSettle).WithPolling(10 * time.Second).Should(Succeed())
			})
		})
	})
})
