package vastai

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// The API describes an instance with two different vocabularies:
//
//   - intended_status is what was asked for, set through target_state on
//     creation or through ManageInstance: "running" or "stopped".
//   - actual_status is what the container currently is. A stopped container
//     reports "exited", never "stopped".

// Instance intended_status values.
const (
	InstanceStateRunning = "running"
	InstanceStateStopped = "stopped"
)

// Instance actual_status values relevant to lifecycle handling.
const (
	InstanceStatusRunning = "running"
	InstanceStatusExited  = "exited"
	InstanceStatusUnknown = "unknown"
	InstanceStatusOffline = "offline"
)

// statusForState is the actual_status an instance reports once it has
// reached an intended state.
var statusForState = map[string]string{
	InstanceStateRunning: InstanceStatusRunning,
	InstanceStateStopped: InstanceStatusExited,
}

const pollInterval = 10 * time.Second

// ErrNotFound is reported when the requested object does not exist.
// APIErrors with a 404 status match it via errors.Is.
var ErrNotFound = errors.New("not found")

// Client is the generated client bound to a Vast.ai account. It adds
// authentication, uniform error handling and the lifecycle helpers the
// provider needs; every generated operation remains available on it.
type Client struct {
	*ClientWithResponses
	bearer string
}

// New returns a Client for baseURL (e.g. "https://console.vast.ai")
// authenticated with apiKey.
func New(apiKey, baseURL string) (*Client, error) {
	bearer := "Bearer " + apiKey
	auth := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", bearer)
		req.Header.Set("Accept", "application/json")
		return nil
	}
	c, err := NewClientWithResponses(baseURL,
		WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
		WithRequestEditorFn(auth),
	)
	if err != nil {
		return nil, err
	}
	return &Client{ClientWithResponses: c, bearer: bearer}, nil
}

// APIError is an error response from the API: a non-2xx status, or a 2xx
// body carrying {"success": false}, which Vast.ai uses for many failures.
type APIError struct {
	StatusCode int
	Code       string // the "error" field, e.g. "no_ssh_key"; may be empty
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Code != e.Message {
		return fmt.Sprintf("vast.ai: %s: %s (status %d)", e.Code, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("vast.ai: %s (status %d)", e.Message, e.StatusCode)
}

func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// response is the part of every generated *Response type check relies on.
type response interface {
	StatusCode() int
	GetBody() []byte
}

// check turns a failed call or an error response into an error.
func check(r response, err error) error {
	if err != nil {
		return err
	}
	var env struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
		Msg     string `json:"msg"`
		Detail  string `json:"detail"` // used by 429 responses
	}
	body := r.GetBody()
	_ = json.Unmarshal(body, &env) // best effort; error bodies are not always JSON
	if r.StatusCode() < 400 && (env.Success == nil || *env.Success) {
		return nil
	}
	return &APIError{
		StatusCode: r.StatusCode(),
		Code:       env.Error,
		Message:    cmp.Or(env.Msg, env.Detail, env.Error, strings.TrimSpace(string(body)), http.StatusText(r.StatusCode())),
	}
}

func hasCode(err error, code string) bool {
	var e *APIError
	return errors.As(err, &e) && e.Code == code
}

func hasStatus(err error, status int) bool {
	var e *APIError
	return errors.As(err, &e) && e.StatusCode == status
}

// CreateInstance rents offer askID and returns the ID of the new contract.
func (c *Client) CreateInstance(ctx context.Context, askID int64, body CreateInstanceJSONRequestBody) (int64, error) {
	r, err := c.CreateInstanceWithResponse(ctx, int(askID), body)
	if err := check(r, err); err != nil {
		return 0, fmt.Errorf("creating instance from offer %d: %w", askID, err)
	}
	if r.JSON200 == nil || r.JSON200.NewContract == nil {
		return 0, fmt.Errorf("creating instance from offer %d: no contract ID in response: %s", askID, r.Body)
	}
	return int64(*r.JSON200.NewContract), nil
}

// ShowInstance returns the instance, or ErrNotFound if it no longer exists.
func (c *Client) ShowInstance(ctx context.Context, id int64) (*Instance, error) {
	r, err := c.ShowInstanceWithResponse(ctx, int(id))
	if err := check(r, err); err != nil {
		return nil, fmt.Errorf("showing instance %d: %w", id, err)
	}
	// A destroyed instance may come back as an empty object rather than a 404.
	if r.JSON200 == nil || r.JSON200.Instances == nil || r.JSON200.Instances.ID == nil {
		return nil, fmt.Errorf("showing instance %d: %w", id, ErrNotFound)
	}
	return r.JSON200.Instances, nil
}

// DestroyInstance permanently destroys an instance and all its data.
// Destroying an instance that no longer exists is not an error.
func (c *Client) DestroyInstance(ctx context.Context, id int64) error {
	// The API expects a JSON body even though it carries nothing; the spec
	// (and so the generated request) has none.
	r, err := c.DestroyInstanceWithResponse(ctx, int(id), withJSONBody("{}"))
	if err := check(r, err); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("destroying instance %d: %w", id, err)
	}
	return nil
}

