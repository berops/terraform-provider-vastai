package vastai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const poolingTime = 10 * time.Second

// VastAiClient is a minimal Vast.ai REST API client.
type VastAiClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// New creates a client for the given base URL (e.g. "https://console.vast.ai/api/v0")
// authenticated with the given API key.
func NewVastAiClient(apiKey, baseURL string) *VastAiClient {
	return &VastAiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// APIError is returned when the API responds with a non-2xx status
// or a {"success": false} body.
type APIError struct {
	StatusCode int
	Code       string // machine-readable "error" field, e.g. "no_ssh_key"; may be empty
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" && e.Code != e.Message {
		return fmt.Sprintf("vast.ai api error (status %d): %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("vast.ai api error (status %d): %s", e.StatusCode, e.Message)
}

// IsCode reports whether err is an APIError carrying the given error code.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// Do sends a request to path (relative to baseURL) with an optional JSON body
// and decodes the JSON response into result (may be nil).
func (c *VastAiClient) Do(ctx context.Context, method, path string, body, result any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/"+strings.TrimLeft(path, "/"), reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		code, msg := errorFields(data)
		return &APIError{StatusCode: resp.StatusCode, Code: code, Message: msg}
	}

	// Vast.ai often returns HTTP 200 with {"success": false, "msg": "..."}.
	var env struct {
		Success *bool `json:"success"`
	}
	if json.Unmarshal(data, &env) == nil && env.Success != nil && !*env.Success {
		code, msg := errorFields(data)
		return &APIError{StatusCode: resp.StatusCode, Code: code, Message: msg}
	}

	if result != nil && len(data) > 0 {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

func (c *VastAiClient) Get(ctx context.Context, path string, result any) error {
	return c.Do(ctx, http.MethodGet, path, nil, result)
}

func (c *VastAiClient) Post(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, http.MethodPost, path, body, result)
}

func (c *VastAiClient) Put(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, http.MethodPut, path, body, result)
}

func (c *VastAiClient) Delete(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, http.MethodDelete, path, body, result)
}

// errorFields pulls the "error" code and "msg" text out of a JSON error body.
// The message falls back to the code, then to the raw body, so it is never empty.
func errorFields(data []byte) (code, msg string) {
	var m map[string]any
	if json.Unmarshal(data, &m) == nil {
		code, _ = m["error"].(string)
		msg, _ = m["msg"].(string)
		if msg == "" {
			// 429 bodies use "detail".
			msg, _ = m["detail"].(string)
		}
	}
	if msg == "" {
		msg = code
	}
	if msg == "" {
		msg = string(data)
	}
	return code, msg
}

func (c *VastAiClient) CreateInstance(ctx context.Context, id int, reqBody CreateInstanceRequest) (CreateInstanceResponse, error) {
	path := fmt.Sprintf("/api/v0/asks/%d/", id)
	resp := CreateInstanceResponse{}
	reqBody.ClientID = "me"

	if err := c.Put(ctx, path, reqBody, &resp); err != nil {
		return CreateInstanceResponse{}, fmt.Errorf("creating instance from offer %d: %w", id, err)
	}

	return resp, nil
}

func (c *VastAiClient) ShowInstance(ctx context.Context, id int64) (Instance, error) {
	path := fmt.Sprintf("/api/v0/instances/%d/", id)
	resp := ShowInstanceResponse{}

	if err := c.Get(ctx, path, &resp); err != nil {
		return Instance{}, fmt.Errorf("showing instance %d: %w", id, err)
	}

	return resp.Instances, nil
}

// DestroyInstance permanently destroys an instance and all its data. An
// instance that no longer exists is treated as already destroyed.
func (c *VastAiClient) DestroyInstance(ctx context.Context, id int64) error {
	path := fmt.Sprintf("/api/v0/instances/%d/", id)
	resp := DestroyInstanceResponse{}

	// The API expects a JSON body even though it carries nothing.
	if err := c.Delete(ctx, path, struct{}{}, &resp); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("destroying instance %d: %w", id, err)
	}

	return nil
}

// CreateSSHKey registers a public key on the account. The API also adds it
// to every instance the account currently owns.
func (c *VastAiClient) CreateSSHKey(ctx context.Context, publicKey string) (SSHKey, error) {
	resp := CreateSSHKeyResponse{}

	if err := c.Post(ctx, "/api/v0/ssh/", CreateSSHKeyRequest{SSHKey: publicKey}, &resp); err != nil {
		return SSHKey{}, fmt.Errorf("creating ssh key: %w", err)
	}

	return resp.Key, nil
}

// UpdateSSHKey replaces the public key stored under id.
func (c *VastAiClient) UpdateSSHKey(ctx context.Context, id int64, publicKey string) (SSHKey, error) {
	path := fmt.Sprintf("/api/v0/ssh/%d/", id)
	resp := UpdateSSHKeyResponse{}

	if err := c.Put(ctx, path, UpdateSSHKeyRequest{ID: id, SSHKey: publicKey}, &resp); err != nil {
		return SSHKey{}, fmt.Errorf("updating ssh key %d: %w", id, err)
	}

	key := resp.Key.toSSHKey()
	// Fill in what the caller already knows if the response omits it.
	if key.ID == 0 {
		key.ID = id
	}
	if key.PublicKey == "" {
		key.PublicKey = publicKey
	}

	return key, nil
}

// DeleteSSHKey removes a public key from the account. A key that no longer
// exists is treated as already deleted.
func (c *VastAiClient) DeleteSSHKey(ctx context.Context, id int64) error {
	path := fmt.Sprintf("/api/v0/ssh/%d/", id)
	resp := DeleteSSHKeyResponse{}

	if err := c.Delete(ctx, path, nil, &resp); err != nil {
		// A missing key is reported as 400 no_ssh_key, not 404; accept both.
		var apiErr *APIError
		if IsCode(err, "no_ssh_key") || (errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound) {
			return nil
		}
		return fmt.Errorf("deleting ssh key %d: %w", id, err)
	}

	return nil
}

// ListSSHKeys returns every public key registered on the account. An
// account with no keys yields an empty list, not an error.
func (c *VastAiClient) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var items []sshKeyListItem

	if err := c.Get(ctx, "/api/v0/ssh/", &items); err != nil {
		// The API reports "no keys" as 404 rather than an empty array.
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return []SSHKey{}, nil
		}
		return nil, fmt.Errorf("listing ssh keys: %w", err)
	}

	keys := make([]SSHKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.toSSHKey())
	}

	return keys, nil
}

func (c *VastAiClient) WaitForStatus(ctx context.Context, id int64, target string) (Instance, error) {
	ticker := time.NewTicker(poolingTime)
	defer ticker.Stop()

	var last Instance
	for {
		inst, err := c.ShowInstance(ctx, id)
		if err != nil {
			// A throttled tick must not fail an apply that has already rented
			// a machine; wait for the next tick and try again.
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
				return last, err
			}
		} else {
			last = inst

			status := ""
			if inst.ActualStatus != nil {
				status = *inst.ActualStatus
			}

			if status == target {
				return inst, nil
			}

			if isTerminalStatus(status) {
				msg := ""
				if inst.StatusMsg != nil && *inst.StatusMsg != "" {
					msg = ": " + *inst.StatusMsg
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
	case StatusExited, StatusUnknown, StatusOffline:
		return true
	}
	return false
}
