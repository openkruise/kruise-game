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
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/util/intstr"
)

// TemplateContext holds context variables available for template rendering
type TemplateContext struct {
	// Result is the probe result message
	Result string
}

// RenderTemplate renders a template string with given probe result
// Returns original string if no template syntax found or rendering fails
func RenderTemplate(tmpl string, probeResult string) string {
	// Quick check: if no template syntax, return original
	if !strings.Contains(tmpl, "{{") {
		return tmpl
	}

	ctx := TemplateContext{
		Result: probeResult,
	}

	t, err := template.New("field").Funcs(templateFuncs()).Parse(tmpl)
	if err != nil {
		// If template parsing fails, return original value
		return tmpl
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		// If execution fails, return original value
		return tmpl
	}

	return buf.String()
}

// templateFuncs returns custom functions available in templates
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// Comparison functions
		"eq": func(a, b interface{}) bool {
			return a == b
		},
		"ne": func(a, b interface{}) bool {
			return a != b
		},
		"lt": func(a, b string) bool {
			ai, aerr := strconv.ParseFloat(a, 64)
			bi, berr := strconv.ParseFloat(b, 64)
			if aerr == nil && berr == nil {
				return ai < bi
			}
			return a < b
		},
		"le": func(a, b string) bool {
			ai, aerr := strconv.ParseFloat(a, 64)
			bi, berr := strconv.ParseFloat(b, 64)
			if aerr == nil && berr == nil {
				return ai <= bi
			}
			return a <= b
		},
		"gt": func(a, b string) bool {
			ai, aerr := strconv.ParseFloat(a, 64)
			bi, berr := strconv.ParseFloat(b, 64)
			if aerr == nil && berr == nil {
				return ai > bi
			}
			return a > b
		},
		"ge": func(a, b string) bool {
			ai, aerr := strconv.ParseFloat(a, 64)
			bi, berr := strconv.ParseFloat(b, 64)
			if aerr == nil && berr == nil {
				return ai >= bi
			}
			return a >= b
		},
	}
}

// ParseIntOrStringFromTemplate renders template and parses result to IntOrString
// Returns parsed value and error. Error is returned if template rendering fails or result is not a valid number.
// For priority fields (UpdatePriority/DeletionPriority), only numeric values are accepted.
func ParseIntOrStringFromTemplate(tmpl string, probeResult string) (*intstr.IntOrString, error) {
	// Render template first
	renderedValue := RenderTemplate(tmpl, probeResult)

	if renderedValue == "" {
		return nil, fmt.Errorf("rendered value is empty (template: %s, result: %s)", tmpl, probeResult)
	}

	// Try to parse as integer (strict validation for priority fields)
	if intVal, err := strconv.Atoi(renderedValue); err == nil {
		result := intstr.FromInt(intVal)
		return &result, nil
	}

	// Priority fields must be numeric, reject non-numeric strings
	return nil, fmt.Errorf("priority value must be numeric, got: %s (template: %s, probe result: %s)", renderedValue, tmpl, probeResult)
}