// ManageInstance changes what the API allows to change on a running contract:
// the intended state (start/stop) and the label. Nil fields are left as they
// are. A state change is asynchronous; use WaitForInstanceState to observe it.
func (c *Client) ManageInstance(ctx context.Context, id int64, body ManageInstanceJSONRequestBody) error {
	r, err := c.ManageInstanceWithResponse(ctx, int(id), body)
	if err := check(r, err); err != nil {
		return fmt.Errorf("managing instance %d: %w", id, err)
	}
	return nil
}

func withJSONBody(body string) RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/json")
		return nil
	}
}

// WaitForInstanceState polls until the instance has reached the intended
// state, InstanceStateRunning or InstanceStateStopped. An empty state means
// the API default, running.
func (c *Client) WaitForInstanceState(ctx context.Context, id int64, state string) (*Instance, error) {
	if state == "" {
		state = InstanceStateRunning
	}
	status, ok := statusForState[state]
	if !ok {
		return nil, fmt.Errorf("waiting for instance %d: unknown state %q", id, state)
	}
	return c.WaitForInstanceStatus(ctx, id, status)
}

// WaitForInstanceStatus polls until the instance's actual_status is target.
// It fails early if the instance enters a status it cannot recover from. The
// last instance seen is returned alongside any error.
func (c *Client) WaitForInstanceStatus(ctx context.Context, id int64, target string) (*Instance, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var last *Instance
	for {
		inst, err := c.ShowInstance(ctx, id)
		switch {
		case hasStatus(err, http.StatusTooManyRequests):
			// Being throttled must not fail an apply that has already rented a machine.
		case err != nil:
			return last, err
		default:
			last = inst
			status := inst.ActualStatus.GetOrEmpty()
			if status == target {
				return inst, nil
			}
			if isTerminalStatus(status) {
				msg := inst.StatusMsg.GetOrEmpty()
				if msg != "" {
					msg = ": " + msg
				}
				return inst, fmt.Errorf("instance %d entered status %q and will not reach %q%s", id, status, target, msg)
			}
		}

		select {
		case <-ctx.Done():
			return last, fmt.Errorf("waiting for instance %d to reach %q: %w", id, target, ctx.Err())
		case <-ticker.C:
		}
	}
}

// isTerminalStatus reports whether an instance in status will never reach
// running on its own; the API documents these as "destroy and retry".
func isTerminalStatus(status string) bool {
	switch status {
	case InstanceStatusExited, InstanceStatusUnknown, InstanceStatusOffline:
		return true
	}
	return false
}

// SSHKey is a public key registered on the account. The create and list
// endpoints return it in different shapes; this is their common form.
type SSHKey struct {
	ID        int64
	UserID    int64
	PublicKey string
	CreatedAt time.Time
}

// sshKeyJSON is the wire form of a key. The create endpoint calls the key
// material public_key and the list endpoint calls it key; both are read.
//
// The OpenAPI spec declares created_at as an RFC 3339 string, but the API
// sends epoch seconds, so the generated parsers reject every key response.
// This hand-written form decodes either representation.
type sshKeyJSON struct {
	ID        int64    `json:"id"`
	UserID    int64    `json:"user_id"`
	PublicKey string   `json:"public_key"`
	Key       string   `json:"key"`
	CreatedAt unixTime `json:"created_at"`
}

