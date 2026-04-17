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
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"

	"github.com/unikorn-cloud/compute/test/api"
)

const (
	sshCommand = "id -un"
)

var sshUsers = []string{"cloud-user", "ubuntu"}

var _ = Describe("SSH Integration", func() {
	var regionClient *api.RegionAPIClient

	BeforeEach(func() {
		if !config.EnableSSHIntegration {
			Skip("SSH integration tests are disabled")
		}

		var err error
		regionClient, err = api.NewRegionClient("")
		Expect(err).NotTo(HaveOccurred(), "Failed to create region client")
	})

	It("should connect over SSH using the instance private key", func() {
		securityGroupID := api.CreateSSHOpenSecurityGroupWithCleanup(regionClient, ctx, config)

		_, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
			api.NewInstancePayload().
				WithPublicIP(true).
				WithSecurityGroups(securityGroupID).
				Build(),
		)

		api.WaitForInstanceActive(client, ctx, config, instanceID)
		publicIP := api.WaitForInstancePublicIP(client, ctx, config, instanceID)
		api.WaitForSSHReady(publicIP, 5*time.Minute)

		sshKey, err := client.GetInstanceSSHKey(ctx, instanceID)
		Expect(err).NotTo(HaveOccurred(), "Failed to get instance SSH key")

		signer, err := ssh.ParsePrivateKey([]byte(sshKey.PrivateKey))
		Expect(err).NotTo(HaveOccurred(), "Failed to parse instance SSH key")

		user, output, err := api.RunSSHCommandWithUserFallback(
			net.JoinHostPort(publicIP, "22"),
			sshUsers,
			func(string) ([]ssh.AuthMethod, error) {
				return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
			},
			30*time.Second,
			sshCommand,
		)
		Expect(err).NotTo(HaveOccurred(), "Failed to SSH to instance %s", instanceID)
		Expect(output).To(Equal(user))
	})

	It("should connect over SSH using a certificate authority", func() {
		securityGroupID := api.CreateSSHOpenSecurityGroupWithCleanup(regionClient, ctx, config)

		caKeyPair, err := api.NewSSHKeyPair()
		Expect(err).NotTo(HaveOccurred(), "Failed to generate SSH CA keypair")

		sshCertificateAuthorityID := api.CreateSSHCertificateAuthorityWithCleanup(regionClient, ctx, config, caKeyPair.AuthorizedKey)

		userKeyPair, err := api.NewSSHKeyPair()
		Expect(err).NotTo(HaveOccurred(), "Failed to generate SSH user keypair")

		_, instanceID := api.CreateInstanceWithCleanup(client, ctx, config,
			api.NewInstancePayload().
				WithPublicIP(true).
				WithSecurityGroups(securityGroupID).
				WithSSHCertificateAuthorityID(sshCertificateAuthorityID).
				Build(),
		)

		api.WaitForInstanceActive(client, ctx, config, instanceID)
		publicIP := api.WaitForInstancePublicIP(client, ctx, config, instanceID)
		api.WaitForSSHReady(publicIP, 5*time.Minute)

		user, output, err := api.RunSSHCommandWithUserFallback(
			net.JoinHostPort(publicIP, "22"),
			sshUsers,
			func(user string) ([]ssh.AuthMethod, error) {
				certificateAuth, err := api.SignSSHUserCertificate(
					caKeyPair.PrivateKeySigner,
					userKeyPair.PrivateKeySigner,
					user,
					15*time.Minute,
				)
				if err != nil {
					return nil, err
				}

				return []ssh.AuthMethod{certificateAuth}, nil
			},
			30*time.Second,
			sshCommand,
		)
		Expect(err).NotTo(HaveOccurred(), "Failed to SSH with certificate to instance %s", instanceID)
		Expect(output).To(Equal(user), fmt.Sprintf("unexpected SSH command output from instance %s", instanceID))
	})
})
