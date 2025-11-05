/*
Copyright 2024 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"testing"
)

func TestParseIntOrStringFromTemplate(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		probeResult string
		wantValue   string
		wantErr     bool
	}{
		{
			name:        "simple integer result",
			template:    "{{.Result}}",
			probeResult: "42",
			wantValue:   "42",
			wantErr:     false,
		},
		{
			name:        "conditional template - true case",
			template:    "{{if eq .Result \"0\"}}1{{else}}100{{end}}",
			probeResult: "0",
			wantValue:   "1",
			wantErr:     false,
		},
		{
			name:        "conditional template - false case",
			template:    "{{if eq .Result \"0\"}}1{{else}}100{{end}}",
			probeResult: "50",
			wantValue:   "100",
			wantErr:     false,
		},
		{
			name:        "no template - direct integer",
			template:    "75",
			probeResult: "anything",
			wantValue:   "75",
			wantErr:     false,
		},
		{
			name:        "no template - direct string",
			template:    "high",
			probeResult: "anything",
			wantValue:   "",
			wantErr:     true, // Non-numeric strings should be rejected
		},
		{
			name:        "empty result",
			template:    "",
			probeResult: "50",
			wantValue:   "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIntOrStringFromTemplate(tt.template, tt.probeResult)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIntOrStringFromTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got == nil {
					t.Errorf("ParseIntOrStringFromTemplate() returned nil")
					return
				}
				if got.String() != tt.wantValue {
					t.Errorf("ParseIntOrStringFromTemplate() = %v, want %v", got.String(), tt.wantValue)
				}
			}
		})
	}
}
