//go:build integration

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

//nolint:testpackage,revive // test package in suites is standard for these tests
package suites

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/unikorn-cloud/compute/test/api"
)

var _ = Describe("Security and Authentication", func() {
	Context("When accessing API with different authentication states", func() {
		Describe("Given invalid authentication", func() {
			It("should reject requests with invalid tokens", func() {
				invalidClient, err := api.NewAPIClient(config.BaseURL)
				Expect(err).NotTo(HaveOccurred())
				invalidClient.SetAuthToken("invalid-malformed-token-12345")

				_, listErr := invalidClient.ListRegions(ctx, config.OrgID)
				Expect(listErr).To(HaveOccurred())
				Expect(listErr.Error()).To(ContainSubstring("401"))
			})

			It("should reject requests with missing authentication", func() {
				unauthClient, err := api.NewAPIClient(config.BaseURL)
				Expect(err).NotTo(HaveOccurred())
				unauthClient.SetAuthToken("")

				_, listErr := unauthClient.ListRegions(ctx, config.OrgID)
				Expect(listErr).To(HaveOccurred())
				// TODO: API returns 400 but should return 401 for missing auth
				Expect(listErr.Error()).To(Or(
					ContainSubstring("400"),
					ContainSubstring("401"),
				))
			})
		})
	})

	Context("When submitting malicious input", func() {
		Describe("Given security testing", func() {

			DescribeTable("should reject XSS payloads",
				func(payload string) {
					_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
						api.NewClusterPayload().
							WithName(payload).
							WithDescription(payload).
							BuildTyped())

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("400"),
						ContainSubstring("validation"),
						ContainSubstring("invalid"),
					))
				},
				Entry("script tag", "<script>alert('XSS')</script>"),
				Entry("img onerror", "<img src=x onerror=alert('XSS')>"),
				Entry("javascript protocol", "javascript:alert('XSS')"),
				Entry("svg onload", "<svg onload=alert('XSS')>"),
			)

			DescribeTable("should handle path traversal attempts",
				func(payload string) {
					_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
						api.NewClusterPayload().
							WithName(payload).
							BuildTyped())

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("400"),
						ContainSubstring("validation"),
						ContainSubstring("invalid"),
					))
				},
				Entry("unix path traversal", "../../../etc/passwd"),
				Entry("windows path traversal", "..\\..\\windows\\system32"),
				Entry("double encoded traversal", "....//....//etc/passwd"),
				Entry("null byte injection", "valid-name\x00../../../etc/passwd"),
			)
		})

		Describe("Given encoding and Unicode issues", func() {
			DescribeTable("should handle Unicode characters properly",
				func(unicodeName string) {
					_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
						api.NewClusterPayload().
							WithName(unicodeName).
							BuildTyped())

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("400"),
						ContainSubstring("validation"),
						ContainSubstring("invalid"),
					))
				},
				Entry("emoji characters", "test-cluster-🚀🔥"),
				Entry("chinese characters", "测试集群"), //nolint:gosmopolitan // intentionally testing non-ASCII input
				Entry("arabic characters", "مجموعة-الاختبار"),
				Entry("cyrillic characters", "тест-кластер"),
			)

			DescribeTable("should handle invalid character encodings",
				func(payload string) {
					_, err := client.CreateCluster(ctx, config.OrgID, config.ProjectID,
						api.NewClusterPayload().
							WithName(payload).
							BuildTyped())

					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(Or(
						ContainSubstring("400"),
						ContainSubstring("validation"),
						ContainSubstring("invalid"),
					))
				},
				Entry("invalid UTF-8 start byte", "test\xc3\x28"),
				Entry("truncated multi-byte", "test\xc3"),
				Entry("overlong encoding", "test\xc0\xaf"),
			)
		})
	})
})
