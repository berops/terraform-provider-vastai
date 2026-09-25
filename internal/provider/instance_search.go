package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// The search_offer block mirrors the body of POST /api/v0/bundles
// (https://docs.vast.ai/api-reference/search/search-offers). Every filter is
// an object of operators, exactly as in the API:
//
//	search_offer {
//	  gpu_name    = { eq = "RTX 4090" }
//	  reliability = { gte = 0.98 }
//	  geolocation = { in = ["DE", "AT"] }
//	}
//
// Boolean filters take eq/neq, string filters add in/notin, and number filters
// add gt/gte/lt/lte. `rentable` and `rented` are not exposed: the provider
// always asks for rentable offers that are not rented yet.

type filterKind int

const (
	boolFilter filterKind = iota
	stringFilter
	numberFilter
)

var searchOfferFilters = []struct {
	name string
	kind filterKind
	desc string
}{
	{"bw_nvlink", numberFilter, "NVLink interconnect bandwidth in GB/s."},
	{"compute_cap", numberFilter, "CUDA compute capability x 100, e.g. 750 for 7.5."},
	{"cpu_arch", stringFilter, "Host CPU architecture, e.g. `amd64`."},
	{"cpu_cores", numberFilter, "Number of virtual CPUs."},
	{"cpu_cores_effective", numberFilter, "Effective vCPU count for the offer."},
	{"cpu_ghz", numberFilter, "CPU clock speed in GHz."},
	{"cpu_ram", numberFilter, "CPU RAM in MB."},
	{"cuda_max_good", numberFilter, "Maximum supported CUDA version."},
	{"datacenter", boolFilter, "Datacenter hosts only."},
	{"direct_port_count", numberFilter, "Number of direct ports."},
	{"disk_bw", numberFilter, "Disk read bandwidth in MB/s."},
	{"disk_space", numberFilter, "Disk space in GB."},
	{"dlperf", numberFilter, "Deep learning performance score."},
	{"dlperf_per_dphtotal", numberFilter, "DLPerf per dollar per hour."},
	{"dph_total", numberFilter, "Total rental cost in dollars per hour."},
	{"driver_version", stringFilter, "NVIDIA driver version, e.g. `535.129.03`."},
	{"duration", numberFilter, "Time the offer stays available for rent, in seconds."},
	{"external", boolFilter, "Include external offers in addition to datacenter offers."},
	{"flops_per_dphtotal", numberFilter, "TFLOPs per dollar per hour."},
	{"geolocation", stringFilter, "Two-letter country code of the host."},
	{"gpu_arch", stringFilter, "GPU architecture, e.g. `nvidia` or `amd`."},
	{"gpu_display_active", boolFilter, "Whether the GPU has a display attached."},
	{"gpu_frac", numberFilter, "Fraction of the machine's GPUs in the offer."},
	{"gpu_max_power", numberFilter, "GPU power limit in watts."},
	{"gpu_max_temp", numberFilter, "GPU temperature limit in Celsius."},
	{"gpu_mem_bw", numberFilter, "GPU memory bandwidth in GB/s."},
	{"gpu_name", stringFilter, "GPU model, e.g. `RTX 4090`."},
	{"gpu_ram", numberFilter, "GPU RAM per GPU in MB."},
	{"gpu_total_ram", numberFilter, "Total GPU RAM across all GPUs in MB."},
	{"has_avx", numberFilter, "CPU supports AVX (`1`) or not (`0`)."},
	{"host_id", numberFilter, "Host user ID."},
	{"inet_down", numberFilter, "Download bandwidth in Mbps."},
	{"inet_down_cost", numberFilter, "Download bandwidth cost in dollars per GB."},
	{"inet_up", numberFilter, "Upload bandwidth in Mbps."},
	{"inet_up_cost", numberFilter, "Upload bandwidth cost in dollars per GB."},
	{"machine_id", numberFilter, "ID of a specific host machine."},
	{"min_bid", numberFilter, "Minimum bid price in dollars per hour."},
	{"mobo_name", stringFilter, "Motherboard name."},
	{"num_gpus", numberFilter, "Number of GPUs."},
	{"os_version", stringFilter, "Host Ubuntu version."},
	{"pci_gen", numberFilter, "PCIe generation."},
	{"pcie_bw", numberFilter, "PCIe bandwidth between CPU and GPU."},
	{"reliability", numberFilter, "Host reliability score between 0 and 1."},
	{"static_ip", boolFilter, "Hosts with a static IP address."},
	{"storage_cost", numberFilter, "Storage cost in dollars per GB per month."},
	{"total_flops", numberFilter, "Total GPU compute performance in TFLOPs."},
	{"verification", stringFilter, "Verification status: `verified`, `deverified` or `unverified`."},
	{"verified", boolFilter, "Verified hosts only."},
	{"vms_enabled", boolFilter, "Hosts that can run VM instances. Set when `vm` is `true`."},
}

