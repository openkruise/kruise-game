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
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestIsNumInList(t *testing.T) {
	tests := []struct {
		number int
		list   []int
		result bool
	}{
		{
			number: 1,
			list:   []int{1, 2, 4},
			result: true,
		},
	}

	for _, test := range tests {
		actual := IsNumInList(test.number, test.list)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestIsStringInList(t *testing.T) {
	tests := []struct {
		str    string
		list   []string
		result bool
	}{
		{
			str:    "",
			list:   []string{"", "", ""},
			result: true,
		},
	}

	for _, test := range tests {
		actual := IsStringInList(test.str, test.list)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestGetSliceInANotInB(t *testing.T) {
	tests := []struct {
		a      []int
		b      []int
		result []int
	}{
		{
			a:      []int{4, 5, 1},
			b:      []int{1, 2, 3},
			result: []int{4, 5},
		},
		{
			a:      []int{1, 2},
			b:      []int{},
			result: []int{1, 2},
		},
	}

	for _, test := range tests {
		actual := GetSliceInANotInB(test.a, test.b)
		expect := test.result
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
			}
		}
	}
}

func TestGetSliceInAandInB(t *testing.T) {
	tests := []struct {
		a      []int
		b      []int
		result []int
	}{
		{
			a:      []int{4, 5, 1},
			b:      []int{1, 2, 3},
			result: []int{1},
		},
	}

	for _, test := range tests {
		actual := GetSliceInAandInB(test.a, test.b)
		expect := test.result
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
			}
		}
	}
}

func TestIntSliceToString(t *testing.T) {
	tests := []struct {
		number    []int
		delimiter string
		result    string
	}{
		{
			number:    []int{4, 5, 1},
			delimiter: ",",
			result:    "4,5,1",
		},
	}

	for _, test := range tests {
		actual := IntSliceToString(test.number, test.delimiter)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestInt32SliceToString(t *testing.T) {
	tests := []struct {
		number    []int32
		delimiter string
		result    string
	}{
		{
			number:    []int32{4, 5, 1},
			delimiter: ",",
			result:    "4,5,1",
		},
	}

	for _, test := range tests {
		actual := Int32SliceToString(test.number, test.delimiter)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestStringToIntSlice(t *testing.T) {
	tests := []struct {
		str       string
		delimiter string
		result    []int
	}{
		{
			str:       "4,5,1",
			delimiter: ",",
			result:    []int{4, 5, 1},
		},
	}

	for _, test := range tests {
		actual := StringToIntSlice(test.str, test.delimiter)
		expect := test.result
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
			}
		}
	}
}

func TestStringToInt32Slice(t *testing.T) {
	tests := []struct {
		str       string
		delimiter string
		result    []int32
	}{
		{
			str:       "4,5,1",
			delimiter: ",",
			result:    []int32{4, 5, 1},
		},
	}

	for _, test := range tests {
		actual := StringToInt32Slice(test.str, test.delimiter)
		expect := test.result
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
			}
		}
	}
}

func TestIsSliceEqual(t *testing.T) {
	tests := []struct {
		a      []int
		b      []int
		result bool
	}{
		{
			a:      []int{1, 3, 5},
			b:      []int{5, 1, 3},
			result: true,
		},
		{
			a:      []int{1, 3, 5},
			b:      []int{5, 1, 2},
			result: false,
		},
		{
			a:      nil,
			b:      []int{},
			result: true,
		},
		{
			a:      []int{},
			b:      nil,
			result: true,
		},
		{
			a:      nil,
			b:      nil,
			result: true,
		},
		{
			a:      []int{},
			b:      []int{},
			result: true,
		},
	}

	for i, test := range tests {
		actual := IsSliceEqual(test.a, test.b)
		expect := test.result
		if expect != actual {
			t.Errorf("case %d: expect %v but got %v", i, expect, actual)
		}
	}
}

func TestIsRepeat(t *testing.T) {
	tests := []struct {
		nums   []int
		result bool
	}{
		{
			nums:   []int{1, 2, 4, 1},
			result: true,
		},
		{
			nums:   []int{1, 2, 3},
			result: false,
		},
	}

	for _, test := range tests {
		actual := IsRepeat(test.nums)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestRemoveRepeat(t *testing.T) {
	tests := []struct {
		nums   []int
		result []int
	}{
		{
			nums:   []int{1, 2, 4, 2, 1},
			result: []int{1, 2, 4},
		},
		{
			nums:   []int{1, 2, 3},
			result: []int{1, 2, 3},
		},
	}

	for _, test := range tests {
		actual := RemoveRepeat(test.nums)
		expect := test.result
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
			}
		}
	}
}

func TestIsHasNegativeNum(t *testing.T) {
	tests := []struct {
		nums   []int
		result bool
	}{
		{
			nums:   []int{1, -2, 4, 1},
			result: true,
		},
		{
			nums:   []int{1, 2, 3},
			result: false,
		},
		{
			nums:   []int{},
			result: false,
		},
	}

	for _, test := range tests {
		actual := IsHasNegativeNum(test.nums)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestStringToIntStrSlice(t *testing.T) {
	tests := []struct {
		name      string
		str       string
		delimiter string
		result    []intstr.IntOrString
	}{
		{
			name:      "mixed int and string values",
			str:       "4,test,1",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromInt(4),
				intstr.FromString("test"),
				intstr.FromInt(1),
			},
		},
		{
			name:      "only int values",
			str:       "4,5,1",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromInt(4),
				intstr.FromInt(5),
				intstr.FromInt(1),
			},
		},
		{
			name:      "only string values",
			str:       "a,b,c",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromString("a"),
				intstr.FromString("b"),
				intstr.FromString("c"),
			},
		},
		{
			name:      "empty string",
			str:       "",
			delimiter: ",",
			result:    nil,
		},
		{
			name:      "empty delimiter",
			str:       "1,2,3",
			delimiter: "",
			result:    nil,
		},
		{
			name:      "empty parts",
			str:       "1,,3",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromInt(1),
				intstr.FromInt(3),
			},
		},
		{
			name:      "different delimiter",
			str:       "1:test:3",
			delimiter: ":",
			result: []intstr.IntOrString{
				intstr.FromInt(1),
				intstr.FromString("test"),
				intstr.FromInt(3),
			},
		},
		{
			name:      "reversed ids slice",
			str:       "1,2-5,6,7-10",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromInt(1),
				intstr.FromString("2-5"),
				intstr.FromInt(6),
				intstr.FromString("7-10"),
			},
		},
		{
			name:      "has space in the string",
			str:       "1, 2-3, 4",
			delimiter: ",",
			result: []intstr.IntOrString{
				intstr.FromInt(1),
				intstr.FromString("2-3"),
				intstr.FromInt(4),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := StringToIntStrSlice(test.str, test.delimiter)
			if len(actual) != len(test.result) {
				t.Errorf("expect length %v but got %v", len(test.result), len(actual))
				return
			}
			for i := range len(actual) {
				if test.result[i].String() != actual[i].String() {
					t.Errorf("index %d: expect %v but got %v", i, test.result[i], actual[i])
				}
			}
		})
	}
}
