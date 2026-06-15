package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestIsLoopbackURL(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "http://127.0.0.1:4600", want: true},
		{raw: "http://localhost:4600", want: true},
		{raw: "http://[::1]:4600", want: true},
		{raw: "https://beacon.example", want: false},
		{raw: "not-a-url", want: false},
	}
	for _, tt := range tests {
		if got := IsLoopbackURL(tt.raw); got != tt.want {
			t.Fatalf("IsLoopbackURL(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestDefaultConfigPathUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, want := DefaultConfigPath(), filepath.Join(home, ".beacon", "beacon.toml"); got != want {
		t.Fatalf("DefaultConfigPath = %q, want %q", got, want)
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

func TestPlanGuidedConfigPatchUsesDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{FleetRole: FleetRoleBoth})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if got, want := plan.Path, filepath.Join(home, ".beacon", "beacon.toml"); got != want {
		t.Fatalf("plan.Path = %q, want %q", got, want)
	}
}

func TestGuidedConfigPlanNilAndNoopBehavior(t *testing.T) {
	var nilPlan *GuidedConfigPlan
	if got := nilPlan.Content(); got != nil {
		t.Fatalf("nil Content = %#v, want nil", got)
	}
	if err := nilPlan.Write(); err == nil || !strings.Contains(err.Error(), "config plan is nil") {
		t.Fatalf("nil Write error = %v, want nil-plan error", err)
	}

	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := "[fleet]\nrole = \"both\"\nnode_name = \"local\"\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{Path: path, FleetRole: FleetRoleBoth})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if plan.Changed {
		t.Fatalf("plan.Changed = true, want false")
	}
	if err := plan.Write(); err != nil {
		t.Fatalf("noop Write returned error: %v", err)
	}
	if plan.BackupPath != "" {
		t.Fatalf("noop BackupPath = %q, want empty", plan.BackupPath)
	}
}

func TestPlanGuidedConfigPatchRejectsInvalidInputs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	tests := []struct {
		name    string
		prepare func(t *testing.T) string
		patch   GuidedConfigPatch
		wantErr string
	}{
		{
			name: "config path is directory",
			prepare: func(t *testing.T) string {
				return dir
			},
			wantErr: "is a directory",
		},
		{
			name: "invalid existing toml",
			prepare: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "beacon.toml")
				if err := os.WriteFile(path, []byte("[fleet]\nrole = "), 0600); err != nil {
					t.Fatalf("write invalid config: %v", err)
				}
				return path
			},
			wantErr: "load existing config",
		},
		{
			name: "invalid role",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "beacon.toml")
			},
			patch:   GuidedConfigPatch{FleetRole: "enterprise"},
			wantErr: "fleet.role must be one of",
		},
		{
			name: "empty normalized name",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "beacon.toml")
			},
			patch:   GuidedConfigPatch{Name: "\n\t"},
			wantErr: "name is empty",
		},
		{
			name: "long name",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "beacon.toml")
			},
			patch:   GuidedConfigPatch{Name: strings.Repeat("x", DashboardNameMaxLength+1)},
			wantErr: "name must be <=",
		},
		{
			name: "invalid public url",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "beacon.toml")
			},
			patch:   GuidedConfigPatch{PublicURL: "http://beacon.example"},
			wantErr: "server.public_url must use https",
		},
		{
			name: "invalid control plane url",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "beacon.toml")
			},
			patch:   GuidedConfigPatch{ControlPlaneURL: "https://beacon.example/base"},
			wantErr: "fleet.control_plane_url must be a root URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.prepare(t)
			patch := tt.patch
			patch.Path = path
			_, err := PlanGuidedConfigPatch(patch)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("PlanGuidedConfigPatch error = %v, want %q", err, tt.wantErr)
			}
		})
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

func TestNextBackupPathSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beacon.toml")
	now := time.Date(2026, 6, 15, 16, 0, 0, 0, time.UTC)
	firstBackup, err := nextBackupPath(path, now)
	if err != nil {
		t.Fatalf("nextBackupPath first: %v", err)
	}
	if err := os.WriteFile(firstBackup, []byte("occupied"), 0600); err != nil {
		t.Fatalf("write occupied backup: %v", err)
	}
	secondBackup, err := nextBackupPath(path, now)
	if err != nil {
		t.Fatalf("nextBackupPath second: %v", err)
	}
	if secondBackup == firstBackup || !strings.HasSuffix(secondBackup, ".1") {
		t.Fatalf("secondBackup = %q, want first numeric suffix after %q", secondBackup, firstBackup)
	}
}

func TestPlanGuidedConfigPatchUpdatesQuotedKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	original := `
[server]
"public_url" = "https://old.example"

[fleet]
'role' = "collector"
node_name = "old"
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:      path,
		FleetRole: FleetRoleBoth,
		PublicURL: "https://beacon.example",
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if strings.Contains(content, `"public_url"`) || strings.Contains(content, `'role'`) {
		t.Fatalf("planned config kept quoted duplicate-prone keys:\n%s", content)
	}
	if strings.Count(content, "public_url") != 1 || strings.Count(content, "role") != 1 {
		t.Fatalf("planned config duplicated semantic keys:\n%s", content)
	}
	if err := plan.Write(); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load written config: %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" || cfg.Fleet.Role != FleetRoleBoth {
		t.Fatalf("config values = public_url %q role %q", cfg.Server.PublicURL, cfg.Fleet.Role)
	}
}

func TestPlanGuidedConfigPatchUpdatesQuotedTables(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	original := `
["server"]
public_url = "https://old.example"

