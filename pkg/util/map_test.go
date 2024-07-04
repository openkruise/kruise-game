package util

import (
	"reflect"
	"testing"
)

func TestMergeMapString(t *testing.T) {
	tests := []struct {
		a      map[string]string
		b      map[string]string
		result map[string]string
	}{
		{
			a: map[string]string{
				"foo-A": "bar",
			},
			b: map[string]string{
				"foo-B": "bar",
			},
			result: map[string]string{
				"foo-A": "bar",
				"foo-B": "bar",
			},
		},
		{
			a: map[string]string{
				"foo-A": "bar",
			},
			b: map[string]string{
				"foo-A": "barB",
			},
			result: map[string]string{
				"foo-A": "barB",
			},
		},
		{
			a: map[string]string{},
			b: map[string]string{
				"foo-A": "barB",
			},
			result: map[string]string{
				"foo-A": "barB",
			},
		},
		{
			a: map[string]string{
				"foo-A": "bar",
			},
			b: map[string]string{},
			result: map[string]string{
				"foo-A": "bar",
			},
		},
		{
			result: nil,
		},
	}

	for _, test := range tests {
		expect := test.result
		actual := MergeMapString(test.a, test.b)
		if !reflect.DeepEqual(expect, actual) {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}
