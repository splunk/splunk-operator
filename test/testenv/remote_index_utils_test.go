package testenv

import "testing"

func TestGenerateQueueVolumeSpecHasRequiredFields(t *testing.T) {
	vol := GenerateQueueVolumeSpec("queue-secret-ref-volume", "queue-secret")

	if vol.Name != "queue-secret-ref-volume" {
		t.Fatalf("unexpected volume name: %s", vol.Name)
	}
	if vol.SecretRef != "queue-secret" {
		t.Fatalf("unexpected secretRef: %s", vol.SecretRef)
	}
	if vol.Endpoint == "" {
		t.Fatal("endpoint must not be empty")
	}
	if vol.Path == "" {
		t.Fatal("path must not be empty")
	}
	if vol.Provider == "" {
		t.Fatal("provider must not be empty")
	}
	if vol.Type == "" {
		t.Fatal("storage type must not be empty")
	}
	if vol.Region == "" {
		t.Fatal("region must not be empty for aws provider")
	}
}
