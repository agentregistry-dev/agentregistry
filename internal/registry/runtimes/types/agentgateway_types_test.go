package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalListener_OmitemptyNewFields(t *testing.T) {
	listener := LocalListener{
		Name:     "test",
		Protocol: LocalListenerProtocolHTTP,
	}
	data, err := json.Marshal(listener)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{"allowedRoutes", "policies"} {
		if strings.Contains(s, field) {
			t.Errorf("empty LocalListener should omit %q, got %s", field, s)
		}
	}
}

func TestLocalTLSServerConfig_OmitemptyNewFields(t *testing.T) {
	tls := LocalTLSServerConfig{
		Cert: "cert.pem",
		Key:  "key.pem",
	}
	data, err := json.Marshal(tls)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{"mode", "certificateRefs", "options"} {
		if strings.Contains(s, field) {
			t.Errorf("empty LocalTLSServerConfig should omit %q, got %s", field, s)
		}
	}
}

func TestFilterOrPolicy_OmitemptyNewFields(t *testing.T) {
	fp := FilterOrPolicy{}
	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{"trafficAuthorization", "frontendConnect"} {
		if strings.Contains(s, field) {
			t.Errorf("empty FilterOrPolicy should omit %q, got %s", field, s)
		}
	}
}

func TestLocalTLSServerConfig_NewFieldsRoundTrip(t *testing.T) {
	tls := LocalTLSServerConfig{
		Cert: "cert.pem",
		Key:  "key.pem",
		Mode: "Terminate",
		CertificateRefs: []LocalObjectReference{
			{Group: "core", Kind: "Secret", Name: "my-cert", Namespace: "default"},
		},
		Options: map[string]string{"minVersion": "1.3"},
	}
	data, err := json.Marshal(tls)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got LocalTLSServerConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Mode != "Terminate" {
		t.Errorf("Mode = %q, want %q", got.Mode, "Terminate")
	}
	if len(got.CertificateRefs) != 1 || got.CertificateRefs[0].Name != "my-cert" {
		t.Errorf("CertificateRefs round-trip failed: %+v", got.CertificateRefs)
	}
	if got.Options["minVersion"] != "1.3" {
		t.Errorf("Options round-trip failed: %+v", got.Options)
	}
}

func TestLocalListener_NewFieldsRoundTrip(t *testing.T) {
	listener := LocalListener{
		Name:     "https",
		Protocol: LocalListenerProtocolHTTPS,
		AllowedRoutes: &LocalAllowedRoutes{
			Namespaces: []string{"default", "prod"},
			Kinds:      []string{"HTTPRoute"},
		},
		Policies: &FilterOrPolicy{
			MCPAuthorization: &MCPAuthorization{
				Rules: map[string]any{"action": "ALLOW"},
			},
		},
	}
	data, err := json.Marshal(listener)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got LocalListener
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AllowedRoutes == nil {
		t.Fatal("AllowedRoutes should not be nil after round-trip")
	}
	if len(got.AllowedRoutes.Namespaces) != 2 || got.AllowedRoutes.Namespaces[0] != "default" {
		t.Errorf("AllowedRoutes.Namespaces round-trip failed: %+v", got.AllowedRoutes)
	}
	if got.Policies == nil || got.Policies.MCPAuthorization == nil {
		t.Fatal("Policies should not be nil after round-trip")
	}
}

func TestFilterOrPolicy_NewFieldsRoundTrip(t *testing.T) {
	fp := FilterOrPolicy{
		TrafficAuthorization: &TrafficAuthorization{
			Rules: map[string]any{"action": "DENY", "matchExpressions": []any{"request.auth.claims.role == 'admin'"}},
		},
		FrontendConnect: &FrontendConnect{
			Enabled: true,
			Rules:   map[string]any{"action": "ALLOW"},
		},
	}
	data, err := json.Marshal(fp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got FilterOrPolicy
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TrafficAuthorization == nil {
		t.Fatal("TrafficAuthorization should not be nil after round-trip")
	}
	if got.FrontendConnect == nil || !got.FrontendConnect.Enabled {
		t.Fatal("FrontendConnect should be present and enabled after round-trip")
	}
}
