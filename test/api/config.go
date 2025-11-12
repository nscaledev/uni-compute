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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
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

// LoadTestConfig loads configuration from environment variables and .env files using viper.
// Returns an error if required configuration values are missing.
func LoadTestConfig() (*TestConfig, error) {
	v := viper.New()

	// Set up config file search paths for .env files
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath("../../../test")
	// Set default values
	v.SetDefault("REQUEST_TIMEOUT", "30s")
	v.SetDefault("TEST_TIMEOUT", "20m")
	v.SetDefault("SKIP_INTEGRATION", false)
	v.SetDefault("DEBUG_LOGGING", false)
	v.SetDefault("LOG_REQUESTS", false)
	v.SetDefault("LOG_RESPONSES", false)

	if err := v.ReadInConfig(); err != nil {
		// Only warn if it's not a "file not found" error
		if !errors.As(err, &errConfigFileNotFound) {
			fmt.Printf("Warning: error reading config file: %v\n", err)
		}
	}

	v.AutomaticEnv()

	config := &TestConfig{
		BaseURL:            v.GetString("API_BASE_URL"),
		IdentityBaseURL:    v.GetString("IDENTITY_BASE_URL"),
		AuthToken:          v.GetString("API_AUTH_TOKEN"),
		RequestTimeout:     getDurationFromViper(v, "REQUEST_TIMEOUT", 30*time.Second),
		TestTimeout:        getDurationFromViper(v, "TEST_TIMEOUT", 20*time.Minute),
		OrgID:              v.GetString("TEST_ORG_ID"),
		ProjectID:          v.GetString("TEST_PROJECT_ID"),
		SecondaryProjectID: v.GetString("TEST_SECONDARY_PROJECT_ID"),
		RegionID:           v.GetString("TEST_REGION_ID"),
		SecondaryRegionID:  v.GetString("TEST_SECONDARY_REGION_ID"),
		FlavorID:           v.GetString("TEST_FLAVOR_ID"),
		ImageID:            v.GetString("TEST_IMAGE_ID"),
		SkipIntegration:    v.GetBool("SKIP_INTEGRATION"),
		DebugLogging:       v.GetBool("DEBUG_LOGGING"),
		LogRequests:        v.GetBool("LOG_REQUESTS"),
		LogResponses:       v.GetBool("LOG_RESPONSES"),
	}

	if err := validateRequiredFields(config); err != nil {
		return nil, err
	}

	return config, nil
}

func getDurationFromViper(v *viper.Viper, key string, defaultValue time.Duration) time.Duration {
	duration := v.GetDuration(key)
	if duration < time.Millisecond {
		seconds := v.GetInt(key)
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	if duration > 0 {
		return duration
	}

	return defaultValue
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
		return &configError{missing: strings.Join(missing, ", ")}
	}

	return nil
}

var errConfigFileNotFound = viper.ConfigFileNotFoundError{}

type configError struct {
	missing string
}

func (e *configError) Error() string {
	return fmt.Sprintf("missing required configuration: %s. Please set these environment variables or add them to a .env file, or the gh secrets", e.missing)
}
