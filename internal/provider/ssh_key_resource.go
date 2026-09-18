// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"regexp"
	"terraform-provider-vastai/internal/vastai"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource              = &sshKeyResource{}
	_ resource.ResourceWithConfigure = &sshKeyResource{}
)

func NewSshKeyResource() resource.Resource {
	return &sshKeyResource{}
}

type sshKeyResource struct {
	client *vastai.Client
}

type sshKeyResourceModel struct {
	ID        types.Int64  `tfsdk:"id"`
	PublicKey types.String `tfsdk:"public_key"`
	UserID    types.Int64  `tfsdk:"user_id"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (m *sshKeyResourceModel) applySSHKey(key vastai.SSHKey) {
	m.ID = types.Int64Value(key.ID)
	m.UserID = types.Int64Value(key.UserID)
	m.CreatedAt = types.StringValue(key.CreatedAt.Format(time.RFC3339))
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers an SSH public key on the Vast.ai account.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "ID of the SSH key.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"public_key": schema.StringAttribute{
				MarkdownDescription: "The SSH public key, as found in a `.pub` file.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^(ssh-|ecdsa-sha2-|sk-)\S+ \S+`),
						"must be an OpenSSH public key: a key type such as `ssh-ed25519`, a space, and the base64 key",
					),
				},
			},
			"user_id": schema.Int64Attribute{
				MarkdownDescription: "ID of the Vast.ai user that owns the key.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Time the key was registered, in RFC 3339 format.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *sshKeyResource) Configure(
	ctx context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*vastai.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *vastai.Client, got: %T", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model sshKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.CreateSSHKey(ctx, model.PublicKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating SSH key", err.Error())
		return
	}

	model.applySSHKey(key)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model sshKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys, err := r.client.ListSSHKeys(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading SSH key", err.Error())
		return
	}

	var found *vastai.SSHKey
	for i := range keys {
		if keys[i].ID == model.ID.ValueInt64() {
			found = &keys[i]
			break
		}
	}

	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	model.applySSHKey(*found)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var planModel, stateModel sshKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planModel)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &stateModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdateSSHKey(ctx, stateModel.ID.ValueInt64(), planModel.PublicKey.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error updating SSH key", err.Error())
		return
	}

	// Only the key material changes; the identity and timestamps carry over.
	planModel.ID = stateModel.ID
	planModel.UserID = stateModel.UserID
	planModel.CreatedAt = stateModel.CreatedAt

	resp.Diagnostics.Append(resp.State.Set(ctx, &planModel)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model sshKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSSHKey(ctx, model.ID.ValueInt64()); err != nil {
		resp.Diagnostics.AddError("Error deleting SSH key", err.Error())
		return
	}
}
