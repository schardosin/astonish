package credentials

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkRedact measures redaction performance with varying numbers of
// tracked secrets and text sizes.
func BenchmarkRedact(b *testing.B) {
	// Setup: create redactor with N secrets
	for _, numSecrets := range []int{1, 10, 50, 100} {
		for _, textSize := range []int{100, 1000, 10000} {
			name := fmt.Sprintf("secrets=%d/textSize=%d", numSecrets, textSize)
			b.Run(name, func(b *testing.B) {
				r := NewRedactor()
				for i := 0; i < numSecrets; i++ {
					r.AddSecret(
						fmt.Sprintf("cred-%d", i),
						fmt.Sprintf("secret_value_%08d_padding", i),
					)
				}

				// Build text that contains some secrets scattered within
				var builder strings.Builder
				for builder.Len() < textSize {
					builder.WriteString("some normal text without any secrets ")
				}
				text := builder.String()[:textSize]

				b.ResetTimer()
				b.ReportAllocs()

				for i := 0; i < b.N; i++ {
					_ = r.Redact(text)
				}
			})
		}
	}
}

// BenchmarkRedactWithHits measures redaction when the text contains secrets
// that need to be replaced.
func BenchmarkRedactWithHits(b *testing.B) {
	r := NewRedactor()
	secrets := make([]string, 20)
	for i := 0; i < 20; i++ {
		secrets[i] = fmt.Sprintf("secret_value_%08d_padding", i)
		r.AddSecret(fmt.Sprintf("cred-%d", i), secrets[i])
	}

	// Build text that contains every secret once
	var builder strings.Builder
	for _, s := range secrets {
		builder.WriteString("prefix text ")
		builder.WriteString(s)
		builder.WriteString(" suffix text ")
	}
	text := builder.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = r.Redact(text)
	}
}

// BenchmarkRedactMap measures deep-walk redaction of nested maps.
func BenchmarkRedactMap(b *testing.B) {
	r := NewRedactor()
	r.AddSecret("api-key", "sk_test_12345678_production")

	m := map[string]any{
		"output": "The API key is sk_test_12345678_production",
		"nested": map[string]any{
			"deep": "Contains sk_test_12345678_production too",
			"list": []any{
				"normal",
				"sk_test_12345678_production",
				map[string]any{"key": "sk_test_12345678_production"},
			},
		},
		"safe": "no secrets here",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = r.RedactMap(m)
	}
}

// BenchmarkAddSecret measures the cost of adding a new secret to the redactor.
func BenchmarkAddSecret(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		r := NewRedactor()
		for j := 0; j < 50; j++ {
			r.AddSecret(
				fmt.Sprintf("cred-%d", j),
				fmt.Sprintf("secret_value_%08d_padding", j),
			)
		}
	}
}
