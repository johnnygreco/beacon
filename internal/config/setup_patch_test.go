package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeRootURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "https root", raw: " https://beacon.example/ ", want: "https://beacon.example"},
		{name: "https port", raw: "https://beacon.example:9443", want: "https://beacon.example:9443"},
		{name: "loopback http localhost", raw: "http://localhost:4600/", want: "http://localhost:4600"},
		{name: "loopback http ipv4", raw: "http://127.0.0.1:4600", want: "http://127.0.0.1:4600"},
		{name: "loopback http ipv6", raw: "http://[::1]:4600", want: "http://[::1]:4600"},
		{name: "missing scheme", raw: "beacon.example", wantErr: "absolute URL"},
		{name: "bad scheme", raw: "ssh://beacon.example", wantErr: "must use http or https"},
		{name: "userinfo", raw: "https://user:pass@beacon.example", wantErr: "must not include userinfo"},
		{name: "query", raw: "https://beacon.example?x=1", wantErr: "must not include a query string"},
		{name: "fragment", raw: "https://beacon.example#x", wantErr: "must not include a fragment"},
		{name: "path", raw: "https://beacon.example/base", wantErr: "root URL without a path"},
		{name: "non loopback http", raw: "http://beacon.example", wantErr: "https for non-loopback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeRootURL(tt.raw, "test URL")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NormalizeRootURL error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeRootURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeRootURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlanGuidedConfigPatchCreatesMinimalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")

	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:                path,
		FleetRole:           FleetRoleBoth,
		Name:                " Dashboard Node ",
		PublicURL:           "https://beacon.example/",
		ApplyDefaultSources: true,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if plan.Exists {
		t.Fatal("plan.Exists = true, want false")
	}
	if !plan.Changed {
		t.Fatal("plan.Changed = false, want true")
	}
	wantFields := []string{
		"fleet.role",
		"dashboard.name",
		"fleet.node_name",
		"server.public_url",
		"capture.sources",
	}
	if got := changeFields(plan.Changes); !reflect.DeepEqual(got, wantFields) {
		t.Fatalf("change fields = %#v, want %#v", got, wantFields)
	}
	content := string(plan.Content())
	for _, want := range []string{
		"[fleet]",
		`role = "both"`,
		`node_name = "Dashboard Node"`,
		"[dashboard]",
		`name = "Dashboard Node"`,
		"[server]",
		`public_url = "https://beacon.example"`,
		"[[capture.sources]]",
		`name = "claude"`,
		`name = "pi"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("planned config missing %q:\n%s", want, content)
		}
	}
	for _, notWant := range []string{"[database]", "[mcp]", "[search]"} {
		if strings.Contains(content, notWant) {
			t.Fatalf("planned config contains unrelated default %q:\n%s", notWant, content)
		}
	}

	if err := plan.Write(); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if plan.BackupPath != "" {
		t.Fatalf("BackupPath = %q, want empty for new file", plan.BackupPath)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(written config): %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" {
		t.Fatalf("Server.PublicURL = %q", cfg.Server.PublicURL)
	}
	if cfg.Fleet.ControlPlaneURL != "" {
		t.Fatalf("Fleet.ControlPlaneURL = %q, want empty", cfg.Fleet.ControlPlaneURL)
	}
	if cfg.Dashboard.Name != "Dashboard Node" || cfg.Fleet.NodeName != "Dashboard Node" {
		t.Fatalf("names = dashboard %q fleet %q", cfg.Dashboard.Name, cfg.Fleet.NodeName)
	}
	if len(cfg.Capture.Sources) != len(DefaultCaptureSources()) {
		t.Fatalf("sources = %d, want defaults", len(cfg.Capture.Sources))
	}

	second, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:                path,
		FleetRole:           FleetRoleBoth,
		Name:                "Dashboard Node",
		PublicURL:           "https://beacon.example",
		ApplyDefaultSources: true,
	})
	if err != nil {
		t.Fatalf("second PlanGuidedConfigPatch returned error: %v", err)
	}
	if second.Changed {
		t.Fatalf("second plan changed = true, changes %#v", second.Changes)
	}
}

func TestPlanGuidedConfigPatchPreservesExistingConfigAndWritesBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "beacon.toml")
	original := `# keep this comment
[server]
host = "127.0.0.1"
port = 7777

[database]
addrs = ["clickhouse.internal:9440"]
database = "beacon_custom"
read_pool_size = 12

[fleet]
role = "both"
node_name = "old name"
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write original config: %v", err)
	}

	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:      path,
		Name:      "New Name",
		PublicURL: "https://beacon.example",
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if !plan.Exists || !plan.Changed {
		t.Fatalf("plan exists/changed = %v/%v, want true/true", plan.Exists, plan.Changed)
	}
	if err := plan.Write(); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if plan.BackupPath == "" {
		t.Fatal("BackupPath is empty, want backup for existing config")
	}
	backup, err := os.ReadFile(plan.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Fatalf("backup = %q, want original", string(backup))
	}
	writtenBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	written := string(writtenBytes)
	for _, want := range []string{
		"# keep this comment",
		`host = "127.0.0.1"`,
		`port = 7777`,
		`database = "beacon_custom"`,
		`node_name = "New Name"`,
		`public_url = "https://beacon.example"`,
	} {
		if !strings.Contains(written, want) {
			t.Fatalf("written config missing %q:\n%s", want, written)
		}
	}
}

func TestPlanGuidedConfigPatchDryRunDoesNotWrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	original := "[fleet]\nrole = \"collector\"\nnode_name = \"collector\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:      path,
		FleetRole: FleetRoleBoth,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if !plan.Changed {
		t.Fatal("plan.Changed = false, want true")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Fatalf("config changed before Write: %q", string(data))
	}
}

func TestPlanGuidedConfigPatchPreservesExistingSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := `
[capture]
enabled = true

[[capture.sources]]
name = "custom"
runtime = "codex"
provider = "openai"
glob = "/tmp/custom/**/*.jsonl"
watch_root = "/tmp/custom"
format = "jsonl"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:                path,
		FleetRole:           FleetRoleCollector,
		ControlPlaneURL:     "https://beacon.example",
		ApplyDefaultSources: true,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if strings.Contains(content, `name = "claude"`) {
		t.Fatalf("planned config added default sources despite existing source:\n%s", content)
	}
	if strings.Count(content, "[[capture.sources]]") != 1 {
		t.Fatalf("capture source table count = %d, want 1:\n%s", strings.Count(content, "[[capture.sources]]"), content)
	}
}

func TestPlanGuidedConfigPatchCaptureEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	enabled := false
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:           filepath.Join(t.TempDir(), "beacon.toml"),
		FleetRole:      FleetRoleControlPlane,
		Name:           "Control Plane",
		CaptureEnabled: &enabled,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if !strings.Contains(content, "[capture]") || !strings.Contains(content, "enabled = false") {
		t.Fatalf("planned config missing capture.enabled=false:\n%s", content)
	}
}

func changeFields(changes []GuidedConfigChange) []string {
	fields := make([]string, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, change.Field)
	}
	return fields
}
