package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type GuidedConfigPatch struct {
	Path                string
	FleetRole           string
	Name                string
	PublicURL           string
	ControlPlaneURL     string
	CaptureEnabled      *bool
	ApplyDefaultSources bool
}

type GuidedConfigChange struct {
	Field string
	Old   string
	New   string
}

type GuidedConfigPlan struct {
	Path       string
	Exists     bool
	Changed    bool
	Changes    []GuidedConfigChange
	BackupPath string

	content  []byte
	original []byte
	mode     os.FileMode
}

func DefaultConfigPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".beacon", "beacon.toml")
	}
	return filepath.Join("$HOME", ".beacon", "beacon.toml")
}

func PlanGuidedConfigPatch(patch GuidedConfigPatch) (*GuidedConfigPlan, error) {
	path := strings.TrimSpace(patch.Path)
	if path == "" {
		path = DefaultConfigPath()
	}
	var existing []byte
	mode := os.FileMode(0600)
	info, statErr := os.Stat(path)
	exists := statErr == nil
	switch {
	case statErr == nil:
		if info.IsDir() {
			return nil, fmt.Errorf("config path %s is a directory", path)
		}
		mode = info.Mode().Perm()
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		existing = data
		if _, err := Load(path); err != nil {
			return nil, fmt.Errorf("load existing config: %w", err)
		}
	case errors.Is(statErr, os.ErrNotExist):
	default:
		return nil, fmt.Errorf("stat config: %w", statErr)
	}

	decoded, err := decodeTOMLMap(existing)
	if err != nil {
		return nil, err
	}
	doc := newTOMLPatchDocument(existing)
	changes := make([]GuidedConfigChange, 0, 6)

	if patch.FleetRole != "" {
		role := strings.TrimSpace(patch.FleetRole)
		switch role {
		case FleetRoleBoth, FleetRoleControlPlane, FleetRoleCollector:
		default:
			return nil, fmt.Errorf("fleet.role must be one of %q, %q, or %q", FleetRoleBoth, FleetRoleControlPlane, FleetRoleCollector)
		}
		changed := addStringFieldChange(&changes, decoded, "fleet", "role", role)
		if changed {
			doc.setKey("fleet", "role", strconv.Quote(role))
		}
	}

	if patch.Name != "" {
		name := normalizeDashboardName(patch.Name)
		if name == "" {
			return nil, fmt.Errorf("name is empty after normalization")
		}
		if len([]rune(name)) > DashboardNameMaxLength {
			return nil, fmt.Errorf("name must be <= %d characters", DashboardNameMaxLength)
		}
		if addStringFieldChange(&changes, decoded, "dashboard", "name", name) {
			doc.setKey("dashboard", "name", strconv.Quote(name))
		}
		if addStringFieldChange(&changes, decoded, "fleet", "node_name", name) {
			doc.setKey("fleet", "node_name", strconv.Quote(name))
		}
	}

	if patch.PublicURL != "" {
		publicURL, err := NormalizeRootURL(patch.PublicURL, "server.public_url")
		if err != nil {
			return nil, err
		}
		if addStringFieldChange(&changes, decoded, "server", "public_url", publicURL) {
			doc.setKey("server", "public_url", strconv.Quote(publicURL))
		}
	}

	if patch.ControlPlaneURL != "" {
		controlPlaneURL, err := NormalizeControlPlaneURL(patch.ControlPlaneURL, "fleet.control_plane_url")
		if err != nil {
			return nil, err
		}
		if addStringFieldChange(&changes, decoded, "fleet", "control_plane_url", controlPlaneURL) {
			doc.setKey("fleet", "control_plane_url", strconv.Quote(controlPlaneURL))
		}
	}

	if patch.CaptureEnabled != nil {
		enabled := *patch.CaptureEnabled
		if addBoolFieldChange(&changes, decoded, "capture", "enabled", enabled) {
			doc.setKey("capture", "enabled", strconv.FormatBool(enabled))
		}
	}

	if patch.ApplyDefaultSources && !hasCaptureSources(decoded) {
		sources := DefaultCaptureSources()
		doc.appendSources(sources)
		changes = append(changes, GuidedConfigChange{
			Field: "capture.sources",
			Old:   "(unset)",
			New:   fmt.Sprintf("%d default sources", len(sources)),
		})
	}

	changed := len(changes) > 0
	content := existing
	if changed {
		content = doc.bytes()
		if _, err := decodeTOMLMap(content); err != nil {
			return nil, fmt.Errorf("generated config is invalid: %w", err)
		}
	}
	if !changed {
		changes = nil
	}
	return &GuidedConfigPlan{
		Path:     path,
		Exists:   exists,
		Changed:  changed,
		Changes:  changes,
		content:  content,
		original: existing,
		mode:     mode,
	}, nil
}

