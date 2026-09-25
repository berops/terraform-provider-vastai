package provider

import (
	"os"
	"testing"

	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"vastai": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests are skipped unless TF_ACC is set")
	}
	if personalAPIKey() == "" || teamAPIKey() == "" {
		t.Fatal("VASTAI_PERSONAL_API_KEY and VASTAI_TEAM_API_KEY must be set for acceptance tests")
	}
}

func personalAPIKey() string { return os.Getenv("VASTAI_PERSONAL_API_KEY") }
func teamAPIKey() string     { return os.Getenv("VASTAI_TEAM_API_KEY") }

func usePersonalAPIKey(t *testing.T) {
	t.Helper()
	t.Setenv("VASTAI_API_KEY", personalAPIKey())
}

func useTeamAPIKey(t *testing.T) {
	t.Helper()
	t.Setenv("VASTAI_API_KEY", teamAPIKey())
}

func testAccApiURL() string {
	if apiURL := os.Getenv("VASTAI_API_URL"); apiURL != "" {
		return apiURL
	}
	return "https://console.vast.ai"
}

func newVastAiClient(t *testing.T) *vastai.Client {
	t.Helper()
	client, err := vastai.New(os.Getenv("VASTAI_API_KEY"), testAccApiURL())
	if err != nil {
		t.Fatalf("creating vastai client: %v", err)
	}
	return client
}
