/*
Copyright 2022 The Kruise Authors.

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

package client


import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestNewRegistry(t *testing.T) {
	config := &rest.Config{Host: "https://example.com", UserAgent: "kruise"}
	err := NewRegistry(config)
	if err != nil {
		t.Fatalf("NewRegistry() failed: %v", err)
	}

	if GetGenericClient() == nil {
		t.Errorf("Expected defaultGenericClient to be set")
	}
}

func TestGetGenericClientWithName(t *testing.T) {
	config := &rest.Config{Host: "https://example.com", UserAgent: "kruise"}
	err := NewRegistry(config)
	if err != nil {
		t.Fatalf("NewRegistry() failed: %v", err)
	}

	clientWithUA := GetGenericClientWithName("game")
	if clientWithUA == nil {
		t.Errorf("Expected client with user-agent to be created")
	}
}

func TestGetGenericClientWithName_NilConfig(t *testing.T) {
	// reset cfg to nil manually
	cfg = nil

	result := GetGenericClientWithName("fail")
	if result != nil {
		t.Errorf("Expected nil when cfg is nil")
	}
}
