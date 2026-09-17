package vastai

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Instance actual_status values relevant to lifecycle handling.
const (
	InstanceStatusRunning = "running"
	InstanceStatusExited  = "exited"
	InstanceStatusUnknown = "unknown"
	InstanceStatusOffline = "offline"
)

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

func withJSONBody(body string) RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Body = io.NopCloser(strings.NewReader(body))
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/json")
		return nil
	}
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

// CreateSSHKey registers publicKey on the account. The API also adds it to
// every instance the account currently owns.
func (c *Client) CreateSSHKey(ctx context.Context, publicKey string) (SSHKey, error) {
	r, err := c.CreateSSHKeyWithResponse(ctx, CreateSSHKeyJSONRequestBody{SSHKey: publicKey})
	if err := check(r, err); err != nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: %w", err)
	}
	if r.JSON200 == nil || r.JSON200.Key == nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: no key in response: %s", r.Body)
	}
	k := r.JSON200.Key
	return SSHKey{
		ID:        int64(deref(k.ID)),
		UserID:    int64(deref(k.UserID)),
		PublicKey: cmp.Or(deref(k.PublicKey), publicKey),
		CreatedAt: deref(k.CreatedAt),
	}, nil
}

// ListSSHKeys returns every key registered on the account.
func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	r, err := c.GetSSHKeysUserWithResponse(ctx, &GetSSHKeysUserParams{Authorization: c.bearer})
	switch err := check(r, err); {
	case errors.Is(err, ErrNotFound):
		return nil, nil // the API reports "no keys" as 404
	case err != nil:
		return nil, fmt.Errorf("listing ssh keys: %w", err)
	case r.JSON200 == nil:
		return nil, fmt.Errorf("listing ssh keys: unexpected response: %s", r.Body)
	}
	keys := make([]SSHKey, 0, len(*r.JSON200))
	for _, k := range *r.JSON200 {
		keys = append(keys, SSHKey{
			ID:        int64(deref(k.ID)),
			UserID:    int64(deref(k.UserID)),
			PublicKey: deref(k.Key),
			CreatedAt: deref(k.CreatedAt),
		})
	}
	return keys, nil
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

// deref returns the value p points to, or the zero value when p is nil.
func deref[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return v
}
