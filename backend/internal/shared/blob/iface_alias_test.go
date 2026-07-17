package blob

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Compile-time: the concrete stores satisfy the SDK interface, and the
// blob aliases point at the iface types.
var _ iface.ObjectStore = (*s3Store)(nil)
var _ iface.ObjectStore = (*CachedStore)(nil)

func TestBlobStoreIsIfaceAlias(t *testing.T) {
	var s Store = (*CachedStore)(nil)
	var _ iface.ObjectStore = s // assignable because Store == iface.ObjectStore
	var _ iface.PresignedPut = PresignedPut{}
}
