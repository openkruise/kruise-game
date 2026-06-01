/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// dns1123 matches lower-case Kubernetes DNS-1123 label segments (we use it to
// scrub names of disallowed characters before stuffing them into KCC metadata).
var dns1123 = regexp.MustCompile(`[^a-z0-9-]+`)

// DeriveResourceID returns a deterministic, DNS-1123-safe GCP resource name
// derived from a stable Kubernetes UID plus a short kind suffix.
//
// The same (uid, suffix) pair always yields the same ID, so a plugin restart
// will adopt the existing KCC CR via spec.resourceID. The hash collapses the
// UID into 10 hex characters, leaving plenty of room for the suffix and a
// fixed "gs-" prefix while staying under the 63-character cap.
func DeriveResourceID(uid types.UID, suffix string) string {
	sum := sha1.Sum([]byte(string(uid)))
	hash := hex.EncodeToString(sum[:5]) // 10 hex chars
	suffix = sanitizeSuffix(suffix)
	out := fmt.Sprintf("gs-%s-%s", suffix, hash)
	if len(out) > 63 {
		out = out[:63]
	}
	return strings.TrimRight(out, "-")
}

// DeriveServiceName returns the K8s Service name a plugin uses for a given pod.
// Single Service per pod, suffixed by the plugin nickname so passthrough and
// proxy plugins on the same pod do not collide.
func DeriveServiceName(podName, pluginSuffix string) string {
	pluginSuffix = sanitizeSuffix(pluginSuffix)
	name := fmt.Sprintf("%s-%s", podName, pluginSuffix)
	if len(name) > 63 {
		// Hash the over-long suffix portion so the result is still deterministic.
		sum := sha1.Sum([]byte(name))
		name = fmt.Sprintf("%s-%s", podName, hex.EncodeToString(sum[:5]))
		if len(name) > 63 {
			name = name[:63]
		}
	}
	return strings.TrimRight(name, "-")
}

func sanitizeSuffix(s string) string {
	s = strings.ToLower(s)
	s = dns1123.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "x"
	}
	return s
}
