package beaconcli

import (
	"github.com/johnnygreco/beacon/internal/config"
	"github.com/johnnygreco/beacon/internal/redaction"
)

func redactionPolicyFromConfig(cfg *config.Config) *redaction.Policy {
	if cfg == nil {
		return redaction.DefaultPolicy()
	}
	return redaction.NewPolicy(redaction.Config{
		PathMasks:    cfg.Redaction.PathMasks,
		EnvMasks:     cfg.Redaction.EnvMasks,
		LiteralMasks: cfg.Redaction.LiteralMasks,
	})
}

func redactStrings(policy *redaction.Policy, values []string) []string {
	if len(values) == 0 {
		return nil
	}
	if policy == nil {
		policy = redaction.DefaultPolicy()
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = policy.Redact(value)
	}
	return out
}
