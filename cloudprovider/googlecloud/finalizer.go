/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// hasFinalizer reports whether a slice already contains the given name.
func hasFinalizer(list []string, name string) bool {
	for _, f := range list {
		if f == name {
			return true
		}
	}
	return false
}

// removeFinalizer returns the slice with the named finalizer removed (if any).
func removeFinalizer(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, f := range list {
		if f == name {
			continue
		}
		out = append(out, f)
	}
	return out
}

// EnsurePodFinalizer adds name to obj's finalizers in-place and returns true
// when a mutation happened. The caller is responsible for persisting via the
// admission-webhook return value (Plugin contract) or a client.Update call.
func EnsurePodFinalizer(obj client.Object, name string) (mutated bool) {
	finals := obj.GetFinalizers()
	if hasFinalizer(finals, name) {
		return false
	}
	obj.SetFinalizers(append(finals, name))
	return true
}

// RemovePodFinalizer drops the named finalizer from obj and persists via Update.
// Caller handles the returned error.
func RemovePodFinalizer(ctx context.Context, c client.Client, obj client.Object, name string) error {
	finals := obj.GetFinalizers()
	if !hasFinalizer(finals, name) {
		return nil
	}
	obj.SetFinalizers(removeFinalizer(finals, name))
	return c.Update(ctx, obj)
}
