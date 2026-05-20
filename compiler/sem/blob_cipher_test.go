package sem

import (
	"testing"

	"github.com/ryanwible/wrela3/compiler/diag"
)

func TestDevelopmentBlobCipherRequiresOptIn(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.bad_blob_cipher

use { BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH, BlobCipherPolicy } from storage.blob

data BlobCipherConsumer {
    policy: BlobCipherPolicy
}

executor BlobCipherExecutor {
    consumer: BlobCipherConsumer

    start fn main(self) -> never {
        let policy = BlobCipherPolicy(
            mode = BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH,
            key_id = 0,
            nonce_low = 0,
            nonce_high = 0,
            development_opt_in = false
        )
        while true {}
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0123) {
		t.Fatalf("diagnostics = %#v, want SEM0123", ds)
	}
}

func TestBlobCipherDynamicModeRequiresOptIn(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.dynamic_blob_cipher

use { BlobCipherPolicy } from storage.blob

executor BlobCipherExecutor {
    mode: U64

    start fn main(self) -> never {
        let policy = BlobCipherPolicy(
            mode = self.mode,
            key_id = 0,
            nonce_low = 0,
            nonce_high = 0,
            development_opt_in = false
        )
        while true {}
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0123) {
		t.Fatalf("diagnostics = %#v, want SEM0123", ds)
	}
}

func TestEventCannotReferenceUnpublishedBlob(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.bad_unpublished_blob

use { PublishedBlobRef, UnpublishedBlobRef } from storage.blob

event BlobWritten id 1 {
    blob: UnpublishedBlobRef

    layout 1 current {
        blob: PublishedBlobRef = self.blob
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if !hasCode(ds, diag.SEM0117) {
		t.Fatalf("diagnostics = %#v, want SEM0117", ds)
	}
}

func TestBlobSourcePublicationAndCipherTypesCompile(t *testing.T) {
	modules := parseBlobModules(t, `
module sem.blob_publication_consumer

use {
    BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH,
    BlobCipherPolicy,
    BlobExtentAllocator,
    BlobRef,
    PublishedBlobRef,
    UnpublishedBlobRef
} from storage.blob

data BlobPublicationConsumer {
    allocator: BlobExtentAllocator

    fn write(self) -> UnpublishedBlobRef {
        let policy = BlobCipherPolicy(
            mode = BLOB_CIPHER_DEVELOPMENT_PASSTHROUGH,
            key_id = 0,
            nonce_low = 0,
            nonce_high = 0,
            development_opt_in = true
        )
        return self.allocator.write_blob(blob_id = 7, logical_bytes = 1, policy = policy)
    }

    fn published(self) -> PublishedBlobRef {
        return PublishedBlobRef(ref = BlobRef(blob_id = 7, start_lba = 1, block_count = 1))
    }
}
`)
	index := mustBuildIndexAllowingMissingImage(t, modules)
	_, ds := checkAllowingMissingImage(t, index, modules)
	if len(ds) != 0 {
		t.Fatalf("semantic diagnostics: %#v", ds)
	}
}
