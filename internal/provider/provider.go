// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"os"
	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/ephemeral"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure ScaffoldingProvider satisfies various provider interfaces.
var _ provider.Provider = &VastAiProvider{}
var _ provider.ProviderWithFunctions = &VastAiProvider{}
var _ provider.ProviderWithEphemeralResources = &VastAiProvider{}
var _ provider.ProviderWithActions = &VastAiProvider{}

// VastAiProvider defines the provider implementation.
type VastAiProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// ScaffoldingProviderModel describes the provider data model.
type VastAiProviderModel struct {
	ApiKey types.String `tfsdk:"api_key"`
	ApiUrl types.String `tfsdk:"api_url"`
}

func (p *VastAiProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "vastai"
	resp.Version = p.version
}

func (p *VastAiProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "Vast.ai API key. Can also be set via VASTAI_API_KEY environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_url": schema.StringAttribute{
				Description: "Vast.ai API base URL. Can also be set via VASTAI_API_URL environment variable. Defaults to https://console.vast.ai.",
				Optional:    true,
			},
		},
	}
}

func (p *VastAiProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config VastAiProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	// Check for unknown values before accessing them. Unknown values can occur
	// when a provider attribute references a resource attribute that has not
	// yet been computed during the plan phase.
	if config.ApiKey.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unknown Vast.ai API Key",
			"The provider cannot create the Vast.ai API client as there is an unknown configuration value for the Vast.ai API key. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the VASTAI_API_KEY environment variable.",
		)
		return
	}

	if config.ApiUrl.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unknown Vast.ai API URL",
			"The provider cannot create the Vast.ai API client as there is an unknown configuration value for the Vast.ai API URL. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the VASTAI_API_URL environment variable.",
		)
		return
	}

	// Default values from environment variables, with config overrides.
	apiKey := os.Getenv("VASTAI_API_KEY")
	apiUrl := os.Getenv("VASTAI_API_URL")

	if !config.ApiKey.IsNull() {
		apiKey = config.ApiKey.ValueString()
	}

	if !config.ApiUrl.IsNull() {
		apiUrl = config.ApiUrl.ValueString()
	}

	if apiUrl == "" {
		apiUrl = "https://console.vast.ai"
	}

	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing Vast.ai API Key",
			"The provider requires a Vast.ai API key to authenticate. "+
				"Set the api_key attribute in the provider configuration block or use the VASTAI_API_KEY environment variable.",
		)
		return
	}

	client, err := vastai.New(apiKey, apiUrl)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Vast.ai API Client",
			"An unexpected error occurred when creating the Vast.ai API client: "+err.Error(),
		)
		return
	}
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *VastAiProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewInstanceResource,
		NewSshKeyResource,
	}
}

func (p *VastAiProvider) EphemeralResources(ctx context.Context) []func() ephemeral.EphemeralResource {
	return nil
}

func (p *VastAiProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return nil
}

func (p *VastAiProvider) Functions(ctx context.Context) []func() function.Function {
	return nil
}

func (p *VastAiProvider) Actions(ctx context.Context) []func() action.Action {
	return nil
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &VastAiProvider{
			version: version,
		}
	}
}
