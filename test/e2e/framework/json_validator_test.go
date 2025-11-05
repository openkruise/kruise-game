package framework

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateJSONLogs(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantError bool
	}{
		{
			name: "valid JSON logs",
			content: `# Logs from pod: manager-abc
{"time":"2025-01-10T12:34:56Z","level":"INFO","severity_number":9,"msg":"Starting controller","code":{"function":"main.main","filepath":"/workspace/main.go","lineno":123}}
{"time":"2025-01-10T12:34:57Z","level":"INFO","severity_number":9,"msg":"Reconciling","code":{"function":"github.com/openkruise/kruise-game/controllers.Reconcile","filepath":"/workspace/controllers/reconcile.go","lineno":45}}
`,
			wantError: false,
		},
		{
			name: "invalid JSON",
			content: `{"time":"2025-01-10T12:34:56Z","level":"INFO","severity_number":9,"msg":"Starting","code":{"function":"main.main","filepath":"/workspace/main.go","lineno":1}}
this is not JSON
`,
			wantError: true,
		},
		{
			name: "missing required field",
			content: `{"time":"2025-01-10T12:34:56Z","msg":"Missing level","code":{"function":"main.main","filepath":"/workspace/main.go","lineno":1}}
`,
			wantError: true,
		},
		{
			name: "empty file",
			content: `
# Only comments
`,
			wantError: true,
		},
		{
			name: "missing code block",
			content: `{"time":"2025-01-10T12:34:56Z","level":"INFO","severity_number":9,"msg":"Test"}
`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := filepath.Join(t.TempDir(), "test.log")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			err := ValidateJSONLogs(tmpFile)
			if (err != nil) != tt.wantError {
				t.Errorf("ValidateJSONLogs() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