func (p *GuidedConfigPlan) Content() []byte {
	if p == nil {
		return nil
	}
	out := make([]byte, len(p.content))
	copy(out, p.content)
	return out
}

func (p *GuidedConfigPlan) Write() error {
	if p == nil {
		return fmt.Errorf("config plan is nil")
	}
	if !p.Changed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if p.Exists {
		backupPath, err := nextBackupPath(p.Path, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := os.WriteFile(backupPath, p.original, p.mode); err != nil {
			return fmt.Errorf("write config backup: %w", err)
		}
		p.BackupPath = backupPath
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.Path), filepath.Base(p.Path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create config temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(p.mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod config temp file: %w", err)
	}
	if _, err := tmp.Write(p.content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write config temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync config temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close config temp file: %w", err)
	}
	if err := os.Rename(tmpPath, p.Path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	cleanup = false
	return nil
}

func decodeTOMLMap(data []byte) (map[string]any, error) {
	decoded := map[string]any{}
	if strings.TrimSpace(string(data)) == "" {
		return decoded, nil
	}
	if err := toml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("parse existing config: %w", err)
	}
	return decoded, nil
}

func addStringFieldChange(changes *[]GuidedConfigChange, root map[string]any, section, key, next string) bool {
	current, ok := stringField(root, section, key)
	if ok && current == next {
		return false
	}
	*changes = append(*changes, GuidedConfigChange{
		Field: section + "." + key,
		Old:   displayCurrent(ok, current),
		New:   next,
	})
	return true
}

func addBoolFieldChange(changes *[]GuidedConfigChange, root map[string]any, section, key string, next bool) bool {
	current, ok := boolField(root, section, key)
	if ok && current == next {
		return false
	}
	*changes = append(*changes, GuidedConfigChange{
		Field: section + "." + key,
		Old:   displayCurrent(ok, strconv.FormatBool(current)),
		New:   strconv.FormatBool(next),
	})
	return true
}

func displayCurrent(ok bool, current string) string {
	if !ok {
		return "(unset)"
	}
	return current
}

func stringField(root map[string]any, section, key string) (string, bool) {
	table, ok := tableField(root, section)
	if !ok {
		return "", false
	}
	value, ok := table[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func boolField(root map[string]any, section, key string) (bool, bool) {
	table, ok := tableField(root, section)
	if !ok {
		return false, false
	}
	value, ok := table[key]
	if !ok {
		return false, false
	}
	enabled, ok := value.(bool)
	return enabled, ok
}

func tableField(root map[string]any, section string) (map[string]any, bool) {
	value, ok := root[section]
	if !ok {
		return nil, false
	}
	table, ok := value.(map[string]any)
	return table, ok
}

func hasCaptureSources(root map[string]any) bool {
	capture, ok := tableField(root, "capture")
	if !ok {
		return false
	}
	_, ok = capture["sources"]
	return ok
}

func nextBackupPath(path string, now time.Time) (string, error) {
	stamp := now.Format("20060102150405")
	for i := 0; i < 1000; i++ {
		candidate := path + ".bak." + stamp
		if i > 0 {
			candidate = fmt.Sprintf("%s.bak.%s.%d", path, stamp, i)
		}
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat config backup: %w", err)
		}
	}
	return "", fmt.Errorf("could not choose unused backup path for %s", path)
}

type tomlPatchDocument struct {
	lines []string
}

func newTOMLPatchDocument(data []byte) *tomlPatchDocument {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return &tomlPatchDocument{}
	}
	return &tomlPatchDocument{lines: strings.Split(text, "\n")}
}

func (d *tomlPatchDocument) bytes() []byte {
	if len(d.lines) == 0 {
		return nil
	}
	return []byte(strings.Join(d.lines, "\n") + "\n")
}

func (d *tomlPatchDocument) setKey(section, key, encodedValue string) {
	if d.setRootDottedKey(section, key, encodedValue) {
		return
	}
	start, end, ok := d.sectionRange(section)
	line := key + " = " + encodedValue
	if !ok {
		d.appendSection(section)
		d.lines = append(d.lines, line)
		return
	}
	for i := start + 1; i < end; i++ {
		if keyLineMatches(d.lines[i], key) {
			indent := leadingWhitespace(d.lines[i])
			d.lines[i] = indent + line
			return
		}
	}
	insertAt := end
	d.lines = append(d.lines, "")
	copy(d.lines[insertAt+1:], d.lines[insertAt:])
	d.lines[insertAt] = line
}

func (d *tomlPatchDocument) setRootDottedKey(section, key, encodedValue string) bool {
	line := section + "." + key + " = " + encodedValue
	for i := range d.lines {
		if _, _, ok := tableLineName(d.lines[i]); ok {
			return false
		}
		trimmed := strings.TrimSpace(d.lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := tomlKeyValueDelimiterIndex(trimmed)
		if eq < 0 {
			continue
		}
		if !tomlKeyPathMatches(strings.TrimSpace(trimmed[:eq]), section, key) {
			continue
		}
		indent := leadingWhitespace(d.lines[i])
		d.lines[i] = indent + line
		return true
	}
	return false
}

func (d *tomlPatchDocument) appendSources(sources []SourceConfig) {
	if len(sources) == 0 {
		return
	}
	if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
		d.lines = append(d.lines, "")
	}
	for i, source := range sources {
		if i > 0 {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines,
			"[[capture.sources]]",
			"name = "+strconv.Quote(source.Name),
			"runtime = "+strconv.Quote(source.Runtime),
			"provider = "+strconv.Quote(source.Provider),
			"glob = "+strconv.Quote(source.Glob),
			"watch_root = "+strconv.Quote(source.WatchRoot),
			"format = "+strconv.Quote(source.Format),
		)
	}
}

func (d *tomlPatchDocument) appendSection(section string) {
	if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, "["+section+"]")
}

func (d *tomlPatchDocument) sectionRange(section string) (int, int, bool) {
	start := -1
	for i, line := range d.lines {
		name, array, ok := tableLineName(line)
		if !ok {
			continue
		}
		if !array && name == section {
			start = i
			continue
		}
		if start >= 0 {
			return start, i, true
		}
	}
	if start >= 0 {
		return start, len(d.lines), true
	}
	return 0, 0, false
}

func tableLineName(line string) (string, bool, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[[") {
		end := strings.Index(trimmed, "]]")
		if end < 0 {
			return "", false, false
		}
		name, ok := normalizeTOMLKeyPath(strings.TrimSpace(trimmed[2:end]))
		return name, true, ok
	}
	if strings.HasPrefix(trimmed, "[") {
		end := strings.Index(trimmed, "]")
		if end < 0 {
			return "", false, false
		}
		name, ok := normalizeTOMLKeyPath(strings.TrimSpace(trimmed[1:end]))
		return name, false, ok
	}
	return "", false, false
}

func keyLineMatches(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	eq := tomlKeyValueDelimiterIndex(trimmed)
	if eq < 0 {
		return false
	}
	return tomlKeyPathMatches(strings.TrimSpace(trimmed[:eq]), key)
}

func tomlKeyValueDelimiterIndex(line string) int {
	var quote rune
	escaped := false
	for i, r := range line {
		if quote != 0 {
			if quote == '"' && r == '\\' && !escaped {
				escaped = true
				continue
			}
			if r == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
		case '=':
			return i
		}
	}
	return -1
}

func tomlKeyPathMatches(raw string, keys ...string) bool {
	parts, ok := splitTOMLKeyPath(raw)
	if !ok || len(parts) != len(keys) {
		return false
	}
	for i := range keys {
		if parts[i] != keys[i] {
			return false
		}
	}
	return true
}

func normalizeTOMLKeyPath(raw string) (string, bool) {
	parts, ok := splitTOMLKeyPath(raw)
	if !ok {
		return "", false
	}
	return strings.Join(parts, "."), true
}

func splitTOMLKeyPath(raw string) ([]string, bool) {
	segments := splitTOMLKeySegments(raw)
	if len(segments) == 0 {
		return nil, false
	}
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		part, ok := parseTOMLKeySegment(strings.TrimSpace(segment))
		if !ok {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

func splitTOMLKeySegments(raw string) []string {
	var segments []string
	start := 0
	var quote rune
	escaped := false
	for i, r := range raw {
		if quote != 0 {
			if quote == '"' && r == '\\' && !escaped {
				escaped = true
				continue
			}
			if r == quote && !escaped {
				quote = 0
			}
			escaped = false
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
		case '.':
			segments = append(segments, raw[start:i])
			start = i + 1
		}
	}
	segments = append(segments, raw[start:])
	return segments
}

func parseTOMLKeySegment(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	switch raw[0] {
	case '"':
		unquoted, err := strconv.Unquote(raw)
		return unquoted, err == nil
	case '\'':
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", false
		}
		return raw[1 : len(raw)-1], true
	default:
		return raw, true
	}
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
