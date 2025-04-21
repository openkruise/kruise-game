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

package util

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestOrdinalSetToIntStrSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    sets.Set[int]
		expected []intstr.IntOrString
	}{
		{
			name:     "single element",
			input:    sets.New(5),
			expected: []intstr.IntOrString{intstr.FromInt(5)},
		},
		{
			name:     "continuous elements",
			input:    sets.New(1, 2, 3, 4, 5),
			expected: []intstr.IntOrString{intstr.FromString("1-5")},
		},
		{
			name:  "multiple continuous elements",
			input: sets.New(1, 2, 3, 5, 6, 7),
			expected: []intstr.IntOrString{
				intstr.FromString("1-3"),
				intstr.FromString("5-7"),
			},
		},
		{
			name:  "multiple continuous elements with single element",
			input: sets.New(1, 2, 3, 5, 7, 8, 9, 11),
			expected: []intstr.IntOrString{
				intstr.FromString("1-3"),
				intstr.FromInt(5),
				intstr.FromString("7-9"),
				intstr.FromInt(11),
			},
		},
		{
			name:  "unsorted continuous elements",
			input: sets.New(3, 1, 2, 4, 8, 6, 7),
			expected: []intstr.IntOrString{
				intstr.FromString("1-4"),
				intstr.FromString("6-8"),
			},
		},
		{
			name:  "non-continuous elements",
			input: sets.New(1, 2, 5, 7, 9),
			expected: []intstr.IntOrString{
				intstr.FromInt(1),
				intstr.FromInt(2),
				intstr.FromInt(5),
				intstr.FromInt(7),
				intstr.FromInt(9),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := OrdinalSetToIntStrSlice(tc.input)

			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("OrdinalSetToIntStrSlice(%v) = %v, expected %v", tc.input.UnsortedList(), result, tc.expected)
			}
		})
	}
}

// test OrdinalSetToIntStrSlice with different types
func TestOrdinalSetToIntStrSliceWithDifferentTypes(t *testing.T) {
	int32Set := sets.New[int32](1, 2, 3, 5)
	expected := []intstr.IntOrString{
		intstr.FromString("1-3"),
		intstr.FromInt(5),
	}

	result := OrdinalSetToIntStrSlice(int32Set)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("use int32 type test failed: got %v, expected %v", result, expected)
	}

	// 测试uint类型
	uintSet := sets.New[uint](10, 11, 12, 15)
	expected = []intstr.IntOrString{
		intstr.FromString("10-12"),
		intstr.FromInt(15),
	}

	result = OrdinalSetToIntStrSlice(uintSet)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("use uint type test failed: got %v, expected %v", result, expected)
	}
}

func TestStringToOrdinalIntSet(t *testing.T) {
	tests := []struct {
		name      string
		str       string
		delimiter string
		expected  sets.Set[int]
	}{
		{
			name:      "empty string",
			str:       "",
			delimiter: ",",
			expected:  sets.New[int](),
		},
		{
			name:      "single number",
			str:       "5",
			delimiter: ",",
			expected:  sets.New(5),
		},
		{
			name:      "multiple numbers",
			str:       "1,3,5,7",
			delimiter: ",",
			expected:  sets.New(1, 3, 5, 7),
		},
		{
			name:      "single range",
			str:       "1-5",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 4, 5),
		},
		{
			name:      "multiple ranges",
			str:       "1-3,7-9",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 7, 8, 9),
		},
		{
			name:      "mixed numbers and ranges",
			str:       "1-3,5,7-9,11",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 5, 7, 8, 9, 11),
		},
		{
			name:      "with spaces",
			str:       "1-3, 5, 7-9, 11",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 5, 7, 8, 9, 11),
		},
		{
			name:      "different delimiter",
			str:       "1-3;5;7-9;11",
			delimiter: ";",
			expected:  sets.New(1, 2, 3, 5, 7, 8, 9, 11),
		},
		{
			name:      "invalid number",
			str:       "1,abc,3",
			delimiter: ",",
			expected:  sets.New(1, 3),
		},
		{
			name:      "invalid range",
			str:       "1-3,5-abc,7-9",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 7, 8, 9),
		},
		{
			name:      "inverted range",
			str:       "1-3,5-2,7-9",
			delimiter: ",",
			expected:  sets.New(1, 2, 3, 7, 8, 9),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := StringToOrdinalIntSet(tc.str, tc.delimiter)

			if !result.Equal(tc.expected) {
				t.Errorf("StringToOrdinalIntSet(%q, %q) = %v, expected %v",
					tc.str, tc.delimiter, result.UnsortedList(), tc.expected.UnsortedList())
			}
		})
	}
}

func TestIntSetToString(t *testing.T) {
	tests := []struct {
		name      string
		set       sets.Set[int]
		delimiter string
		expected  string
	}{
		{
			name:      "empty set",
			set:       sets.New[int](),
			delimiter: ",",
			expected:  "",
		},
		{
			name:      "single element",
			set:       sets.New(5),
			delimiter: ",",
			expected:  "5",
		},
		{
			name:      "multiple elements",
			set:       sets.New(1, 3, 5, 7),
			delimiter: ",",
			expected:  "1,3,5,7",
		},
		{
			name:      "continuous elements",
			set:       sets.New(1, 2, 3, 4, 5),
			delimiter: ",",
			expected:  "1-5",
		},
		{
			name:      "mixed continuous and single elements",
			set:       sets.New(1, 2, 3, 5, 7, 8, 9, 11),
			delimiter: ",",
			expected:  "1-3,5,7-9,11",
		},
		{
			name:      "unsorted elements",
			set:       sets.New(5, 3, 1, 2, 4),
			delimiter: ",",
			expected:  "1-5",
		},
		{
			name:      "different delimiter",
			set:       sets.New(1, 2, 3, 5, 7),
			delimiter: ";",
			expected:  "1-3;5;7",
		},
		{
			name:      "non-continuous elements",
			set:       sets.New(1, 2, 5),
			delimiter: ",",
			expected:  "1,2,5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := intSetToString(tc.set, tc.delimiter)
			if result != tc.expected {
				t.Errorf("intSetToString(%v, %q) = %q, expected %q",
					tc.set.UnsortedList(), tc.delimiter, result, tc.expected)
			}
		})
	}
}
