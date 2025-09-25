package suites

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/unikorn-cloud/compute/test/api"
)

var (
	client *api.APIClient
	ctx    context.Context
	config *api.TestConfig
)

var _ = BeforeEach(func() {
	config = api.LoadTestConfig()
	client = api.NewAPIClientWithConfig(config)
	ctx = context.Background()
})

func TestSuites(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Test Suites")
}