func searchOfferBlock() schema.SingleNestedBlock {
	attrs := map[string]schema.Attribute{
		"type": schema.StringAttribute{
			MarkdownDescription: "Offer type: `ondemand` (the default), `bid` or `reserved`.",
			Optional:            true,
			Validators:          []validator.String{stringvalidator.OneOf("ondemand", "bid", "reserved")},
		},
		"limit": schema.Int64Attribute{
			MarkdownDescription: "How many matching offers to fetch and try in turn before giving up. Defaults to 5. " +
				"Raise it when many instances are created at once, since each one takes an offer.",
			Optional:   true,
			Validators: []validator.Int64{int64validator.AtLeast(1)},
		},
		"order": schema.ListAttribute{
			MarkdownDescription: "Sort order as `[field, \"asc\"|\"desc\"]` pairs. Defaults to `[[\"dph_total\", \"asc\"]]`, cheapest first.",
			Optional:            true,
			ElementType:         types.ListType{ElemType: types.StringType},
		},
		"allocated_storage": schema.Float64Attribute{
			MarkdownDescription: "Disk size in GB used to price the offers. Defaults to 8.",
			Optional:            true,
		},
	}
	for _, f := range searchOfferFilters {
		attrs[f.name] = filterAttribute(f.kind, f.desc)
	}

	return schema.SingleNestedBlock{
		MarkdownDescription: "Search the marketplace for an offer at create time instead of naming one with `offer_id`. " +
			"Every filter is an object of operators as in the Vast.ai API, e.g. `reliability = { gte = 0.98 }`. " +
			"Number filters accept `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in` and `notin`, string filters `eq`, `neq`, `in` and `notin`, " +
			"and boolean filters `eq` and `neq`. " +
			"The provider rents the first available offer from the returned list, sorted by `order`; the rented one is stored in `offer_id`. " +
			"Changing the criteria forces a new instance.",
		Attributes: attrs,
		PlanModifiers: []planmodifier.Object{
			// Replace when the criteria change, but not when the block is
			// first added to an imported instance (state null) or removed in
			// favour of offer_id (plan null).
			objectplanmodifier.RequiresReplaceIf(
				func(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
					resp.RequiresReplace = !req.StateValue.IsNull() && !req.PlanValue.IsNull()
				},
				"Changing the search criteria forces a new instance.",
				"Changing the search criteria forces a new instance.",
			),
		},
	}
}

// filterAttribute builds the operator object for one filter field.
func filterAttribute(kind filterKind, desc string) schema.Attribute {
	ops := map[string]schema.Attribute{}
	switch kind {
	case boolFilter:
		for _, op := range []string{"eq", "neq"} {
			ops[op] = schema.BoolAttribute{Optional: true}
		}
	case stringFilter:
		for _, op := range []string{"eq", "neq"} {
			ops[op] = schema.StringAttribute{Optional: true}
		}
		for _, op := range []string{"in", "notin"} {
			ops[op] = schema.ListAttribute{Optional: true, ElementType: types.StringType}
		}
	case numberFilter:
		for _, op := range []string{"eq", "neq", "gt", "gte", "lt", "lte"} {
			ops[op] = schema.Float64Attribute{Optional: true}
		}
		for _, op := range []string{"in", "notin"} {
			ops[op] = schema.ListAttribute{Optional: true, ElementType: types.Float64Type}
		}
	}
	return schema.SingleNestedAttribute{MarkdownDescription: desc, Optional: true, Attributes: ops}
}

// searchOfferQuery turns the block into the request body. Unset attributes
// are left out, so the API's own defaults apply to everything not listed here.
func searchOfferQuery(block types.Object) map[string]any {
	q := map[string]any{
		"type":  "ondemand",
		"limit": 5,
		"order": [][]string{{"dph_total", "asc"}},
	}
	for name, v := range block.Attributes() {
		if !v.IsNull() {
			q[name] = toGo(v)
		}
	}
	q["rentable"] = map[string]any{"eq": true}
	q["rented"] = map[string]any{"eq": false}
	return q
}

// toGo converts a known framework value into plain Go for JSON encoding.
// Null attributes inside objects are dropped.
func toGo(v attr.Value) any {
	switch v := v.(type) {
	case types.Bool:
		return v.ValueBool()
	case types.String:
		return v.ValueString()
	case types.Int64:
		return v.ValueInt64()
	case types.Float64:
		return v.ValueFloat64()
	case types.List:
		out := make([]any, 0, len(v.Elements()))
		for _, e := range v.Elements() {
			out = append(out, toGo(e))
		}
		return out
	case types.Object:
		out := map[string]any{}
		for k, e := range v.Attributes() {
			if !e.IsNull() {
				out[k] = toGo(e)
			}
		}
		return out
	}
	return nil
}

// rentMatchingOffer searches for offers and rents the first one that is still
// available. Offers vanish as soon as anyone rents them, so a candidate that
// is gone by the time we get to it is skipped, not treated as an error.
func (r *instanceResource) rentMatchingOffer(ctx context.Context, block types.Object, body vastai.CreateInstanceJSONRequestBody) (offerID, instanceID int64, err error) {
	query := searchOfferQuery(block)
	offers, err := r.client.SearchOffers(ctx, query)
	if err != nil {
		return 0, 0, err
	}
	if len(offers) == 0 {
		return 0, 0, fmt.Errorf("no offers on the marketplace match the search criteria, relax them or retry later:\n%s", prettyJSON(query))
	}

	for _, offer := range offers {
		tflog.Info(ctx, "renting offer", map[string]any{
			"offer_id": offer.OfferID, "gpu": offer.GPUName, "num_gpus": offer.NumGPUs,
			"dph_total": offer.DPHTotal, "geolocation": offer.Geolocation,
		})
		instanceID, err := r.client.CreateInstance(ctx, offer.OfferID, body)
		if errors.Is(err, vastai.ErrOfferUnavailable) {
			tflog.Info(ctx, "offer is no longer available, trying the next one", map[string]any{"offer_id": offer.OfferID})
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		return offer.OfferID, instanceID, nil
	}
	return 0, 0, fmt.Errorf("all %d matching offers were rented by someone else first, retry or raise search_offer.limit:\n%s", len(offers), prettyJSON(query))
}

func prettyJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
