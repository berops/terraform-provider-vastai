// Copyright IBM Corp. 2021, 2026
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ resource.Resource              = &instanceResource{}
	_ resource.ResourceWithConfigure = &instanceResource{}
)

// NewInstanceResource is a helper function to simplify the provider implementation.
func NewInstanceResource() resource.Resource {
	return &instanceResource{}
}

// orderResource is the resource implementation.
type instanceResource struct {
	client *vastai.Client
}

// instanceResourceModel describes the resource data model. It mirrors the
// request body of PUT /api/v0/asks/{id} (create instance).
// instanceResourceModel describes the resource data model. Configurable
// attributes mirror the request body of PUT /api/v0/asks/{id} (create
// instance); computed attributes are populated from GET /api/v0/instances/{id}
// (show instance).
type instanceResourceModel struct {
	// --- Configurable (create instance request) ---
	ID             types.Int64   `tfsdk:"id"`
	Image          types.String  `tfsdk:"image"`
	TemplateHashID types.String  `tfsdk:"template_hash_id"`
	Label          types.String  `tfsdk:"label"`
	Disk           types.Float64 `tfsdk:"disk"`
	Runtype        types.String  `tfsdk:"runtype"`
	TargetState    types.String  `tfsdk:"target_state"`
	Price          types.Float64 `tfsdk:"price"`
	Env            types.String  `tfsdk:"env"`
	CancelUnavail  types.Bool    `tfsdk:"cancel_unavail"`
	VM             types.Bool    `tfsdk:"vm"`
	Onstart        types.String  `tfsdk:"onstart"`
	Args           types.List    `tfsdk:"args"`
	ArgsStr        types.String  `tfsdk:"args_str"`
	UseJupyterLab  types.Bool    `tfsdk:"use_jupyter_lab"`
	JupyterDir     types.String  `tfsdk:"jupyter_dir"`
	PythonUTF8     types.Bool    `tfsdk:"python_utf8"`
	LangUTF8       types.Bool    `tfsdk:"lang_utf8"`
	Force          types.Bool    `tfsdk:"force"`
	User           types.String  `tfsdk:"user"`
	ImageLogin     types.String  `tfsdk:"image_login"`
	VolumeInfo     types.Object  `tfsdk:"volume_info"`

	// --- Computed (show instance response) ---
	InstanceID   types.Int64   `tfsdk:"instance_id"`
	SSHHost      types.String  `tfsdk:"ssh_host"`
	SSHPort      types.Int64   `tfsdk:"ssh_port"`
	PublicIPAddr types.String  `tfsdk:"public_ipaddr"`
	Ports        types.Map     `tfsdk:"ports"`
	JupyterToken types.String  `tfsdk:"jupyter_token"`
	MachineID    types.Int64   `tfsdk:"machine_id"`
	GPUName      types.String  `tfsdk:"gpu_name"`
	NumGPUs      types.Int64   `tfsdk:"num_gpus"`
	GPUTotalRAM  types.Int64   `tfsdk:"gpu_totalram"`
	Geolocation  types.String  `tfsdk:"geolocation"`
	DPHTotal     types.Float64 `tfsdk:"dph_total"`
	StartDate    types.Float64 `tfsdk:"start_date"`
}

// volumeInfoModel describes the volume_info nested object.
type volumeInfoModel struct {
	CreateNew types.Bool   `tfsdk:"create_new"`
	VolumeID  types.Int64  `tfsdk:"volume_id"`
	Size      types.Int64  `tfsdk:"size"`
	MountPath types.String `tfsdk:"mount_path"`
}

// Optional+Computed attributes are unknown (not null) in the plan when the
// user leaves them unset, and the framework's Value*Pointer helpers return a
// pointer to the zero value for unknowns. These variants return nil instead so
// the field is omitted from the request.
func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueStringPointer()
}

// float32Ptr narrows to float32 because the generated request body types
// JSON numbers without an explicit format as float32.
func float32Ptr(v types.Float64) *float32 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	f := float32(v.ValueFloat64())
	return &f
}

func intPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return v.ValueBoolPointer()
}

// deref returns the value p points to, or the zero value when p is nil. The
// generated models use a pointer for every optional field.
func deref[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return v
}

// enumPtr converts a string attribute to one of the generated enum types.
func enumPtr[E ~string](v types.String) *E {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	e := E(v.ValueString())
	return &e
}

