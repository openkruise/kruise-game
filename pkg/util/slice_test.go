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

import "testing"

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
	}

	for _, test := range tests {
		actual := IsSliceEqual(test.a, test.b)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
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
