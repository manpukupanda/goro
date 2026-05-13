package referrer

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*(?::[0-9]{1,5})?$`)

// NormalizeWhitelist validates and normalizes whitelist rules.
// Rules must be domain-only and may include an optional wildcard prefix ("*.").
func NormalizeWhitelist(raw []string) ([]string, error) {
	normalized := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		rule, err := normalizeRule(entry)
		if err != nil {
			return nil, err
		}
		if rule == "" {
			continue
		}
		if _, exists := seen[rule]; exists {
			continue
		}
		seen[rule] = struct{}{}
		normalized = append(normalized, rule)
	}
	return normalized, nil
}

// EncodeWhitelist stores rules as newline-separated text.
func EncodeWhitelist(rules []string) string {
	return strings.Join(rules, "\n")
}

// DecodeWhitelist restores rules from newline-separated text.
func DecodeWhitelist(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// IsAllowed checks whether the provided Referer header matches the whitelist.
// Empty whitelist means "no check".
func IsAllowed(refererHeader string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	parsed, err := url.Parse(refererHeader)
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Host)
	for _, rule := range whitelist {
		if strings.HasPrefix(rule, "*.") {
			suffix := rule[2:]
			if host != suffix && strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if host == rule {
			return true
		}
	}
	return false
}

func normalizeRule(entry string) (string, error) {
	rule := strings.ToLower(strings.TrimSpace(entry))
	if rule == "" {
		return "", nil
	}
	if strings.Contains(rule, "://") || strings.ContainsAny(rule, "/?#@") {
		return "", fmt.Errorf("invalid domain rule: %q", entry)
	}

	wildcard := false
	if strings.HasPrefix(rule, "*.") {
		wildcard = true
		rule = strings.TrimPrefix(rule, "*.")
	}

	if strings.Contains(rule, "*") || !domainPattern.MatchString(rule) {
		return "", fmt.Errorf("invalid domain rule: %q", entry)
	}

	if host, port, hasPort := strings.Cut(rule, ":"); hasPort {
		if host == "" || port == "" {
			return "", fmt.Errorf("invalid domain rule: %q", entry)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("invalid domain rule: %q", entry)
		}
	}

	if wildcard {
		return "*." + rule, nil
	}
	return rule, nil
}