func (k sshKeyJSON) sshKey() SSHKey {
	return SSHKey{
		ID:        k.ID,
		UserID:    k.UserID,
		PublicKey: cmp.Or(k.PublicKey, k.Key),
		CreatedAt: k.CreatedAt.Time,
	}
}

// CreateSSHKey registers publicKey on the account. The API also adds it to
// every instance the account currently owns.
func (c *Client) CreateSSHKey(ctx context.Context, publicKey string) (SSHKey, error) {
	// The generated CreateSSHKeyWithResponse cannot decode the body (see
	// sshKeyJSON), so issue the request through the generated transport
	// and decode the body here.
	r, err := readResponse(c.ClientInterface.CreateSSHKey(ctx, CreateSSHKeyJSONRequestBody{SSHKey: publicKey}))
	if err := check(r, err); err != nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: %w", err)
	}
	var body struct {
		Key *sshKeyJSON `json:"key"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: decoding response: %w: %s", err, r.body)
	}
	if body.Key == nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: no key in response: %s", r.body)
	}
	k := body.Key.sshKey()
	k.PublicKey = cmp.Or(k.PublicKey, publicKey)
	return k, nil
}

// ListSSHKeys returns every key registered on the account.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	// Decoded by hand for the same reason as CreateSSHKey.
	r, err := readResponse(c.GetSSHKeysUser(ctx, &GetSSHKeysUserParams{Authorization: c.bearer}))
	switch err := check(r, err); {
	case errors.Is(err, ErrNotFound):
		return nil, nil // the API reports "no keys" as 404
	case err != nil:
		return nil, fmt.Errorf("listing ssh keys: %w", err)
	}
	var body []sshKeyJSON
	if err := json.Unmarshal(r.body, &body); err != nil {
		return nil, fmt.Errorf("listing ssh keys: decoding response: %w: %s", err, r.body)
	}
	keys := make([]SSHKey, 0, len(body))
	for _, k := range body {
		keys = append(keys, k.sshKey())
	}
	return keys, nil
}

// unixTime is a timestamp that decodes from the epoch-second numbers Vast.ai
// returns as well as from the RFC 3339 strings its spec promises, so the
// client keeps working if the API is ever brought in line with the spec.
type unixTime struct{ time.Time }

func (t *unixTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var secs float64
	if err := json.Unmarshal(b, &secs); err == nil {
		whole, frac := math.Modf(secs)
		t.Time = time.Unix(int64(whole), int64(math.Round(frac*1e9))).UTC()
		return nil
	}
	return json.Unmarshal(b, &t.Time)
}

// rawResponse is a fully read HTTP response. It satisfies response so that
// bodies the generated parsers cannot decode can still go through check.
type rawResponse struct {
	status int
	body   []byte
}

func (r rawResponse) StatusCode() int { return r.status }
func (r rawResponse) GetBody() []byte { return r.body }

// readResponse drains rsp into a rawResponse. It takes the (response, error)
// pair of a generated transport call directly so callers can wrap them.
func readResponse(rsp *http.Response, err error) (rawResponse, error) {
	if err != nil {
		return rawResponse{}, err
	}
	defer func() { _ = rsp.Body.Close() }()
	body, err := io.ReadAll(rsp.Body)
	if err != nil {
		return rawResponse{}, fmt.Errorf("reading response body: %w", err)
	}
	return rawResponse{status: rsp.StatusCode, body: body}, nil
}

// UpdateSSHKey replaces the public key stored under id.
func (c *Client) UpdateSSHKey(ctx context.Context, id int64, publicKey string) error {
	r, err := c.UpdateSSHKeyWithResponse(ctx, int(id), UpdateSSHKeyJSONRequestBody{SSHKey: publicKey})
	if err := check(r, err); err != nil {
		return fmt.Errorf("updating ssh key %d: %w", id, err)
	}
	return nil
}

// DeleteSSHKey removes a key from the account. Deleting a key that no longer
// exists is not an error.
func (c *Client) DeleteSSHKey(ctx context.Context, id int64) error {
	r, err := c.DeleteSSHKeyWithResponse(ctx, id)
	// A missing key is reported as 400 no_ssh_key rather than 404.
	if err := check(r, err); err != nil && !errors.Is(err, ErrNotFound) && !hasCode(err, "no_ssh_key") {
		return fmt.Errorf("deleting ssh key %d: %w", id, err)
	}
	return nil
}
