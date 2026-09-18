// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"os"
	"testing"

	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/echoprovider"
)

// testAccProtoV6ProviderFactories is used to instantiate a provider during acceptance testing.
// The factory function is called for each Terraform CLI command to create a provider
// server that the CLI can connect to and interact with.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"vastai": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccProtoV6ProviderFactoriesWithEcho includes the echo provider alongside the vastai provider.
// It allows for testing assertions on data returned by an ephemeral resource during Open.
// The echoprovider is used to arrange tests by echoing ephemeral data into the Terraform state.
// This lets the data be referenced in test assertions with state checks.
var testAccProtoV6ProviderFactoriesWithEcho = map[string]func() (tfprotov6.ProviderServer, error){
	"vastai": providerserver.NewProtocol6WithError(New("test")()),
	"echo":   echoprovider.NewProviderServer(),
}

// testAccPreCheck fails fast when the credentials acceptance tests need are missing.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("VASTAI_API_KEY") == "" {
		t.Fatal("VASTAI_API_KEY must be set for acceptance tests")
	}
}

// testAccApiURL returns the API base URL the provider under test talks to.
func testAccApiURL() string {
	if apiURL := os.Getenv("VASTAI_API_URL"); apiURL != "" {
		return apiURL
	}
	return "https://console.vast.ai"
}

// newVastAiClient returns an API client configured the same way the provider
// configures itself, for verifying remote state outside of Terraform.
func newVastAiClient(t *testing.T) *vastai.Client {
	t.Helper()
	client, err := vastai.New(os.Getenv("VASTAI_API_KEY"), testAccApiURL())
	if err != nil {
		t.Fatalf("creating vastai client: %v", err)
	}
	return client
}