func (m *instanceResourceModel) toCreateInstanceRequest(ctx context.Context) (vastai.CreateInstanceJSONRequestBody, diag.Diagnostics) {
	var diags diag.Diagnostics

	req := vastai.CreateInstanceJSONRequestBody{
		// image is required by the API schema; when a template supplies it
		// the attribute is unknown here and an empty string is sent.
		Image:          m.Image.ValueString(),
		TemplateHashID: stringPtr(m.TemplateHashID),
		Label:          stringPtr(m.Label),
		Disk:           float32Ptr(m.Disk),
		Runtype:        enumPtr[vastai.CreateInstanceJSONBodyRuntype](m.Runtype),
		TargetState:    enumPtr[vastai.CreateInstanceJSONBodyTargetState](m.TargetState),
		Price:          float32Ptr(m.Price),
		Env:            stringPtr(m.Env),
		CancelUnavail:  boolPtr(m.CancelUnavail),
		VM:             boolPtr(m.VM),
		Onstart:        stringPtr(m.Onstart),
		ArgsStr:        stringPtr(m.ArgsStr),
		UseJupyterLab:  boolPtr(m.UseJupyterLab),
		JupyterDir:     stringPtr(m.JupyterDir),
		PythonUTF8:     boolPtr(m.PythonUTF8),
		LangUTF8:       boolPtr(m.LangUTF8),
		Force:          boolPtr(m.Force),
		User:           stringPtr(m.User),
		ImageLogin:     stringPtr(m.ImageLogin),
	}

	if !m.Args.IsNull() && !m.Args.IsUnknown() {
		var args []string
		diags.Append(m.Args.ElementsAs(ctx, &args, false)...)
		req.Args = &args
	}

	if !m.VolumeInfo.IsNull() && !m.VolumeInfo.IsUnknown() {
		var vi volumeInfoModel
		diags.Append(m.VolumeInfo.As(ctx, &vi, basetypes.ObjectAsOptions{})...)
		// The generated body declares volume_info as an anonymous struct, so
		// it has to be spelled out here to construct it.
		req.VolumeInfo = &struct {
			CreateNew *bool   `json:"create_new,omitempty"`
			MountPath *string `json:"mount_path,omitempty"`
			Size      *int    `json:"size,omitempty"`
			VolumeID  *int    `json:"volume_id,omitempty"`
		}{
			CreateNew: boolPtr(vi.CreateNew),
			MountPath: stringPtr(vi.MountPath),
			Size:      intPtr(vi.Size),
			VolumeID:  intPtr(vi.VolumeID),
		}
	}

	return req, diags
}

// applyInstance copies values from the API response into the computed
// attributes of the model, leaving configured attributes untouched.
func (m *instanceResourceModel) applyInstance(ctx context.Context, in *vastai.Instance) diag.Diagnostics {
	var diags diag.Diagnostics

	m.InstanceID = types.Int64Value(int64(deref(in.ID)))
	m.SSHHost = types.StringValue(deref(in.SSHHost))
	m.SSHPort = types.Int64Value(int64(deref(in.SSHPort)))
	m.PublicIPAddr = types.StringValue(deref(in.PublicIpaddr))
	m.JupyterToken = types.StringValue(deref(in.JupyterToken))
	m.MachineID = types.Int64Value(int64(deref(in.MachineID)))
	m.GPUName = types.StringValue(deref(in.GpuName))
	m.NumGPUs = types.Int64Value(int64(deref(in.NumGpus)))
	m.GPUTotalRAM = types.Int64Value(int64(deref(in.GpuTotalram)))
	m.Geolocation = types.StringValue(deref(in.Geolocation))
	m.DPHTotal = types.Float64Value(float64(deref(in.DphTotal)))
	m.StartDate = types.Float64Value(float64(deref(in.StartDate)))

	// Each container port usually has an IPv4 and an IPv6 binding on the same
	// host port; keep the first one.
	hostPorts := map[string]int64{}
	if in.Ports != nil {
		for port, bindings := range *in.Ports {
			if len(bindings) == 0 || bindings[0].HostPort == nil {
				continue
			}
			hp, err := strconv.ParseInt(*bindings[0].HostPort, 10, 64)
			if err != nil {
				diags.AddError("Unexpected port binding", fmt.Sprintf("host port %q for %s is not a number: %s", *bindings[0].HostPort, port, err))
				continue
			}
			hostPorts[port] = hp
		}
	}
	ports, d := types.MapValueFrom(ctx, types.Int64Type, hostPorts)
	diags.Append(d...)
	m.Ports = ports

	// Optional+Computed: only take the API value when the user didn't set one,
	// otherwise Terraform errors with "inconsistent result after apply".
	if m.Image.IsUnknown() {
		m.Image = types.StringValue(deref(in.ImageUUID))
	}
	if m.Disk.IsUnknown() {
		m.Disk = types.Float64Value(float64(deref(in.DiskSpace)))
	}
	if m.Runtype.IsUnknown() {
		m.Runtype = types.StringValue(deref(in.ImageRuntype))
	}
	if m.TargetState.IsUnknown() {
		m.TargetState = types.StringValue(deref(in.IntendedStatus))
	}
	if m.VM.IsUnknown() {
		m.VM = types.BoolValue(false)
	}

	return diags
}

