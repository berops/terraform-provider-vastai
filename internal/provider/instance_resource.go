package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

var (
	_ resource.Resource                     = &instanceResource{}
	_ resource.ResourceWithConfigure        = &instanceResource{}
	_ resource.ResourceWithConfigValidators = &instanceResource{}
	_ resource.ResourceWithImportState      = &instanceResource{}
)

func NewInstanceResource() resource.Resource {
	return &instanceResource{}
}

type instanceResource struct {
	client *vastai.Client
}

type instanceResourceModel struct {
	// Vast.ai also calls this the contract ID
	InstanceID types.Int64 `tfsdk:"id"`

	// --- Offer selection: exactly one of the two ---
	OfferID     types.Int64  `tfsdk:"offer_id"`
	SearchOffer types.Object `tfsdk:"search_offer"`

	// --- Configurable (create instance request) ---
	Image          types.String  `tfsdk:"image"`
	TemplateHashID types.String  `tfsdk:"template_hash_id"`
	Label          types.String  `tfsdk:"label"`
	Disk           types.Float64 `tfsdk:"disk"`
	Runtype        types.String  `tfsdk:"runtype"`
	TargetState    types.String  `tfsdk:"target_state"`
	// attribute price is applicable only to interruptible instance
	// we do not care about it right now, it simplifies Udpate function
	// Price          types.Float64 `tfsdk:"price"`
	Env           types.String `tfsdk:"env"`
	CancelUnavail types.Bool   `tfsdk:"cancel_unavail"`
	VM            types.Bool   `tfsdk:"vm"`
	Onstart       types.String `tfsdk:"onstart"`
	Args          types.List   `tfsdk:"args"`
	ArgsStr       types.String `tfsdk:"args_str"`
	UseJupyterLab types.Bool   `tfsdk:"use_jupyter_lab"`
	JupyterDir    types.String `tfsdk:"jupyter_dir"`
	PythonUTF8    types.Bool   `tfsdk:"python_utf8"`
	LangUTF8      types.Bool   `tfsdk:"lang_utf8"`
	Force         types.Bool   `tfsdk:"force"`
	User          types.String `tfsdk:"user"`
	ImageLogin    types.String `tfsdk:"image_login"`
	VolumeInfo    types.Object `tfsdk:"volume_info"`

	// --- Computed (show instance response) ---
	SSHHost      types.String  `tfsdk:"ssh_host"`
	SSHPort      types.Int64   `tfsdk:"ssh_port"`
	PublicIPAddr types.String  `tfsdk:"public_ipaddr"`
	Ports        types.Map     `tfsdk:"ports"`
	JupyterToken types.String  `tfsdk:"jupyter_token"`
	MachineID    types.Int64   `tfsdk:"machine_id"`
	GPUName      types.String  `tfsdk:"gpu_name"`
	NumGPUs      types.Int64   `tfsdk:"num_gpus"`
	GPUTotalRAM  types.Int64   `tfsdk:"gpu_total_ram"`
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

// The generated request bodies use *int and enum types where the framework
// offers *int64 and *string, so these convert. They also return nil for
// unknown values: Optional+Computed attributes are unknown (not null) in the
// plan when left unset, and the framework's Value*Pointer getters would return
// a pointer to the zero value for them. Attributes that are only Optional are
// always null or known at apply time and use the getters directly.

func intPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
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
		TemplateHashID: m.TemplateHashID.ValueStringPointer(),
		Label:          m.Label.ValueStringPointer(),
		Runtype:        enumPtr[vastai.CreateInstanceJSONBodyRuntype](m.Runtype),
		TargetState:    enumPtr[vastai.CreateInstanceJSONBodyTargetState](m.TargetState),
		// Price:          m.Price.ValueFloat64Pointer(),
		Env:           m.Env.ValueStringPointer(),
		CancelUnavail: m.CancelUnavail.ValueBoolPointer(),
		Onstart:       m.Onstart.ValueStringPointer(),
		ArgsStr:       m.ArgsStr.ValueStringPointer(),
		UseJupyterLab: m.UseJupyterLab.ValueBoolPointer(),
		JupyterDir:    m.JupyterDir.ValueStringPointer(),
		PythonUTF8:    m.PythonUTF8.ValueBoolPointer(),
		LangUTF8:      m.LangUTF8.ValueBoolPointer(),
		Force:         m.Force.ValueBoolPointer(),
		User:          m.User.ValueStringPointer(),
		ImageLogin:    m.ImageLogin.ValueStringPointer(),
		VM:            m.VM.ValueBoolPointer(),
	}

	// disk is Optional+Computed, so it is unknown rather than null when unset.
	// The generated body types JSON numbers without a format as float32.
	if !m.Disk.IsUnknown() && !m.Disk.IsNull() {
		disk := float32(m.Disk.ValueFloat64())
		req.Disk = &disk
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
			CreateNew: vi.CreateNew.ValueBoolPointer(),
			MountPath: vi.MountPath.ValueStringPointer(),
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
	// otherwise Terraform errors with "inconsistent result after apply". They
	// are unknown when unset in a plan and null right after an import.
	unset := func(v attr.Value) bool { return v.IsUnknown() || v.IsNull() }
	if unset(m.Image) {
		m.Image = types.StringValue(deref(in.ImageUUID))
	}
	if unset(m.Disk) {
		m.Disk = types.Float64Value(float64(deref(in.DiskSpace)))
	}
	if unset(m.Runtype) {
		m.Runtype = types.StringValue(deref(in.ImageRuntype))
	}
	if unset(m.TargetState) {
		m.TargetState = types.StringValue(deref(in.IntendedStatus))
	}

	return diags
}

