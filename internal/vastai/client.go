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
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	IntendedStatusRunning = "running"
	IntendedStatusStopped = "stopped"
	IntendedStatusGone    = "gone"
)

const (
	ActualStatusRunning = "running"
	ActualStatusExited  = "exited"
	ActualStatusUnknown = "unknown"
	ActualStatusOffline = "offline"
)

var actualStatusFor = map[string]string{
	IntendedStatusRunning: ActualStatusRunning,
	IntendedStatusStopped: ActualStatusExited,
}

const pollInterval = 10 * time.Second

var ErrNotFound = errors.New("not found")

type Client struct {
	*ClientWithResponses
	bearer string
}

func New(apiKey, baseURL string) (*Client, error) {
	bearer := "Bearer " + apiKey
	auth := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", bearer)
		req.Header.Set("Accept", "application/json")
		return nil
	}
	rc := retryablehttp.NewClient()
	rc.Logger = nil
	rc.RetryMax = 4
	rc.RetryWaitMin = time.Second
	rc.RetryWaitMax = 30 * time.Second
	rc.HTTPClient.Timeout = 60 * time.Second // per attempt
	rc.CheckRetry = func(_ context.Context, resp *http.Response, err error) (bool, error) {
		return err == nil && resp.StatusCode == http.StatusTooManyRequests, nil
	}
	rc.Backoff = rateLimitBackoff
	// Hand the last 429 back to the caller instead of swallowing it, so
	// check() can surface the API's own "requests too frequent" message.
	rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
	c, err := NewClientWithResponses(baseURL,
		WithHTTPClient(rc.StandardClient()),
		WithRequestEditorFn(auth),
	)
	if err != nil {
		return nil, err
	}
	return &Client{ClientWithResponses: c, bearer: bearer}, nil
}

// rateLimitBackoff spaces out retries after a 429. vast.ai enforces a minimum
// interval between calls to the same endpoint and answers a too-early call
// with "Retry-After: 0", which the library's default backoff takes literally,
// burning every retry within the same sub-second window. We always wait at
// least minWait*2^attempt (1s, 2s, 4s, 8s) and only let Retry-After lengthen that
// wait, never shorten it.
func rateLimitBackoff(minWait, maxWait time.Duration, attemptNum int, resp *http.Response) time.Duration {
	wait := minWait * time.Duration(1<<uint(attemptNum))
	if resp != nil {
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = max(wait, time.Duration(secs)*time.Second)
		}
	}
	return min(wait, maxWait)
}

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

type response interface {
	StatusCode() int
	GetBody() []byte
}

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

func (c *Client) ShowInstance(ctx context.Context, id int64) (*Instance, error) {
	r, err := c.ShowInstanceWithResponse(ctx, int(id))
	if err := check(r, err); err != nil {
		return nil, fmt.Errorf("showing instance %d: %w", id, err)
	}
	if r.JSON200 == nil || r.JSON200.Instances == nil || r.JSON200.Instances.ID == nil {
		return nil, fmt.Errorf("showing instance %d: %w", id, ErrNotFound)
	}
	return r.JSON200.Instances, nil
}

func (c *Client) DestroyInstance(ctx context.Context, id int64) error {
	r, err := c.DestroyInstanceWithResponse(ctx, int(id), withJSONBody("{}"))
	if err := check(r, err); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("destroying instance %d: %w", id, err)
	}
	return nil
}

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

func (c *Client) WaitForIntendedStatus(ctx context.Context, id int64, intendedStatus string, terminalActualStatuses []string) (*Instance, error) {
	if intendedStatus == "" {
		intendedStatus = IntendedStatusRunning
	}
	wantActualStatus, ok := actualStatusFor[intendedStatus]
	if !ok && intendedStatus != IntendedStatusGone {
		return nil, fmt.Errorf("waiting for instance %d: unknown intended status %q", id, intendedStatus)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var last *Instance
	for {
		inst, err := c.ShowInstance(ctx, id)
		switch {
		case hasStatus(err, http.StatusTooManyRequests):
		case errors.Is(err, ErrNotFound) && intendedStatus == IntendedStatusGone:
			return last, nil
		case err != nil:
			return last, err
		case intendedStatus == IntendedStatusGone:
			last = inst
		default:
			last = inst
			actualStatus := inst.ActualStatus.GetOrEmpty()
			if actualStatus == wantActualStatus {
				return inst, nil
			}
			if slices.Contains(terminalActualStatuses, actualStatus) {
				msg := inst.StatusMsg.GetOrEmpty()
				if msg != "" {
					msg = ": " + msg
				}
				return inst, fmt.Errorf("instance %d entered actual status %q and will not reach %q%s", id, actualStatus, wantActualStatus, msg)
			}
		}

		select {
		case <-ctx.Done():
			return last, fmt.Errorf("waiting for instance %d to reach intended status %q: %w", id, intendedStatus, ctx.Err())
		case <-ticker.C:
		}
	}
}

type SSHKey struct {
	ID        int64
	UserID    int64
	PublicKey string
	CreatedAt time.Time
}

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

func (c *Client) CreateSSHKey(ctx context.Context, publicKey string) (SSHKey, error) {
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

func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	r, err := readResponse(c.GetSSHKeysUser(ctx, &GetSSHKeysUserParams{Authorization: c.bearer}))
	switch err := check(r, err); {
	case errors.Is(err, ErrNotFound):
		return nil, nil
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

type rawResponse struct {
	status int
	body   []byte
}

func (r rawResponse) StatusCode() int { return r.status }
func (r rawResponse) GetBody() []byte { return r.body }

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

func (c *Client) UpdateSSHKey(ctx context.Context, id int64, publicKey string) error {
	r, err := c.UpdateSSHKeyWithResponse(ctx, int(id), UpdateSSHKeyJSONRequestBody{SSHKey: publicKey})
	if err := check(r, err); err != nil {
		return fmt.Errorf("updating ssh key %d: %w", id, err)
	}
	return nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id int64) error {
	r, err := c.DeleteSSHKeyWithResponse(ctx, id)
	if err := check(r, err); err != nil && !errors.Is(err, ErrNotFound) && !hasCode(err, "no_ssh_key") {
		return fmt.Errorf("deleting ssh key %d: %w", id, err)
	}
	return nil
}