// Metadata returns the resource type name.
func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

// Schema defines the schema for the resource.
func (r *instanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Creates a Vast.ai instance by accepting an offer (\"ask\") from a provider. " +
			"Use the search offers endpoint or data source to discover available machines. " +
			"When `template_hash_id` is set, template defaults are merged with (or overridden by) the " +
			"attributes set here: scalar attributes override the template, while `env` is merged by key " +
			"with the value set here winning on conflicts.",

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "ID of the offer (ask) to accept. Renting a different offer means renting a different machine, so changing this forces a new instance.",
				Required:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"instance_id": schema.Int64Attribute{
				MarkdownDescription: "ID of the instance contract created from the offer, returned by the API as `new_contract`. This is the ID used to manage the instance after creation.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Docker image to run, for example `vastai/base-image:@vastai-automatic-tag`. Optional only when `template_hash_id` supplies one.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"template_hash_id": schema.StringAttribute{
				MarkdownDescription: "Content-based hash ID of a template to use as base configuration, for example `4e17788f74f075dd9aab7d0d4427968f`.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"label": schema.StringAttribute{
				MarkdownDescription: "Custom name for the instance.",
				Optional:            true,
			},
			"disk": schema.Float64Attribute{
				MarkdownDescription: "Size of the local disk partition, in GB.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Float64{
					float64planmodifier.RequiresReplace(),
					float64planmodifier.UseStateForUnknown(),
				},
			},
			"runtype": schema.StringAttribute{
				MarkdownDescription: "Launch mode for the instance. Defaults to `ssh` unless `args` or `args_str` is set.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf(
						"ssh",
						"jupyter",
						"args",
						"ssh_proxy",
						"ssh_direct",
						"jupyter_proxy",
						"jupyter_direct",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"target_state": schema.StringAttribute{
				MarkdownDescription: "Desired state of the instance.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("running", "stopped"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"price": schema.Float64Attribute{
				MarkdownDescription: "Bid price per machine in $/hour, between 0.001 and 128. Only meaningful for interruptible instances.",
				Optional:            true,
			},
			"env": schema.StringAttribute{
				MarkdownDescription: "Environment variables and port mappings in Docker flag format, for example `-e HF_TOKEN=hf_xxx -p 8000:8000`. Merged by key with the template `env` when `template_hash_id` is set.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cancel_unavail": schema.BoolAttribute{
				MarkdownDescription: "Whether to cancel if the instance cannot start immediately. Defaults to `false` for interruptible instances and `true` for on-demand instances with `target_state = \"running\"`.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"vm": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a VM instance rather than a Docker instance. Note that a VM instance requires an SSH key registered on the account before creation.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"onstart": schema.StringAttribute{
				MarkdownDescription: "Commands to run when the instance starts, for example `env | grep _ >> /etc/environment; echo 'starting up'`. Limited to 4048 characters; gzip+base64 longer scripts.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"args": schema.ListAttribute{
				MarkdownDescription: "Arguments passed to the image entrypoint, for example `[\"bash\", \"-c\", \"echo 'starting up'\"]`. Mutually exclusive with `args_str`.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"args_str": schema.StringAttribute{
				MarkdownDescription: "Arguments passed to the image entrypoint as a single string, for example `bash -c \"echo 'starting up'\"`. Mutually exclusive with `args`.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"use_jupyter_lab": schema.BoolAttribute{
				MarkdownDescription: "Launch the instance with Jupyter Lab instead of Jupyter Notebook.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"jupyter_dir": schema.StringAttribute{
				MarkdownDescription: "Directory to launch Jupyter from, for example `/home/notebooks`.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"python_utf8": schema.BoolAttribute{
				MarkdownDescription: "Set Python's locale to C.UTF-8.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"lang_utf8": schema.BoolAttribute{
				MarkdownDescription: "Set the instance locale to C.UTF-8.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"force": schema.BoolAttribute{
				MarkdownDescription: "Skip sanity checks when creating from an existing instance.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"user": schema.StringAttribute{
				MarkdownDescription: "User to use with `docker create`. Breaks some images, use with caution.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"image_login": schema.StringAttribute{
				MarkdownDescription: "Docker registry credentials, if the image requires them.",
				Optional:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"volume_info": schema.SingleNestedAttribute{
				MarkdownDescription: "Volume to create or link to the instance.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"create_new": schema.BoolAttribute{
						MarkdownDescription: "`true` to create a new volume, `false` to link an existing one.",
						Optional:            true,
					},
					"volume_id": schema.Int64Attribute{
						MarkdownDescription: "When `create_new` is `false`, the ID of an existing volume. When `create_new` is `true`, the ID of a volume offer.",
						Optional:            true,
					},
					"size": schema.Int64Attribute{
						MarkdownDescription: "Size of the volume in GB. Only used when `create_new` is `true`.",
						Optional:            true,
					},
					"mount_path": schema.StringAttribute{
						MarkdownDescription: "Mount path for the volume inside the container, for example `/workspace`.",
						Optional:            true,
					},
				},
			},
			"ssh_host": schema.StringAttribute{
				MarkdownDescription: "Hostname to use when connecting to the instance over SSH, for example `ssh5.vast.ai`.",
				Computed:            true,
			},
			"ssh_port": schema.Int64Attribute{
				MarkdownDescription: "Port to use when connecting to the instance over SSH.",
				Computed:            true,
			},
			"public_ipaddr": schema.StringAttribute{
				MarkdownDescription: "Public IP address of the machine hosting the instance.",
				Computed:            true,
			},
			"ports": schema.MapAttribute{
				MarkdownDescription: "Host ports the instance's container ports are published on, keyed by container port and protocol, for example `{\"22/tcp\" = 38545}`.",
				Computed:            true,
				ElementType:         types.Int64Type,
			},
			"jupyter_token": schema.StringAttribute{
				MarkdownDescription: "Token used to authenticate against the instance's Jupyter server.",
				Computed:            true,
				Sensitive:           true,
			},
			"machine_id": schema.Int64Attribute{
				MarkdownDescription: "ID of the physical machine the instance runs on.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"gpu_name": schema.StringAttribute{
				MarkdownDescription: "Model of the GPUs attached to the instance, for example `RTX 4090`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"num_gpus": schema.Int64Attribute{
				MarkdownDescription: "Number of GPUs attached to the instance.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"gpu_totalram": schema.Int64Attribute{
				MarkdownDescription: "Total GPU memory across all attached GPUs, in MB.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"geolocation": schema.StringAttribute{
				MarkdownDescription: "Location of the machine hosting the instance, for example `Poland, PL`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"dph_total": schema.Float64Attribute{
				MarkdownDescription: "Total cost of the instance in dollars per hour, including storage.",
				Computed:            true,
			},
			"start_date": schema.Float64Attribute{
				MarkdownDescription: "Time the instance was created, as a Unix timestamp in seconds.",
				Computed:            true,
				PlanModifiers: []planmodifier.Float64{
					float64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *instanceResource) Configure(
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
			fmt.Sprintf("Expected *vastai.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

// Create creates the resource and sets the initial Terraform state.
func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// retrieve values from the plan
	var instance instanceResourceModel
	diags := req.Plan.Get(ctx, &instance)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqBody, diags := instance.toCreateInstanceRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	contractID, err := r.client.CreateInstance(ctx, instance.ID.ValueInt64(), reqBody)
	if err != nil {
		resp.Diagnostics.AddError("Error creating an instance", err.Error())
		return
	}

	createdInstance, err := r.client.WaitForInstanceStatus(ctx, contractID, vastai.InstanceStatusRunning)
	if err != nil {
		// The instance exists and is billing at this point. Record its ID so
		// Terraform tracks it (as tainted) instead of leaking it.
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), instance.ID)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), contractID)...)
		resp.Diagnostics.AddError("Error waiting for instance to become ready", err.Error())
		return
	}

	resp.Diagnostics.Append(instance.applyInstance(ctx, createdInstance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &instance)...)
}

