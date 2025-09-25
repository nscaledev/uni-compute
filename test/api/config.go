package api

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

type TestConfig struct {
	BaseURL         string
	AuthToken       string
	RequestTimeout  time.Duration
	TestTimeout     time.Duration
	OrgID           string
	ProjectID       string
	RegionID        string
	FlavorID        string
	SkipIntegration bool
	DebugLogging    bool
	LogRequests     bool
	LogResponses    bool
}

func LoadTestConfig() *TestConfig {
	config := &TestConfig{
		BaseURL:         "REQUIRED: Set API_BASE_URL in the .env file",
		RequestTimeout:  30 * time.Second,
		TestTimeout:     60 * time.Second,
		OrgID:           "REQUIRED: Set TEST_ORG_ID in the .env file",
		ProjectID:       "REQUIRED: Set TEST_PROJECT_ID in the .env file",
		RegionID:        "REQUIRED: Set TEST_REGION_ID in the .env file",
		FlavorID:        "REQUIRED: Set TEST_FLAVOR_ID in the .env file",
		SkipIntegration: false,
		DebugLogging:    false,
		LogRequests:     true,
		LogResponses:    false,
	}

	envVars := loadEnvFile()

	if val := getEnvValue(envVars, "API_BASE_URL"); val != "" {
		config.BaseURL = val
	}
	if val := getEnvValue(envVars, "API_AUTH_TOKEN"); val != "" {
		config.AuthToken = val
	}
	if val := getEnvValue(envVars, "REQUEST_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil {
			config.RequestTimeout = time.Duration(timeout) * time.Second
		}
	}
	if val := getEnvValue(envVars, "TEST_TIMEOUT"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil {
			config.TestTimeout = time.Duration(timeout) * time.Second
		}
	}
	if val := getEnvValue(envVars, "TEST_ORG_ID"); val != "" {
		config.OrgID = val
	}
	if val := getEnvValue(envVars, "TEST_PROJECT_ID"); val != "" {
		config.ProjectID = val
	}
	if val := getEnvValue(envVars, "TEST_REGION_ID"); val != "" {
		config.RegionID = val
	}
	if val := getEnvValue(envVars, "TEST_FLAVOR_ID"); val != "" {
		config.FlavorID = val
	}
	if val := getEnvValue(envVars, "SKIP_INTEGRATION_TESTS"); val != "" {
		config.SkipIntegration = parseBool(val)
	}
	if val := getEnvValue(envVars, "ENABLE_DEBUG_LOGGING"); val != "" {
		config.DebugLogging = parseBool(val)
	}
	if val := getEnvValue(envVars, "LOG_HTTP_REQUESTS"); val != "" {
		config.LogRequests = parseBool(val)
	}
	if val := getEnvValue(envVars, "LOG_HTTP_RESPONSES"); val != "" {
		config.LogResponses = parseBool(val)
	}

	return config
}

func loadEnvFile() map[string]string {
	envVars := make(map[string]string)

	// Try multiple paths to find the .env file
	envPaths := []string{
		"test/.env",          // From project root
		"../.env",           // From test/api directory
		"../../../test/.env", // From test/api/suites directory
		".env",              // Current directory
	}

	var file *os.File
	var err error

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

func parseBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}