func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func (r *instanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Rents a Vast.ai instance, either from a specific offer (`offer_id`) or from the offers matching a `search_offer` block. With `search_offer`, the provider rents the first available offer from the returned list, so the `order` attribute decides which offer wins.",
		Blocks: map[string]schema.Block{
			"search_offer": searchOfferBlock(),
		},
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "ID of the instance (contract).",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"offer_id": schema.Int64Attribute{
				MarkdownDescription: "ID of the offer (ask) to rent. Mutually exclusive with `search_offer`, which " +
					"sets this to the offer it rented. Changing a configured value forces a new instance.",
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
					int64planmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"image": schema.StringAttribute{
				MarkdownDescription: "Docker image to run. Required unless `template_hash_id` supplies one.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"template_hash_id": schema.StringAttribute{
				MarkdownDescription: "Hash ID of a template to use as base configuration.",
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
					float64planmodifier.UseStateForUnknown(),
					float64planmodifier.RequiresReplace(),
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
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"target_state": schema.StringAttribute{
				MarkdownDescription: "Desired state of the instance, `running` (the default) or `stopped`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("running", "stopped"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			// "price": schema.Float64Attribute{
			// 	MarkdownDescription: "Bid price per machine in $/hour, between 0.001 and 128. Only used for interruptible instances. Changing this forces a new instance.",
			// 	Optional:            true,
			// 	PlanModifiers: []planmodifier.Float64{
			// 		float64planmodifier.RequiresReplace(),
			// 	},
			// },
			"env": schema.StringAttribute{
				MarkdownDescription: "Environment variables and port mappings in Docker flag format. Merged by key with the template `env` when `template_hash_id` is set.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cancel_unavail": schema.BoolAttribute{
				MarkdownDescription: "Cancel the request if the instance cannot start immediately. Defaults to `true` for on-demand instances and `false` for interruptible ones.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"vm": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a VM instance rather than a Docker instance. Requires an SSH key on the account before creation.",
				Optional:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.RequiresReplace(),
				},
			},
			"onstart": schema.StringAttribute{
				MarkdownDescription: "Commands to run when the instance starts. Limited to 4048 characters.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"args": schema.ListAttribute{
				MarkdownDescription: "Arguments passed to the image entrypoint. Mutually exclusive with `args_str`.",
				Optional:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"args_str": schema.StringAttribute{
				MarkdownDescription: "Arguments passed to the image entrypoint as a single string. Mutually exclusive with `args`.",
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
				MarkdownDescription: "Directory to launch Jupyter from.",
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
				MarkdownDescription: "User to use with `docker create`. Breaks some images.",
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
						MarkdownDescription: "ID of an existing volume, or of a volume offer when `create_new` is `true`.",
						Optional:            true,
					},
					"size": schema.Int64Attribute{
						MarkdownDescription: "Size of the volume in GB. Only used when `create_new` is `true`.",
						Optional:            true,
					},
					"mount_path": schema.StringAttribute{
						MarkdownDescription: "Mount path for the volume inside the container.",
						Optional:            true,
					},
				},
			},
			"ssh_host": schema.StringAttribute{
				MarkdownDescription: "Hostname to use when connecting to the instance over SSH.",
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
				MarkdownDescription: "Host ports the container ports are published on, keyed by container port and protocol.",
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
				MarkdownDescription: "Model of the GPUs attached to the instance.",
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
			"gpu_total_ram": schema.Int64Attribute{
				MarkdownDescription: "Total GPU memory across all attached GPUs, in MB.",
				Computed:            true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"geolocation": schema.StringAttribute{
				MarkdownDescription: "Location of the machine hosting the instance.",
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

func (r *instanceResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("offer_id"), path.MatchRoot("search_offer")),
	}
}

// ImportState imports an instance by its ID. The offer it was rented from is
// not recorded by the API, so offer_id and search_offer stay empty.
func (r *instanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	instanceID, err := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected the numeric instance ID, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), instanceID)...)
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
			fmt.Sprintf("Expected *vastai.Client, got: %T", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model instanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	reqBody, diags := model.toCreateInstanceRequest(ctx)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var err error
	offerID := model.OfferID.ValueInt64()
	var instanceID int64
	if model.SearchOffer.IsNull() {
		instanceID, err = r.client.CreateInstance(ctx, offerID, reqBody)
	} else {
		offerID, instanceID, err = r.rentMatchingOffer(ctx, model.SearchOffer, reqBody)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error creating an instance", err.Error())
		return
	}

	// The instance is rented and billing from this point on. Record its ID
	// before anything else can fail, so that a failed apply still tracks it
	// and the next plan destroys it instead of leaving it running.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), instanceID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("offer_id"), offerID)...)
	model.OfferID = types.Int64Value(offerID)

	terminalActualStatuses := []string{vastai.ActualStatusExited, vastai.ActualStatusUnknown, vastai.ActualStatusOffline}
	createdInstance, err := r.client.WaitForIntendedStatus(ctx, instanceID, model.TargetState.ValueString(), terminalActualStatuses)
	if err != nil {
		resp.Diagnostics.AddError("Error waiting for instance to become ready", err.Error())
		return
	}

	resp.Diagnostics.Append(model.applyInstance(ctx, createdInstance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model instanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inst, err := r.client.ShowInstance(ctx, model.InstanceID.ValueInt64())
	if errors.Is(err, vastai.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading instance", err.Error())
		return
	}

	// update computed attributes
	resp.Diagnostics.Append(model.applyInstance(ctx, inst)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// can update only target state and label, everything else requires replacement.
func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var planModel, stateModel instanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planModel)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &stateModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceID := stateModel.InstanceID.ValueInt64()

	body := vastai.ManageInstanceJSONRequestBody{
		Label: planModel.Label.ValueStringPointer(),
		State: enumPtr[vastai.ManageInstanceJSONBodyState](planModel.TargetState),
	}

	if err := r.client.ManageInstance(ctx, instanceID, body); err != nil {
		resp.Diagnostics.AddError("Error updating instance", err.Error())
		return
	}

	terminalActualStatuses := []string{vastai.ActualStatusUnknown, vastai.ActualStatusOffline}
	inst, err := r.client.WaitForIntendedStatus(ctx, instanceID, planModel.TargetState.ValueString(), terminalActualStatuses)
	if err != nil {
		resp.Diagnostics.AddError("Error waiting for instance after update", err.Error())
		return
	}

	resp.Diagnostics.Append(planModel.applyInstance(ctx, inst)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &planModel)...)
}

func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model instanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceID := model.InstanceID.ValueInt64()
	if err := r.client.DestroyInstance(ctx, instanceID); err != nil {
		resp.Diagnostics.AddError("Error destroying instance", err.Error())
		return
	}

	// Destruction is asynchronous. Keep the resource in state until the API
	// no longer knows the instance, so a destroy that fails server-side is
	// not silently forgotten while it keeps billing.
	if _, err := r.client.WaitForIntendedStatus(ctx, instanceID, vastai.IntendedStatusGone, nil); err != nil {
		resp.Diagnostics.AddError("Error waiting for instance to be destroyed", err.Error())
		return
	}
}
