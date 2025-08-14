package services

import (
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestFetchService_matchesDomainPattern(t *testing.T) {
	cfg := &config.Config{
		Fetch: config.FetchConfig{
			Enabled: true,
		},
	}

	service := NewFetchService(cfg)
	tests := getDomainPatternTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.matchesDomainPattern(tt.domain, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesDomainPattern(%q, %q) = %v, expected %v",
					tt.domain, tt.pattern, result, tt.expected)
			}
		})
	}
}

type domainPatternTest struct {
	name     string
	domain   string
	pattern  string
	expected bool
}

func getDomainPatternTests() []domainPatternTest {
	return []domainPatternTest{
		{"wildcard allows any domain", "blog.example.com", "*", true},
		{"wildcard allows github.com", "github.com", "*", true},
		{"wildcard allows subdomains", "api.example.com", "*", true},
		{"exact domain match", "github.com", "github.com", true},
		{"exact domain no match", "api.github.com", "github.com", false},
		{"exact domain different", "gitlab.com", "github.com", false},
		{"subdomain pattern matches base domain", "example.com", "*.example.com", true},
		{"subdomain pattern matches subdomain", "api.example.com", "*.example.com", true},
		{"subdomain pattern matches nested subdomain", "v1.api.example.com", "*.example.com", true},
		{"subdomain pattern no match different domain", "api.github.com", "*.example.com", false},
		{"subdomain pattern with nested", "docs.api.example.com", "*.api.example.com", true},
		{"regex pattern simple", "api1.example.com", "api[0-9]+\\.example\\.com", true},
		{"regex pattern no match", "api.example.com", "api[0-9]+\\.example\\.com", false},
		{"regex pattern with alternatives", "api.example.com", "(api|www)\\.example\\.com", true},
		{"regex pattern with alternatives no match", "docs.example.com", "(api|www)\\.example\\.com", false},
		{"regex pattern anchored", "example.com", "^example\\.com$", true},
		{"regex pattern anchored no match", "api.example.com", "^example\\.com$", false},
		{"no implicit suffix matching", "api.github.com", "github.com", false},
		{"no implicit nested suffix matching", "v1.api.github.com", "github.com", false},
		{"different domain no match", "github.io", "github.com", false},
		{"empty pattern", "example.com", "", false},
		{"empty domain", "", "example.com", false},
		{"both empty", "", "", true},
		{"invalid regex pattern", "example.com", "[invalid", false},
		{"test.com subdomain", "blog.test.com", "*.test.com", true},
		{"test.com base domain", "test.com", "*.test.com", true},
		{"test.com different domain", "example.com", "*.test.com", false},
	}
}

func TestFetchService_ValidateURL(t *testing.T) {
	tests := []struct {
		name               string
		enabled            bool
		whitelistedDomains []string
		url                string
		wantError          bool
		errorContains      string
	}{
		{
			name:               "fetch disabled",
			enabled:            false,
			whitelistedDomains: []string{"github.com"},
			url:                "https://github.com/user/repo",
			wantError:          true,
			errorContains:      "fetch tool is not enabled",
		},
		{
			name:               "no whitelisted domains",
			enabled:            true,
			whitelistedDomains: []string{},
			url:                "https://github.com/user/repo",
			wantError:          true,
			errorContains:      "no whitelisted domains configured",
		},
		{
			name:               "invalid URL format",
			enabled:            true,
			whitelistedDomains: []string{"*"},
			url:                "not-a-url",
			wantError:          true,
			errorContains:      "only HTTP and HTTPS URLs are allowed",
		},
		{
			name:               "non-HTTP scheme",
			enabled:            true,
			whitelistedDomains: []string{"*"},
			url:                "ftp://example.com/file.txt",
			wantError:          true,
			errorContains:      "only HTTP and HTTPS URLs are allowed",
		},
		{
			name:               "wildcard allows any domain",
			enabled:            true,
			whitelistedDomains: []string{"*"},
			url:                "https://blog.example.com/post",
			wantError:          false,
		},
		{
			name:               "exact domain match",
			enabled:            true,
			whitelistedDomains: []string{"github.com"},
			url:                "https://github.com/user/repo",
			wantError:          false,
		},
		{
			name:               "subdomain pattern match",
			enabled:            true,
			whitelistedDomains: []string{"*.github.com"},
			url:                "https://api.github.com/user",
			wantError:          false,
		},
		{
			name:               "domain not whitelisted",
			enabled:            true,
			whitelistedDomains: []string{"github.com"},
			url:                "https://gitlab.com/user/repo",
			wantError:          true,
			errorContains:      "domain not whitelisted",
		},
		{
			name:               "multiple patterns one matches",
			enabled:            true,
			whitelistedDomains: []string{"github.com", "*.example.com", "api[0-9]+\\.test\\.com"},
			url:                "https://api1.test.com/endpoint",
			wantError:          false,
		},
		{
			name:               "HTTP protocol allowed",
			enabled:            true,
			whitelistedDomains: []string{"*"},
			url:                "http://example.com",
			wantError:          false,
		},
		{
			name:               "URL with port",
			enabled:            true,
			whitelistedDomains: []string{"localhost", "*.local"},
			url:                "http://localhost:8080/api",
			wantError:          false,
		},
		{
			name:               "URL with path and query",
			enabled:            true,
			whitelistedDomains: []string{"*"},
			url:                "https://api.example.com/v1/users?page=1&limit=10",
			wantError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Fetch: config.FetchConfig{
					Enabled:            tt.enabled,
					WhitelistedDomains: tt.whitelistedDomains,
				},
			}

			service := NewFetchService(cfg)
			err := service.ValidateURL(tt.url)

			if tt.wantError {
				validateExpectedError(t, err, tt.url, tt.errorContains)
				return
			}
			if err != nil {
				t.Errorf("ValidateURL(%q) unexpected error: %v", tt.url, err)
			}
		})
	}
}

// validateExpectedError validates that an error occurred and contains expected text
func validateExpectedError(t *testing.T, err error, url, errorContains string) {
	if err == nil {
		t.Errorf("ValidateURL(%q) expected error but got none", url)
		return
	}
	if errorContains != "" && !containsString(err.Error(), errorContains) {
		t.Errorf("ValidateURL(%q) error = %q, expected to contain %q",
			url, err.Error(), errorContains)
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
