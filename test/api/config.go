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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
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

// LoadTestConfig loads configuration from environment variables and .env files.
// Returns an error if required configuration values are missing.
func LoadTestConfig() (*TestConfig, error) {
	loadEnvFile()

	requestTimeout := getDurationWithDefault("REQUEST_TIMEOUT", 30*time.Second)
	testTimeout := getDurationWithDefault("TEST_TIMEOUT", 20*time.Minute)
	config := &TestConfig{
		BaseURL:            os.Getenv("API_BASE_URL"),
		IdentityBaseURL:    os.Getenv("IDENTITY_BASE_URL"),
		AuthToken:          os.Getenv("API_AUTH_TOKEN"),
		RequestTimeout:     requestTimeout,
		TestTimeout:        testTimeout,
		OrgID:              os.Getenv("TEST_ORG_ID"),
		ProjectID:          os.Getenv("TEST_PROJECT_ID"),
		SecondaryProjectID: os.Getenv("TEST_SECONDARY_PROJECT_ID"),
		RegionID:           os.Getenv("TEST_REGION_ID"),
		SecondaryRegionID:  os.Getenv("TEST_SECONDARY_REGION_ID"),
		FlavorID:           os.Getenv("TEST_FLAVOR_ID"),
		ImageID:            os.Getenv("TEST_IMAGE_ID"),
		SkipIntegration:    getBoolWithDefault("SKIP_INTEGRATION", false),
		DebugLogging:       getBoolWithDefault("DEBUG_LOGGING", false),
		LogRequests:        getBoolWithDefault("LOG_REQUESTS", false),
		LogResponses:       getBoolWithDefault("LOG_RESPONSES", false),
	}

	// Validate required fields
	if err := validateRequiredFields(config); err != nil {
		return nil, err
	}

	return config, nil
}

// getDurationWithDefault gets a duration from environment variable or returns default.
func getDurationWithDefault(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}

	return duration
}

// getBoolWithDefault gets a boolean from environment variable or returns default.
func getBoolWithDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}

	return boolValue
}

func loadEnvFile() {

	envPaths := []string{
		"../../../test/.env", // From test/api/suites directory
	}

	var envPath string
	for _, path := range envPaths {
		if _, err := os.Stat(path); err == nil {
			absPath, err := filepath.Abs(path)
			if err == nil {
				envPath = absPath
				break
			}
		}
	}

	if envPath == "" {
		// .env file not found - this is OK in CI/CD where env vars are set directly
		return
	}

	// Load .env file
	if err := godotenv.Load(envPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load .env file from %s: %v\n", envPath, err)
	}
}

// validateRequiredFields checks that all required configuration values are set.
func validateRequiredFields(config *TestConfig) error {
	var missing []string

	required := map[string]string{
		"API_BASE_URL":              config.BaseURL,
		"IDENTITY_BASE_URL":         config.IdentityBaseURL,
		"TEST_ORG_ID":               config.OrgID,
		"TEST_PROJECT_ID":           config.ProjectID,
		"TEST_SECONDARY_PROJECT_ID": config.SecondaryProjectID,
		"TEST_REGION_ID":            config.RegionID,
		"TEST_SECONDARY_REGION_ID":  config.SecondaryRegionID,
		"TEST_FLAVOR_ID":            config.FlavorID,
		"TEST_IMAGE_ID":             config.ImageID,
	}

	for envVar, value := range required {
		if value == "" {
			missing = append(missing, envVar)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s. Please set these environment variables or add them to a .env file, or the gh secrets", strings.Join(missing, ", "))
	}

	return nil
}
