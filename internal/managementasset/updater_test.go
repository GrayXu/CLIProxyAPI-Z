package managementasset

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
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

func TestAutoUpdateSkipReason(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantReason string
		wantSkip   bool
	}{
		{
			name:       "nil config",
			cfg:        nil,
			wantReason: "config not yet available",
			wantSkip:   true,
		},
		{
			name: "cluster mode",
			cfg: &config.Config{
				Home: config.HomeConfig{Enabled: true},
			},
			wantReason: "cluster mode enabled",
			wantSkip:   true,
		},
		{
			name: "control panel disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableControlPanel: true},
			},
			wantReason: "control panel disabled",
			wantSkip:   true,
		},
		{
			name: "auto update disabled",
			cfg: &config.Config{
				RemoteManagement: config.RemoteManagement{DisableAutoUpdatePanel: true},
			},
			wantReason: "disable-auto-update-panel is enabled",
			wantSkip:   true,
		},
		{
			name:       "enabled",
			cfg:        &config.Config{},
			wantReason: "",
			wantSkip:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReason, gotSkip := autoUpdateSkipReason(tt.cfg)
			if gotReason != tt.wantReason || gotSkip != tt.wantSkip {
				t.Fatalf("autoUpdateSkipReason() = (%q, %t), want (%q, %t)", gotReason, gotSkip, tt.wantReason, tt.wantSkip)
			}
		})
	}
}
