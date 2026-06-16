package redaction

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const Version = "redact-v1"

const (
	SecretMarker     = "[REDACTED_SECRET]"
	PathMarker       = "[REDACTED_PATH]"
	EnvMarker        = "[REDACTED_ENV]"
	LiteralMarker    = "[REDACTED_VALUE]"
	PrivateKeyMarker = "[REDACTED_PRIVATE_KEY]"
	CredentialMarker = "[REDACTED_CREDENTIAL]"
)

var DefaultEnvMasks = []string{
	"OPENAI_API_KEY",
	"ANTHROPIC_API_KEY",
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
}

type Config struct {
	PathMasks    []string
	EnvMasks     []string
	LiteralMasks []string
}

type Policy struct {
	pathMasks    []string
	envMasks     []string
	literalMasks []string
}

type Result struct {
	Text     string
	Changed  bool
	Redacted bool
}

type regexReplacement struct {
	re          *regexp.Regexp
	replacement string
}

var privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
var urlCredentialPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)([^/@:\s]+):([^/@\s]+)@`)

var credentialPatterns = []regexReplacement{
	{regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]{8,}`), `${1}` + SecretMarker},
	{regexp.MustCompile(`(?i)\b(Basic\s+)[A-Za-z0-9+/=]{12,}`), `${1}` + SecretMarker},
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`), SecretMarker},
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`), SecretMarker},
	{regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{16,}\b`), SecretMarker},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{16,}\b`), SecretMarker},
	{regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{16,}\b`), SecretMarker},
}

const credentialKey = `(?:api[_-]?key|apikey|token|secret|password|passwd|authorization|access[_-]?token|refresh[_-]?token|client[_-]?secret|private[_-]?key|aws[_-]?secret[_-]?access[_-]?key|aws[_-]?access[_-]?key[_-]?id|session[_-]?token|credential|credentials)`

var assignmentPatterns = []regexReplacement{
	{regexp.MustCompile(`(?i)(\\["'])(` + credentialKey + `)(\\["'])(\s*:\s*)(\\["'])(?:\\\\.|[^\\])*?(\\["'])`), `$1$2$3$4$5` + SecretMarker + `$6`},
	{regexp.MustCompile(`(?i)(["']?)(` + credentialKey + `)(["']?)(\s*[:=]\s*)(")(?:\\.|[^"\\])*(")`), `$1$2$3$4$5` + SecretMarker + `$6`},
	{regexp.MustCompile(`(?i)(["']?)(` + credentialKey + `)(["']?)(\s*[:=]\s*)(')(?:\\.|[^'\\])*(')`), `$1$2$3$4$5` + SecretMarker + `$6`},
	{regexp.MustCompile(`(?i)\b(` + credentialKey + `)\b(\s*[:=]\s*)[^\s"',;}\\]+`), `$1$2` + SecretMarker},
}

func DefaultPolicy() *Policy {
	return NewPolicy(Config{EnvMasks: DefaultEnvMasks})
}

func NewPolicy(cfg Config) *Policy {
	p := &Policy{
		pathMasks:    normalizedLiteralMasks(expandedPathMasks(cfg.PathMasks), 3),
		envMasks:     normalizedLiteralMasks(envMaskValues(cfg.EnvMasks), 4),
		literalMasks: normalizedLiteralMasks(cfg.LiteralMasks, 4),
	}
	return p
}

func (p *Policy) Apply(value string) Result {
	if value == "" {
		return Result{Text: value}
	}
	original := value
	value = privateKeyPattern.ReplaceAllString(value, PrivateKeyMarker)
	value = urlCredentialPattern.ReplaceAllString(value, `${1}`+CredentialMarker+`@`)
	for _, pattern := range credentialPatterns {
		value = pattern.re.ReplaceAllString(value, pattern.replacement)
	}
	for _, pattern := range assignmentPatterns {
		value = pattern.re.ReplaceAllString(value, pattern.replacement)
	}
	value = replaceLiterals(value, p.pathMasks, PathMarker)
	value = replaceLiterals(value, p.envMasks, EnvMarker)
	value = replaceLiterals(value, p.literalMasks, LiteralMarker)
	changed := value != original
	return Result{Text: value, Changed: changed, Redacted: changed}
}

func (p *Policy) Redact(value string) string {
	if p == nil {
		p = DefaultPolicy()
	}
	return p.Apply(value).Text
}

func (p *Policy) RedactPath(value string) string {
	return p.Redact(value)
}

func Redact(value string) string {
	return DefaultPolicy().Redact(value)
}

func replaceLiterals(value string, masks []string, marker string) string {
	for _, mask := range masks {
		value = strings.ReplaceAll(value, mask, marker)
	}
	return value
}

func expandedPathMasks(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
		expanded := expandHomePath(value)
		if expanded != value {
			out = append(out, expanded)
		}
		if cleaned := filepath.Clean(expanded); cleaned != "." && cleaned != expanded {
			out = append(out, cleaned)
		}
	}
	return out
}

func envMaskValues(names []string) []string {
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func normalizedLiteralMasks(values []string, minLen int) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if len(value) < minLen {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) == len(out[j]) {
			return out[i] < out[j]
		}
		return len(out[i]) > len(out[j])
	})
	return out
}

func expandHomePath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