['fleet']
role = "collector"
node_name = "old"
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:      path,
		FleetRole: FleetRoleBoth,
		PublicURL: "https://beacon.example",
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if strings.Contains(content, "[server]") || strings.Contains(content, "[fleet]") {
		t.Fatalf("planned config appended duplicate bracketed section:\n%s", content)
	}
	if err := plan.Write(); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load written config: %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" || cfg.Fleet.Role != FleetRoleBoth {
		t.Fatalf("config values = public_url %q role %q", cfg.Server.PublicURL, cfg.Fleet.Role)
	}
}

func TestPlanGuidedConfigPatchUpdatesDottedKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	original := `
server.public_url = "https://old.example"
fleet.role = "collector"
fleet.node_name = "old"
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:      path,
		FleetRole: FleetRoleBoth,
		PublicURL: "https://beacon.example",
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if strings.Contains(content, "[server]") || strings.Contains(content, "[fleet]") {
		t.Fatalf("planned config appended bracketed section instead of updating dotted keys:\n%s", content)
	}
	if strings.Count(content, "server.public_url") != 1 || strings.Count(content, "fleet.role") != 1 {
		t.Fatalf("planned config duplicated dotted keys:\n%s", content)
	}
	if err := plan.Write(); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load written config: %v", err)
	}
	if cfg.Server.PublicURL != "https://beacon.example" || cfg.Fleet.Role != FleetRoleBoth {
		t.Fatalf("config values = public_url %q role %q", cfg.Server.PublicURL, cfg.Fleet.Role)
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

func TestPlanGuidedConfigPatchTreatsEmptySourcesAsUserOwned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := `
[capture]
sources = []
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write original config: %v", err)
	}
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:                path,
		FleetRole:           FleetRoleBoth,
		ApplyDefaultSources: true,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	content := string(plan.Content())
	if strings.Contains(content, "[[capture.sources]]") {
		t.Fatalf("planned config appended source tables despite existing sources key:\n%s", content)
	}
	if strings.Count(content, "sources = []") != 1 {
		t.Fatalf("planned config did not preserve existing empty sources key:\n%s", content)
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

func TestPlanGuidedConfigPatchCaptureEnabledNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "beacon.toml")
	body := `
[capture]
enabled = false

[fleet]
role = "control-plane"
node_name = "Control Plane"
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	enabled := false
	plan, err := PlanGuidedConfigPatch(GuidedConfigPatch{
		Path:           path,
		FleetRole:      FleetRoleControlPlane,
		CaptureEnabled: &enabled,
	})
	if err != nil {
		t.Fatalf("PlanGuidedConfigPatch returned error: %v", err)
	}
	if plan.Changed {
		t.Fatalf("plan.Changed = true, want false: %#v", plan.Changes)
	}
}

func TestTOMLKeyHelpers(t *testing.T) {
	if got := newTOMLPatchDocument(nil).bytes(); got != nil {
		t.Fatalf("empty document bytes = %#v, want nil", got)
	}
	tableTests := []struct {
		line      string
		name      string
		array     bool
		wantMatch bool
	}{
		{line: "[server]", name: "server", wantMatch: true},
		{line: "['capture'.sources]", name: "capture.sources", wantMatch: true},
		{line: "[[\"capture\".sources]]", name: "capture.sources", array: true, wantMatch: true},
		{line: "[broken", wantMatch: false},
		{line: "[']", wantMatch: false},
		{line: "not a table", wantMatch: false},
	}
	for _, tt := range tableTests {
		name, array, ok := tableLineName(tt.line)
		if ok != tt.wantMatch || name != tt.name || array != tt.array {
			t.Fatalf("tableLineName(%q) = (%q, %v, %v), want (%q, %v, %v)", tt.line, name, array, ok, tt.name, tt.array, tt.wantMatch)
		}
	}

	for _, line := range []string{"", "# comment", "name \"missing equals\""} {
		if keyLineMatches(line, "name") {
			t.Fatalf("keyLineMatches(%q) = true, want false", line)
		}
	}
	if !keyLineMatches(`"public_url" = "https://beacon.example"`, "public_url") {
		t.Fatal("keyLineMatches quoted key = false, want true")
	}
	if tomlKeyValueDelimiterIndex(`"not=delimiter" = true`) < 0 {
		t.Fatal("tomlKeyValueDelimiterIndex did not ignore equals inside quotes")
	}
	if !tomlKeyPathMatches(`"fleet".'role'`, "fleet", "role") {
		t.Fatal("tomlKeyPathMatches quoted dotted key = false, want true")
	}
	if tomlKeyPathMatches("fleet", "fleet", "role") {
		t.Fatal("tomlKeyPathMatches length mismatch = true, want false")
	}
	if _, ok := splitTOMLKeyPath(`"unterminated`); ok {
		t.Fatal("splitTOMLKeyPath malformed quoted key ok = true, want false")
	}
	if got := splitTOMLKeySegments(`"a.b".c`); !reflect.DeepEqual(got, []string{`"a.b"`, "c"}) {
		t.Fatalf("splitTOMLKeySegments = %#v", got)
	}
}

func changeFields(changes []GuidedConfigChange) []string {
	fields := make([]string, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, change.Field)
	}
	return fields
}
