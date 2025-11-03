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

package api

import (
	"bufio"
	"os"
	"strings"
	"time"
)

type TestConfig struct {
	BaseURL            string
	IdentityBaseURL    string
	AuthToken          string
	RequestTimeout     time.Duration
	TestTimeout        time.Duration
	OrgID              string
	ProjectID          string
	SecondaryProjectID string
	RegionID           string
	SecondaryRegionID  string
	FlavorID           string
	ImageID            string
	SkipIntegration    bool
	DebugLogging       bool
	LogRequests        bool
	LogResponses       bool
}

// all of the errors that show below should only show locally, not in the CI/CD pipeline since we will add secrets.
func LoadTestConfig() *TestConfig {
	config := &TestConfig{
		BaseURL:            "REQUIRED: Set API_BASE_URL in the .env file",
		IdentityBaseURL:    "REQUIRED: Set IDENTITY_BASE_URL in the .env file",
		RequestTimeout:     30 * time.Second,
		TestTimeout:        20 * time.Minute, // 20 minutes is the default timeout for the tests for now, as thats what I had it in SOS
		OrgID:              "REQUIRED: Set TEST_ORG_ID in the .env file",
		ProjectID:          "REQUIRED: Set TEST_PROJECT_ID in the .env file",
		SecondaryProjectID: "REQUIRED: Set TEST_SECONDARY_PROJECT_ID in the .env file",
		RegionID:           "REQUIRED: Set TEST_REGION_ID in the .env file",
		SecondaryRegionID:  "REQUIRED: Set TEST_SECONDARY_REGION_ID in the .env file",
		FlavorID:           "REQUIRED: Set TEST_FLAVOR_ID in the .env file",
		ImageID:            "REQUIRED: Set TEST_IMAGE_ID in the .env file",
	}

	envVars := loadEnvFile()
	loadConfigFromEnv(config, envVars)

	return config
}

// loadConfigFromEnv loads configuration values from environment variables.
func loadConfigFromEnv(config *TestConfig, envVars map[string]string) {
	// String values
	setStringValue(&config.BaseURL, getEnvValue(envVars, "API_BASE_URL"))
	setStringValue(&config.IdentityBaseURL, getEnvValue(envVars, "IDENTITY_BASE_URL"))
	setStringValue(&config.AuthToken, getEnvValue(envVars, "API_AUTH_TOKEN"))
	setStringValue(&config.OrgID, getEnvValue(envVars, "TEST_ORG_ID"))
	setStringValue(&config.ProjectID, getEnvValue(envVars, "TEST_PROJECT_ID"))
	setStringValue(&config.SecondaryProjectID, getEnvValue(envVars, "TEST_SECONDARY_PROJECT_ID"))
	setStringValue(&config.RegionID, getEnvValue(envVars, "TEST_REGION_ID"))
	setStringValue(&config.SecondaryRegionID, getEnvValue(envVars, "TEST_SECONDARY_REGION_ID"))
	setStringValue(&config.FlavorID, getEnvValue(envVars, "TEST_FLAVOR_ID"))
	setStringValue(&config.ImageID, getEnvValue(envVars, "TEST_IMAGE_ID"))
}

// setStringValue sets a string value if the env value is not empty.
func setStringValue(target *string, value string) {
	if value != "" {
		*target = value
	}
}

func loadEnvFile() map[string]string {
	envVars := make(map[string]string)

	// Try multiple paths to find the .env file
	//TODO: this is a hack to get the tests to run, I need to fix this before I merge this PR
	envPaths := []string{
		"test/.env",          // From project root
		"../.env",            // From test/api directory
		"../../../test/.env", // From test/api/suites directory
		".env",               // Current directory
	}

	var (
		file *os.File
		err  error
	)

	for _, envPath := range envPaths {
		file, err = os.Open(envPath)
		if err == nil {
			break
		}
	}

	if err != nil {
		return envVars
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			envVars[key] = value
		}
	}

	return envVars
}

func getEnvValue(envVars map[string]string, key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	return envVars[key]
}
