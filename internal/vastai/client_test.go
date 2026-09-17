package vastai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUnixTimeUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		{"epoch seconds", `1758116400`, time.Unix(1758116400, 0).UTC()},
		{"fractional epoch seconds", `1758116400.5`, time.Unix(1758116400, 500_000_000).UTC()},
		{"rfc3339 string", `"2025-09-17T14:20:00Z"`, time.Date(2025, 9, 17, 14, 20, 0, 0, time.UTC)},
		{"null", `null`, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got unixTime
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.in, err)
			}
			if !got.Time.Equal(tc.want) {
				t.Fatalf("unmarshal %s = %v, want %v", tc.in, got.Time, tc.want)
			}
		})
	}

	var got unixTime
	if err := json.Unmarshal([]byte(`"yesterday"`), &got); err == nil {
		t.Fatal("expected an error for a non-timestamp string")
	}
}

// newTestServer serves canned responses for the SSH key endpoints in the
// shape the live API produces: created_at as epoch seconds, not a string.
func newTestServer(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/ssh", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"key":{"id":42,"user_id":7,"public_key":"ssh-ed25519 AAAA test","created_at":1758116400.25,"deleted_at":null}}`))
	})
	mux.HandleFunc("GET /api/v0/ssh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":42,"user_id":7,"key":"ssh-ed25519 AAAA test","created_at":1758116400,"deleted_at":null}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New("test-key", srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestCreateSSHKeyDecodesEpochTimestamps(t *testing.T) {
	c := newTestServer(t)
	key, err := c.CreateSSHKey(context.Background(), "ssh-ed25519 AAAA test")
	if err != nil {
		t.Fatalf("CreateSSHKey: %v", err)
	}
	want := SSHKey{
		ID:        42,
		UserID:    7,
		PublicKey: "ssh-ed25519 AAAA test",
		CreatedAt: time.Unix(1758116400, 250_000_000).UTC(),
	}
	if key != want {
		t.Fatalf("CreateSSHKey = %+v, want %+v", key, want)
	}
}

func TestListSSHKeysDecodesEpochTimestamps(t *testing.T) {
	c := newTestServer(t)
	keys, err := c.ListSSHKeys(context.Background())
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	want := []SSHKey{{
		ID:        42,
		UserID:    7,
		PublicKey: "ssh-ed25519 AAAA test",
		CreatedAt: time.Unix(1758116400, 0).UTC(),
	}}
	if len(keys) != 1 || keys[0] != want[0] {
		t.Fatalf("ListSSHKeys = %+v, want %+v", keys, want)
	}
}
