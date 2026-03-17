package managementasset

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLatestManagementHTMLPrefersExistingLocalFile(t *testing.T) {
	t.Parallel()

	staticDir := t.TempDir()
	localPath := filepath.Join(staticDir, ManagementFileName)
	const localHTML = "<html><body>local</body></html>"
	if err := os.WriteFile(localPath, []byte(localHTML), 0o644); err != nil {
		t.Fatalf("write local management asset: %v", err)
	}

	if ok := EnsureLatestManagementHTML(context.Background(), staticDir, "", "://invalid"); !ok {
		t.Fatal("expected existing local management asset to be used")
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local management asset: %v", err)
	}
	if string(got) != localHTML {
		t.Fatalf("expected local management asset to remain unchanged, got %q", string(got))
	}
}

func TestEnsureLatestManagementHTMLFallsBackToBundledAsset(t *testing.T) {
	t.Parallel()

	staticDir := t.TempDir()
	localPath := filepath.Join(staticDir, ManagementFileName)
	if ok := EnsureLatestManagementHTML(context.Background(), staticDir, "", "://invalid"); !ok {
		t.Fatal("expected bundled management asset to be written")
	}

	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read bundled management asset: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected bundled management asset to be non-empty")
	}
	if string(got) != string(bundledManagementHTML) {
		t.Fatal("expected bundled management asset contents to match embedded HTML")
	}
}
