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

func TestRenderTemplate(t *testing.T) {
	probeResult := "42"

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "no template syntax",
			template: "static-value",
			want:     "static-value",
		},
		{
			name:     "simple result substitution",
			template: "{{.Result}}",
			want:     "42",
		},
		{
			name:     "result in string",
			template: "player-count-{{.Result}}",
			want:     "player-count-42",
		},
		{
			name:     "conditional eq",
			template: "{{if eq .Result \"42\"}}high{{else}}low{{end}}",
			want:     "high",
		},
		{
			name:     "conditional ne",
			template: "{{if ne .Result \"0\"}}active{{else}}empty{{end}}",
			want:     "active",
		},
		{
			name:     "conditional gt",
			template: "{{if gt .Result \"40\"}}high-load{{else}}normal{{end}}",
			want:     "high-load",
		},
		{
			name:     "conditional lt",
			template: "{{if lt .Result \"50\"}}ok{{else}}critical{{end}}",
			want:     "ok",
		},
		{
			name:     "invalid template returns original",
			template: "{{.InvalidField}}",
			want:     "{{.InvalidField}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderTemplate(tt.template, probeResult)
			if got != tt.want {
				t.Errorf("RenderTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderTemplate_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		probeResult string
		wantContain string
	}{
		{
			name:        "empty result",
			template:    "value-{{.Result}}",
			probeResult: "",
			wantContain: "value-",
		},
		{
			name:        "numeric comparison",
			template:    "{{if gt .Result \"100\"}}big{{else}}small{{end}}",
			probeResult: "150",
			wantContain: "big",
		},
		{
			name:        "string comparison fallback",
			template:    "{{if gt .Result \"abc\"}}yes{{else}}no{{end}}",
			probeResult: "xyz",
			wantContain: "yes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderTemplate(tt.template, tt.probeResult)
			if got != tt.wantContain {
				t.Errorf("RenderTemplate() = %v, want %v", got, tt.wantContain)
			}
		})
	}
}
