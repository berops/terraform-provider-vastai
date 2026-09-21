package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"terraform-provider-vastai/internal/vastai"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	testAccInstanceResourceName = "vastai_instance.test"
	testAccInstanceImage        = "docker.io/vastai/kvm:@vastai-automatic-tag"
	testAccInstanceDisk         = 150
	testAccInstanceLabel        = "tf-acc-instance"
	testAccInstanceRelabel      = "tf-acc-instance-renamed"
)

func TestAccInstanceResource(t *testing.T) {
	testAccPreCheck(t)
	useTeamAPIKey(t)

	registerSshKey(t)
	offer := searchCheapestOffer(t)
	t.Logf("renting offer %d: %s in %s at $%.4f/h", offer.ID, offer.GPUName, offer.Geolocation, offer.DPHTotal)

	var instanceID int64

	nonEmpty := knownvalue.StringRegexp(regexp.MustCompile(`.+`))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkIfInstanceWasDestroyed(t),
		Steps: []resource.TestStep{
			// rent the offer and wait until the instance is running
			{
				Config: instanceResourceConfig(offer.ID, testAccInstanceLabel),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					// configured
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("id"), knownvalue.Int64Exact(offer.ID)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("image"), knownvalue.StringExact(testAccInstanceImage)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("disk"), knownvalue.Float64Exact(testAccInstanceDisk)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("runtype"), knownvalue.StringExact("ssh")),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("label"), knownvalue.StringExact(testAccInstanceLabel)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("vm"), knownvalue.Null()),
					// computed from the running instance
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("instance_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("machine_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("num_gpus"), knownvalue.Int64Exact(1)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("gpu_name"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("ssh_host"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("ssh_port"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("public_ipaddr"), nonEmpty),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("dph_total"), knownvalue.NotNull()),
				},
				Check: checkInstanceViaApi(t, vastai.InstanceStatusRunning, testAccInstanceLabel, &instanceID),
			},
			// reapply, idempotency check
			{
				Config: instanceResourceConfig(offer.ID, testAccInstanceLabel),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectEmptyPlan(),
					},
				},
			},
			// relabel and stop in place, in one update: same contract, no replacement
			{
				Config: instanceResourceConfigWithState(offer.ID, testAccInstanceRelabel, "stopped"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionUpdate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("label"), knownvalue.StringExact(testAccInstanceRelabel)),
					statecheck.ExpectKnownValue(testAccInstanceResourceName, tfjsonpath.New("target_state"), knownvalue.StringExact("stopped")),
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkInstanceViaApi(t, vastai.InstanceStatusExited, testAccInstanceRelabel, &instanceID),
				),
			},
			// start again in place
			{
				Config: instanceResourceConfigWithState(offer.ID, testAccInstanceRelabel, "running"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(testAccInstanceResourceName, plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					checkInstanceViaApi(t, vastai.InstanceStatusRunning, testAccInstanceRelabel, &instanceID),
				),
			},
		},
	})
}

func instanceResourceConfig(offerID int64, label string) string {
	return fmt.Sprintf(
		`resource "vastai_instance" "test" {
		  id      = %d
		  image   = %q
		  disk    = %d
		  runtype = "ssh"
		  label   = %q
		}`, offerID, testAccInstanceImage, testAccInstanceDisk, label)
}

func instanceResourceConfigWithState(offerID int64, label, targetState string) string {
	return fmt.Sprintf(
		`resource "vastai_instance" "test" {
		  id           = %d
		  image        = %q
		  disk         = %d
		  runtype      = "ssh"
		  label        = %q
		  target_state = %q
		}`, offerID, testAccInstanceImage, testAccInstanceDisk, label, targetState)
}

// registerSshKey uploads a throwaway SSH key for the duration of the test and
// removes it afterwards, the API refuses to create an instance on an account
// without one.
func registerSshKey(t *testing.T) {
	t.Helper()

	client, err := vastai.New(personalAPIKey(), testAccApiURL())
	if err != nil {
		t.Fatalf("creating vastai client: %v", err)
	}
	key, err := client.CreateSSHKey(t.Context(), generateSshKey(t, testAccInstanceLabel))
	if err != nil {
		t.Fatalf("registering ssh key: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := client.DeleteSSHKey(ctx, key.ID); err != nil {
			t.Errorf("deleting ssh key %d: %v", key.ID, err)
		}
	})
}

