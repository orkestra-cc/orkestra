package services

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestPIIProducerDeclaresItsKey(t *testing.T) {
	p := NewPIIProducer(PIIProducerKeyClient, nil, nil, nil, nil, nil, nil, "", "", "", "")
	k, ok := p.(iface.PIIProducerKeyer)
	if !ok || k.ProducerKey() != "auth.client" {
		t.Fatalf("ProducerKey = %v, %v; want auth.client", k, ok)
	}
	if p.Subject() != "auth" {
		t.Fatalf("Subject must stay %q (audit rows and export bundles reference it)", "auth")
	}
}