// Read refreshes the Terraform state with the latest data.
func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var instance instanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &instance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if instance.InstanceID.IsNull() || instance.InstanceID.IsUnknown() {
		resp.Diagnostics.AddError(
			"Error reading instance",
			"The instance has no instance_id in state, so it cannot be looked up through the API.",
		)
		return
	}

	inst, err := r.client.ShowInstance(ctx, instance.InstanceID.ValueInt64())
	if errors.Is(err, vastai.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading instance", err.Error())
		return
	}

	// Only computed attributes are refreshed. Configurable ones are left as the
	// user wrote them: the API returns normalized forms (e.g. image_uuid for
	// image) that would otherwise show up as a permanent diff and, with
	// RequiresReplace on most of them, force a new instance on every apply.
	resp.Diagnostics.Append(instance.applyInstance(ctx, inst)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &instance)...)
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var instance instanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &instance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if instance.InstanceID.IsNull() || instance.InstanceID.IsUnknown() {
		resp.Diagnostics.AddError(
			"Error destroying instance",
			"The instance has no instance_id in state, so it cannot be destroyed through the API. Remove it from state manually and destroy it in the Vast.ai console.",
		)
		return
	}

	if err := r.client.DestroyInstance(ctx, instance.InstanceID.ValueInt64()); err != nil {
		resp.Diagnostics.AddError("Error destroying instance", err.Error())
		return
	}
	// On success the framework removes the resource from state.
}
