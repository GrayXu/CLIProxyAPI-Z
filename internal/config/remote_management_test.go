package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigOptional_IssueAPIKeyPasswordHashing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte(`
remote-management:
  allow-remote: true
  issue-api-key-password: issue-me
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if got := strings.TrimSpace(cfg.RemoteManagement.IssueAPIKeyPassword); !strings.HasPrefix(got, "$2") {
		t.Fatalf("issue-api-key-password not hashed, got %q", got)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "issue-me") {
		t.Fatalf("config file still contains plaintext issue password: %s", string(raw))
	}
}
