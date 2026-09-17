package vastai

const (
	StatusRunning = "running"
	StatusExited  = "exited"
	StatusUnknown = "unknown"
	StatusOffline = "offline"
)

type CreateInstanceRequest struct {
	// ClientID identifies the renting account. The API expects the literal
	// "me" for the authenticated user; the client fills it in.
	ClientID       string             `json:"client_id"`
	Image          *string            `json:"image,omitempty"`
	TemplateHashID *string            `json:"template_hash_id,omitempty"`
	Label          *string            `json:"label,omitempty"`
	Disk           *float64           `json:"disk,omitempty"`
	Runtype        *string            `json:"runtype,omitempty"`
	TargetState    *string            `json:"target_state,omitempty"`
	Price          *float64           `json:"price,omitempty"`
	Env            *string            `json:"env,omitempty"`
	CancelUnavail  *bool              `json:"cancel_unavail,omitempty"`
	VM             *bool              `json:"vm,omitempty"`
	Onstart        *string            `json:"onstart,omitempty"`
	Args           []string           `json:"args,omitempty"`
	ArgsStr        *string            `json:"args_str,omitempty"`
	UseJupyterLab  *bool              `json:"use_jupyter_lab,omitempty"`
	JupyterDir     *string            `json:"jupyter_dir,omitempty"`
	PythonUTF8     *bool              `json:"python_utf8,omitempty"`
	LangUTF8       *bool              `json:"lang_utf8,omitempty"`
	Force          *bool              `json:"force,omitempty"`
	User           *string            `json:"user,omitempty"`
	ImageLogin     *string            `json:"image_login,omitempty"`
	VolumeInfo     *volumeInfoRequest `json:"volume_info,omitempty"`
}

// volumeInfoRequest is the volume_info object of createInstanceRequest.
type volumeInfoRequest struct {
	CreateNew *bool   `json:"create_new,omitempty"`
	VolumeID  *int64  `json:"volume_id,omitempty"`
	Size      *int64  `json:"size,omitempty"`
	MountPath *string `json:"mount_path,omitempty"`
}

// CreateInstanceResponse is the body returned by PUT /api/v0/asks/{id}.
type CreateInstanceResponse struct {
	Success     bool  `json:"success"`
	NewContract int64 `json:"new_contract"`
}

// DestroyInstanceResponse is the body returned by DELETE /api/v0/instances/{id}.
type DestroyInstanceResponse struct {
	Success bool   `json:"success"`
	Msg     string `json:"msg"`
}

// ShowInstanceResponse is response of GET /api/v0/instances/{id}.
type ShowInstanceResponse struct {
	Instances Instance `json:"instances"`
}

// PricingDetails is the shape of the "search" and "instance" pricing objects.
type PricingDetails struct {
	GPUCostPerHour         float64 `json:"gpuCostPerHour"`
	DiskHour               float64 `json:"diskHour"`
	TotalHour              float64 `json:"totalHour"`
	DiscountTotalHour      float64 `json:"discountTotalHour"`
	DiscountedTotalPerHour float64 `json:"discountedTotalPerHour"`
}

