# Terraform Provider for Vast.ai

A [Terraform](https://www.terraform.io) / [OpenTofu](https://opentofu.org) provider for renting GPU instances on [Vast.ai](https://vast.ai/).

The provider is developed and maintained by [Berops](https://berops.com/). It deliberately covers the minimum surface area needed to integrate Vast.ai into [Claudie](https://claudie.io/), a multi-cloud Kubernetes management platform. Contributions that expand coverage are welcome.

## Features

| Resource                                        | Description                                         |
| ----------------------------------------------- | --------------------------------------------------- |
| [`vastai_instance`](docs/resources/instance.md) | Rents a machine by accepting an offer.              |
| [`vastai_ssh_key`](docs/resources/ssh_key.md)   | Registers an SSH public key on the Vast.ai account. |

There is no data source for searching offers. Use the [`hashicorp/http`](https://registry.terraform.io/providers/hashicorp/http/latest/docs/data-sources/http) provider to query the [bundles endpoint](https://docs.vast.ai/api-reference/search/search-offers) directly, as shown in the example below.

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

The example below finds the cheapest offer matching a set of filters, registers an SSH key, and rents the offer as a virtual machine.

```hcl
terraform {
  required_providers {
    vastai = {
      source  = "berops/vastai"
    }
    http = {
      source  = "hashicorp/http"
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

# Search for offers. See https://docs.vast.ai/api-reference/search/search-offers
# for the full list of filters.
data "http" "vm_offers" {
  url    = "https://console.vast.ai/api/v0/bundles/"
  method = "POST"

  request_headers = {
    Authorization  = "Bearer ${var.vastai_team_api_key}"
    "Content-Type" = "application/json"
  }

  request_body = jsonencode({
    type        = "ondemand"
    verified    = { eq = true }
    datacenter  = { eq = true }
    rentable    = { eq = true }
    rented      = { eq = false }
    vms_enabled = { eq = true }
    reliability = { gte = 0.92 }
    num_gpus    = { eq = 1 }
    disk_space  = { gte = 150 }
    limit       = 3
    order       = [["dph_total", "asc"]]
  })
}

locals {
  vm_offers         = jsondecode(data.http.vm_offers.response_body).offers
  cheapest_vm_offer = local.vm_offers[0]
}

resource "vastai_ssh_key" "key" {
  provider   = vastai.personal
  public_key = file(pathexpand("~/.ssh/id_ed25519.pub"))
}

resource "vastai_instance" "gpu" {
  provider = vastai.team

  # A VM instance requires an SSH key on the account before it is created.
  depends_on = [vastai_ssh_key.key]

  id             = local.cheapest_vm_offer.id
  label          = "example-gpu"
  image          = "docker.io/vastai/kvm:@vastai-automatic-tag"
  vm             = true
  runtype        = "ssh"
  disk           = 150
  cancel_unavail = true

  # The offer search runs on every plan and the cheapest offer changes often.
  # Without this, a new cheapest offer would force the instance to be replaced.
  lifecycle {
    ignore_changes = [id]
  }
}

output "ssh_command" {
  value = "ssh -p ${vastai_instance.gpu.ssh_port} root@${vastai_instance.gpu.ssh_host}"
}

output "hourly_cost_usd" {
  value = vastai_instance.gpu.dph_total
}
```

Once the instance is running, connect to it as `root` with the private key matching the registered public key.

### API client

The Vast.ai API client in `internal/vastai/` is generated from `openapi.yaml` with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). The spec is a pristine copy of what Vast.ai publishes. Corrections to it live in `openapi-overlay.yaml`, and the set of generated operations is listed in `openapi-config.yaml`. After changing any of these files, regenerate the client:

```shell
go generate ./internal/vastai/
```
