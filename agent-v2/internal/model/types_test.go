package model

import (
	"encoding/json"
	"testing"
)

func TestPeerInfoUnmarshalClearnetCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want *string
	}{
		{name: "string", body: `{"Clearnet":"peer.example.com:51820"}`, want: strPtr("peer.example.com:51820")},
		{name: "null", body: `{"Clearnet":null}`, want: nil},
		{name: "false", body: `{"Clearnet":false}`, want: nil},
		{name: "empty string", body: `{"Clearnet":""}`, want: nil},
		{name: "absent", body: `{}`, want: nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var peer PeerInfo
			if err := json.Unmarshal([]byte(tt.body), &peer); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == nil {
				if peer.Clearnet != nil {
					t.Fatalf("Clearnet = %q, want nil", *peer.Clearnet)
				}
				return
			}
			if peer.Clearnet == nil || *peer.Clearnet != *tt.want {
				t.Fatalf("Clearnet = %v, want %q", peer.Clearnet, *tt.want)
			}
		})
	}
}

func TestPeerInfoUnmarshalClearnetRejectsTrue(t *testing.T) {
	t.Parallel()

	var peer PeerInfo
	if err := json.Unmarshal([]byte(`{"Clearnet":true}`), &peer); err == nil {
		t.Fatal("expected error")
	}
}

func TestPeerInfoUnmarshalPeerPayloadCompatibility(t *testing.T) {
	t.Parallel()

	var peer PeerInfo
	if err := json.Unmarshal([]byte(`{
		"ASN": 4242420774,
		"Port": "23374",
		"MTU": "1420",
		"PresharedKey": null,
		"Clearnet": null
	}`), &peer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if peer.Port != 23374 {
		t.Fatalf("Port = %d, want 23374", peer.Port)
	}
	if peer.MTU != 1420 {
		t.Fatalf("MTU = %d, want 1420", peer.MTU)
	}
	if peer.PresharedKey != "" {
		t.Fatalf("PresharedKey = %q, want empty", peer.PresharedKey)
	}
	if peer.Clearnet != nil {
		t.Fatalf("Clearnet = %q, want nil", *peer.Clearnet)
	}
}

func TestPeerInfoUnmarshalRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	var peer PeerInfo
	if err := json.Unmarshal([]byte(`{"Port":"23374/tcp"}`), &peer); err == nil {
		t.Fatal("expected error")
	}
}

func strPtr(s string) *string { return &s }