// PortBinding is one host-side mapping of a container port, in Docker's
// NetworkSettings.Ports format. HostPort is a string in the API.
type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// Instance is the detailed description of a rented instance (contract).
//
// Fields the API documents as nullable are pointers so a missing value can be
// distinguished from a zero value.
type Instance struct {
	// Identity and lifecycle
	ID             int64   `json:"id"`
	ActualStatus   *string `json:"actual_status"`
	IntendedStatus string  `json:"intended_status"`
	CurState       string  `json:"cur_state"`
	NextState      string  `json:"next_state"`
	StatusMsg      *string `json:"status_msg"`
	Label          *string `json:"label"`

	// Template
	TemplateID     *int64  `json:"template_id"`
	TemplateHashID *string `json:"template_hash_id"`
	TemplateName   *string `json:"template_name"`

	// Container configuration
	ImageUUID    string   `json:"image_uuid"`
	ImageArgs    []string `json:"image_args"`
	ImageRuntype string   `json:"image_runtype"`
	ExtraEnv     []string `json:"extra_env"`
	Onstart      *string  `json:"onstart"`
	JupyterToken string   `json:"jupyter_token"`

	// Connectivity
	LocalIPAddrs      string                   `json:"local_ipaddrs"` // space-separated
	PublicIPAddr      string                   `json:"public_ipaddr"`
	StaticIP          bool                     `json:"static_ip"`
	SSHHost           string                   `json:"ssh_host"`
	SSHIdx            string                   `json:"ssh_idx"`
	SSHPort           int                      `json:"ssh_port"`
	MachineDirSSHPort int                      `json:"machine_dir_ssh_port"`
	Ports             map[string][]PortBinding `json:"ports"` // keyed by "<port>/<proto>", e.g. "22/tcp"
	DirectPortCount   int                      `json:"direct_port_count"`
	DirectPortStart   int                      `json:"direct_port_start"`
	DirectPortEnd     int                      `json:"direct_port_end"`
	Webpage           *string                  `json:"webpage"`

	// Host / placement
	MachineID    int64  `json:"machine_id"`
	BundleID     int64  `json:"bundle_id"`
	HostID       int64  `json:"host_id"`
	HostingType  int    `json:"hosting_type"`
	Geolocation  string `json:"geolocation"`
	Verification string `json:"verification"`
	Rentable     bool   `json:"rentable"`
	External     bool   `json:"external"`
	OSVersion    string `json:"os_version"`
	MoboName     string `json:"mobo_name"`
	Logo         string `json:"logo"`

	// Timing (epoch seconds / minutes)
	StartDate          float64  `json:"start_date"`
	EndDate            float64  `json:"end_date"`
	Duration           float64  `json:"duration"`
	UptimeMins         *float64 `json:"uptime_mins"`
	ClientRunTime      float64  `json:"client_run_time"`
	HostRunTime        float64  `json:"host_run_time"`
	TimeRemaining      string   `json:"time_remaining"`
	TimeRemainingIsBid string   `json:"time_remaining_isbid"`

	// CPU / memory
	CPUArch           string   `json:"cpu_arch"`
	CPUName           string   `json:"cpu_name"`
	CPUCores          int      `json:"cpu_cores"`
	CPUCoresEffective float64  `json:"cpu_cores_effective"`
	CPURAM            int64    `json:"cpu_ram"` // MB
	CPUUtil           float64  `json:"cpu_util"`
	MemLimit          *float64 `json:"mem_limit"`
	MemUsage          *float64 `json:"mem_usage"`
	VMemUsage         *float64 `json:"vmem_usage"`

	// GPU
	GPUName     string   `json:"gpu_name"`
	GPUArch     string   `json:"gpu_arch"`
	NumGPUs     int      `json:"num_gpus"`
	GPUTotalRAM int64    `json:"gpu_totalram"` // MB
	GPURAM      int64    `json:"gpu_ram"`      // MB in the REST API, GB in the CLI
	GPUUtil     *float64 `json:"gpu_util"`
	GPUTemp     *float64 `json:"gpu_temp"`
	GPUFrac     float64  `json:"gpu_frac"`
	GPULanes    int      `json:"gpu_lanes"`
	GPUMemBW    float64  `json:"gpu_mem_bw"`
	BWNVLink    float64  `json:"bw_nvlink"`
	PCIGen      float64  `json:"pci_gen"`
	PCIeBW      float64  `json:"pcie_bw"`

	// Disk
	DiskName  string  `json:"disk_name"`
	DiskSpace float64 `json:"disk_space"` // GB
	DiskBW    float64 `json:"disk_bw"`    // MB/s
	DiskUtil  float64 `json:"disk_util"`
	DiskUsage float64 `json:"disk_usage"`

	// Pricing
	MinBid            float64  `json:"min_bid"`
	IsBid             bool     `json:"is_bid"`
	DPHBase           float64  `json:"dph_base"`
	DPHTotal          float64  `json:"dph_total"`
	StorageCost       float64  `json:"storage_cost"`
	StorageTotalCost  float64  `json:"storage_total_cost"`
	VRAMCostPerHour   float64  `json:"vram_costperhour"`
	CreditBalance     *float64 `json:"credit_balance"`
	CreditDiscount    *float64 `json:"credit_discount"`
	CreditDiscountMax float64  `json:"credit_discount_max"`

	// Pricing breakdowns
	Search   PricingDetails `json:"search"`
	Instance PricingDetails `json:"instance"`

	// Benchmarks
	DLPerf            float64 `json:"dlperf"`
	DLPerfPerDPHTotal float64 `json:"dlperf_per_dphtotal"`
	FlopsPerDPHTotal  float64 `json:"flops_per_dphtotal"`
	TotalFlops        float64 `json:"total_flops"`
	Score             float64 `json:"score"`
	Reliability2      float64 `json:"reliability2"`
}
