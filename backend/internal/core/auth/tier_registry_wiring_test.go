package auth

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// The push channel resolves iface.SessionActivityReader from the keys the
// module registers (module.go: ServiceAuthService = opBundle.authService,
// ServiceClientAuthService = clBundle.authService). Build both bundles the
// way the module does and register them the same way.
func TestRegisteredAuthServicesAreSessionActivityReaders(t *testing.T) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://test/test"))
	if err != nil {
		t.Fatal(err)
	}
	db := client.Database("test")

	reg := module.NewServiceRegistry()
	for tier, key := range map[audienceTier]module.ServiceKey{
		tierOperator: module.ServiceAuthService,
		tierClient:   module.ServiceClientAuthService,
	} {
		b, err := buildAuthTierBundle(tierBundleDeps{db: db, tier: tier})
		if err != nil {
			t.Fatalf("%s: buildAuthTierBundle: %v", tier, err)
		}
		reg.Register(key, b.authService)
	}
	for _, key := range []module.ServiceKey{module.ServiceAuthService, module.ServiceClientAuthService} {
		if _, ok := module.GetTyped[iface.SessionActivityReader](reg, key); !ok {
			t.Errorf("%s does not resolve as iface.SessionActivityReader", key)
		}
	}
}
