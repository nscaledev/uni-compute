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
	"regexp"
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

// cloudInitFinished matches the message cloud-init prints to the console at
// the end of each boot it runs, e.g. "Cloud-init v. 24.1 finished at Thu, 26
// Nov 2026 17:00:00 +0000. Datasource DataSourceOpenStack. Up 25.30 seconds".
// Its time and uptime make each boot's message distinct.
var cloudInitFinished = regexp.MustCompile(`Cloud-init v\. \S+ finished at [^\r\n]*`)

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

// lastBoot returns cloud-init's finish message for the last boot in the
// console output, and that boot's output. The console keeps only recent
// output, so a boot's kernel messages may be gone; the boot is taken to start
// after the previous finish message instead. A line can be echoed to more than
// one console, so a repeat of the last message is not the previous one.
func lastBoot(output string) (string, string) {
	matches := cloudInitFinished.FindAllStringIndex(output, -1)
	if len(matches) == 0 {
		return "", ""
	}

	last := matches[len(matches)-1]
	finished := output[last[0]:last[1]]
	start := 0

	for i := len(matches) - 2; i >= 0; i-- {
		if output[matches[i][0]:matches[i][1]] != finished {
			start = matches[i][1]
			break
		}
	}

	return finished, output[start:last[1]]
}

// eventuallyNextBoot waits for a boot to finish other than the one whose
// finish message is previous, and returns its finish message and output.
func eventuallyNextBoot(instanceID, previous string) (string, string) {
	GinkgoHelper()

	var finished, boot string

	Eventually(func(g Gomega) {
		output, err := client.GetInstanceConsoleOutput(ctx, instanceID, nil)
		g.Expect(err).NotTo(HaveOccurred())

		finished, boot = lastBoot(output.Contents)
		g.Expect(finished).NotTo(BeEmpty(), "cloud-init must report finishing on the console; it ends:\n%s",
			output.Contents[max(0, len(output.Contents)-2000):])
		g.Expect(finished).NotTo(Equal(previous), "no boot has finished since the last one seen")
	}).WithTimeout(config.TestTimeout).WithPolling(10 * time.Second).Should(Succeed())

	return finished, boot
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
				firstBoot, boot := eventuallyNextBoot(instanceID, "")
				Expect(boot).To(ContainSubstring(originalMarker))

				instance, err := client.GetInstance(ctx, instanceID)
				Expect(err).NotTo(HaveOccurred())

				By("replacing the user data and changing the image")
				update := updateFromRead(instance)
				update.Spec.UserData = &replacement
				update.Spec.ImageId = idstest.MustParseImageID(config.RebuildImageID)

				_, err = client.UpdateInstance(ctx, instanceID, update)
				Expect(err).NotTo(HaveOccurred())

				eventuallyRebuilt(instanceID)
				secondBoot, boot := eventuallyNextBoot(instanceID, firstBoot)
				Expect(boot).To(ContainSubstring(replacementMarker))
				Expect(boot).NotTo(ContainSubstring(originalMarker))

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
				_, boot = eventuallyNextBoot(instanceID, secondBoot)
				Expect(boot).NotTo(ContainSubstring(replacementMarker))
				Expect(boot).NotTo(ContainSubstring(originalMarker))
			})
		})
	})
})
