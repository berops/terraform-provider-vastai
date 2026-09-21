package provider

import (
	"context"
	"os"
	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &VastAiProvider{}

type VastAiProvider struct {
	version string
}

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
	var model VastAiProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if model.ApiKey.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unknown Vast.ai API Key",
			"The provider cannot create the Vast.ai API client as there is an unknown configuration value for the Vast.ai API key.")
		return
	}

	if model.ApiUrl.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Unknown Vast.ai API URL",
			"The provider cannot create the Vast.ai API client as there is an unknown configuration value for the Vast.ai API URL")
		return
	}

	apiKey := os.Getenv("VASTAI_API_KEY")
	apiUrl := os.Getenv("VASTAI_API_URL")

	// override values with provided arguments
	if !model.ApiKey.IsNull() {
		apiKey = model.ApiKey.ValueString()
	}

	if !model.ApiUrl.IsNull() {
		apiUrl = model.ApiUrl.ValueString()
	}

	if apiUrl == "" {
		apiUrl = "https://console.vast.ai"
	}

	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing Vast.ai API Key",
			`The provider requires a Vast.ai API key to authenticate. Set the api_key attribute in the provider configuration block or use the VASTAI_API_KEY environment variable`,
		)
		return
	}

	client, err := vastai.New(apiKey, apiUrl)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Vast.ai API Client", err.Error())
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

func (p *VastAiProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &VastAiProvider{
			version: version,
		}
	}
}
