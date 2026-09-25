# Rent an offer matching the search criteria. The provider rents the first
# available offer from the returned list, so sort by dph_total ascending to
# get the cheapest one. Every filter is an object of operators as in the
# Vast.ai search API:
# https://docs.vast.ai/api-reference/search/search-offers
resource "vastai_instance" "gpu" {
  provider       = vastai.team
  label          = "example-instance"
  image          = "docker.io/vastai/kvm:@vastai-automatic-tag"
  vm             = true
  runtype        = "ssh"
  cancel_unavail = true
  disk           = 150

  search_offer {
    gpu_name    = { eq = "RTX 4090" }
    num_gpus    = { eq = 1 }
    disk_space  = { gte = 150 }
    reliability = { gte = 0.98 }
    geolocation = { in = ["US", "SK", "PL"] }
    verified    = { eq = true }
    vms_enabled = { eq = true }
    static_ip   = { eq = true }
    inet_down   = { gte = 300 }
    dph_total   = { lte = 0.5 }

    order = [["dph_total", "asc"]]
  }
}

# Or rent a specific offer.
resource "vastai_instance" "pinned" {
  provider = vastai.team
  offer_id = 12345678
  image    = "docker.io/vastai/kvm:@vastai-automatic-tag"
  vm       = true
  disk     = 150
}
