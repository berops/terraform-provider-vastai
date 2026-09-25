# Terraform Provider for Vast.ai

A [Terraform](https://www.terraform.io) / [OpenTofu](https://opentofu.org) provider for renting GPU instances on [Vast.ai](https://vast.ai/).

The provider is developed and maintained by [Berops](https://berops.com/). It deliberately covers the minimum surface area needed to integrate Vast.ai into [Claudie](https://claudie.io/), a multi-cloud Kubernetes management platform. Contributions that expand coverage are welcome.

## Features

| Resource                                        | Description                                                                                   |
| ----------------------------------------------- | --------------------------------------------------------------------------------------------- |
| [`vastai_instance`](docs/resources/instance.md) | Rents a machine, either a specific offer or the first available one matching search criteria. |
| [`vastai_ssh_key`](docs/resources/ssh_key.md)   | Registers an SSH public key on the Vast.ai account.                                           |

## Authentication

The provider accepts the API key either in the provider block or via an environment variable.

```hcl
provider "vastai" {
  api_key = var.vastai_api_key
}
```

| Attribute | Environment variable | Description                                          |
| --------- | -------------------- | ---------------------------------------------------- |
| `api_key` | `VASTAI_API_KEY`     | Vast.ai API key. Required.                           |
| `api_url` | `VASTAI_API_URL`     | API base URL. Defaults to `https://console.vast.ai`. |

### Personal vs. team accounts

Vast.ai does not allow registering SSH keys with a team API key. If you rent instances under a team account, configure two provider aliases: one with a personal API key for `vastai_ssh_key` and one with the team API key for `vastai_instance`. If you only use a personal account, a single provider block is enough.

## Example Usage

The example below registers an SSH key and rents an offer matching a set of
filters as a virtual machine. The `search_offer` block mirrors the
[search offers](https://docs.vast.ai/api-reference/search/search-offers) API:
every filter is an object of operators (`eq`, `neq`, `gt`, `gte`, `lt`, `lte`,
`in`, `notin`). The provider rents the first available offer from the returned
list, so `order` decides which offer is picked; sort by `dph_total` ascending to
get the cheapest one. The rented offer is stored in `offer_id`. The search runs
only when the instance is created; plans, refreshes and destroys never touch the
marketplace.

```hcl
terraform {
  required_providers {
    vastai = {
      source = "berops/vastai"
    }
  }
}

variable "vastai_personal_api_key" {
  type      = string
  sensitive = true
}

variable "vastai_team_api_key" {
  type      = string
  sensitive = true
}

provider "vastai" {
  alias   = "personal"
  api_key = var.vastai_personal_api_key
}

provider "vastai" {
  alias   = "team"
  api_key = var.vastai_team_api_key
}

resource "vastai_ssh_key" "key" {
  provider   = vastai.personal
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "vastai_instance" "gpu" {
  provider = vastai.team

  # A VM instance requires an SSH key on the account before it is created.
  depends_on = [vastai_ssh_key.key]

  label          = "example-gpu"
  image          = "docker.io/vastai/kvm:@vastai-automatic-tag"
  vm             = true
  runtype        = "ssh"
  disk           = 150
  cancel_unavail = true

  search_offer {
    gpu_name    = { eq = "RTX 4090" }
    num_gpus    = { eq = 1 }
    disk_space  = { gte = 150 }
    reliability = { gte = 0.98 }
    verified    = { eq = true }
    datacenter  = { eq = true }
    vms_enabled = { eq = true }
    # sort cheapest first, the first available offer in this order is rented
    order = [["dph_total", "asc"]]
    # how many matches to try in turn
    limit = 5
  }
}

output "ssh_command" {
  value = "ssh -p ${vastai_instance.gpu.ssh_port} root@${vastai_instance.gpu.ssh_host}"
}
```

To rent a specific offer instead, set `offer_id` and leave out `search_offer`.
Exactly one of the two must be set.

Once the instance is running, connect to it as `root` with the private key matching the registered public key.

### API client

The Vast.ai API client in `internal/vastai/` is generated from `openapi.yaml` with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). The spec is a pristine copy of what Vast.ai publishes. Corrections to it live in `openapi-overlay.yaml`, and the set of generated operations is listed in `openapi-config.yaml`. After changing any of these files, regenerate the client:

```shell
go generate ./internal/vastai/
```