type offer struct {
	ID          int64   `json:"id"`
	GPUName     string  `json:"gpu_name"`
	Geolocation string  `json:"geolocation"`
	DPHTotal    float64 `json:"dph_total"`
}

func searchCheapestOffer(t *testing.T) offer {
	t.Helper()

	op := func(operator string, value any) map[string]any {
		return map[string]any{operator: value}
	}

	query, err := json.Marshal(map[string]any{
		"type":        vastai.SearchOffersJSONBodyTypeOndemand,
		"limit":       1,
		"order":       [][]string{{"dph_total", "asc"}},
		"verified":    op("eq", true),
		"datacenter":  op("eq", true),
		"rentable":    op("eq", true),
		"rented":      op("eq", false),
		"vms_enabled": op("eq", true),
		"num_gpus":    op("in", []int{1}),
		"reliability": op("gte", 0.94),
	})
	if err != nil {
		t.Fatalf("encoding offer search query: %v", err)
	}

	r, err := newVastAiClient(t).SearchOffersWithBodyWithResponse(t.Context(), "application/json", bytes.NewReader(query))
	if err != nil {
		t.Fatalf("searching offers: %v", err)
	}
	if r.StatusCode() != http.StatusOK {
		t.Fatalf("searching offers: status %d: %s", r.StatusCode(), r.Body)
	}

	var out struct {
		Offers []offer `json:"offers"`
	}

	if err := json.Unmarshal(r.Body, &out); err != nil {
		t.Fatalf("decoding offer search response: %v: %s", err, r.Body)
	}

	if len(out.Offers) == 0 {
		t.Fatal("offer search returned no offers")
	}

	return out.Offers[0]
}

func instanceIDFromState(s *terraform.State) (int64, error) {
	rs, ok := s.RootModule().Resources[testAccInstanceResourceName]
	if !ok {
		return 0, fmt.Errorf("%s not found in state", testAccInstanceResourceName)
	}
	id, err := strconv.ParseInt(rs.Primary.Attributes["instance_id"], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing instance_id %q: %w", rs.Primary.Attributes["instance_id"], err)
	}
	return id, nil
}

func checkInstanceViaApi(t *testing.T, wantStatus, wantLabel string, gotID *int64) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		id, err := instanceIDFromState(s)
		if err != nil {
			return err
		}
		inst, err := newVastAiClient(t).ShowInstance(t.Context(), id)
		if err != nil {
			return err
		}
		if status := inst.ActualStatus.GetOrEmpty(); status != wantStatus {
			return fmt.Errorf("instance %d has status %q, want %q", id, status, wantStatus)
		}
		if label := inst.Label.GetOrEmpty(); label != wantLabel {
			return fmt.Errorf("instance %d has label %q, want %q", id, label, wantLabel)
		}
		*gotID = id
		return nil
	}
}

func checkIfInstanceWasDestroyed(t *testing.T) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := newVastAiClient(t)
		for _, rs := range s.RootModule().Resources {
			if rs.Type != "vastai_instance" {
				continue
			}
			id, err := strconv.ParseInt(rs.Primary.Attributes["instance_id"], 10, 64)
			if err != nil {
				return fmt.Errorf("parsing instance_id %q: %w", rs.Primary.Attributes["instance_id"], err)
			}
			if err := waitForInstanceGone(t.Context(), client, id); err != nil {
				return err
			}
		}
		return nil
	}
}

// waitForInstanceGone polls until the API no longer knows the instance.
// Destruction is asynchronous, so a lookup right after destroying may still
// return it.
func waitForInstanceGone(ctx context.Context, client *vastai.Client, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		_, err := client.ShowInstance(ctx, id)
		if errors.Is(err, vastai.ErrNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("checking instance %d: %w", id, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("instance %d still exists after destroy", id)
		case <-time.After(5 * time.Second):
		}
	}
}
