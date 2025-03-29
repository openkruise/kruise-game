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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"golang.org/x/exp/constraints"
)

// see github.com/openkruise/kruise/pkg/util/api/asts.go

// ParseRange parses the start and end value from a string like "1-3"
func ParseRange(s string) (start int, end int, err error) {
	split := strings.Split(s, "-")
	if len(split) != 2 {
		return 0, 0, fmt.Errorf("invalid range %s", s)
	}
	start, err = strconv.Atoi(split[0])
	if err != nil {
		return
	}
	end, err = strconv.Atoi(split[1])
	if err != nil {
		return
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid range %s", s)
	}
	return
}

// GetReserveOrdinalIntSet returns a set of ints from parsed reserveOrdinal
func GetReserveOrdinalIntSet(r []intstr.IntOrString) sets.Set[int] {
	values := sets.New[int]()
	for _, elem := range r {
		if elem.Type == intstr.Int {
			values.Insert(int(elem.IntVal))
		} else {
			start, end, err := ParseRange(elem.StrVal)
			if err != nil {
				klog.ErrorS(err, "invalid range reserveOrdinal found, an empty slice will be returned", "reserveOrdinal", elem.StrVal)
				return nil
			}
			for i := start; i <= end; i++ {
				values.Insert(i)
			}
		}
	}
	return values
}

// StringToOrdinalIntSet convert a string to a set of ordinals,
// support ranged ordinals like "1-3,5-7,10"
// eg, "1, 2-5, 7" -> {1, 2, 3, 4, 5, 7}
func StringToOrdinalIntSet(str string, delimiter string) sets.Set[int] {
	ret := sets.New[int]()
	if str == "" {
		return ret
	}

	strList := strings.Split(str, delimiter)
	if len(strList) == 0 {
		return ret
	}

	for _, s := range strList {
		if strings.Contains(s, "-") {
			start, end, err := ParseRange(s)
			if err != nil {
				klog.ErrorS(err, "invalid range found, skip", "range", s)
				continue
			}
			for i := start; i <= end; i++ {
				ret.Insert(i)
			}
		} else {
			num, err := strconv.Atoi(s)
			if err != nil {
				klog.ErrorS(err, "invalid number found, skip", "number", s)
				continue
			}
			ret.Insert(num)
		}
	}

	return ret
}

// OrdinalSetToIntStrSlice convert a set of oridinals to a ranged intstr slice
// e.g. {1, 2, 3, 5, 6, 7, 10} -> ["1-3", "5-7", 10]
func OrdinalSetToIntStrSlice[T constraints.Integer](s sets.Set[T]) []intstr.IntOrString {
	var ret []intstr.IntOrString
	if s.Len() == 0 {
		return ret
	}

	// get all ordinals and sort them
	ordinals := s.UnsortedList()
	slices.Sort(ordinals)

	if len(ordinals) == 0 {
		return ret
	}

	var start, end T
	start = ordinals[0]
	end = start

	for i := 1; i < len(ordinals); i++ {
		if ordinals[i] == end+1 {
			end = ordinals[i]
			continue
		}

		// currnet ordinal is not continuous with previous one
		if start == end {
			ret = append(ret, intstr.FromInt(int(start)))
		} else {
			ret = append(ret, intstr.FromString(fmt.Sprintf("%d-%d", start, end)))
		}

		// reset start and end
		start = ordinals[i]
		end = start
	}

	// handle the last ordinal
	if start == end {
		ret = append(ret, intstr.FromInt(int(start)))
	} else {
		ret = append(ret, intstr.FromString(fmt.Sprintf("%d-%d", start, end)))
	}

	return ret
}

func OrdinalSetToString(s sets.Set[int], delimiter string) string {
	if s.Len() == 0 {
		return ""
	}
	// get all ordinals and sort them
	ss := OrdinalSetToIntStrSlice(s)
	ret := make([]string, 0, len(ss))
	for _, elem := range ss {
		if elem.Type == intstr.Int {
			ret = append(ret, strconv.Itoa(int(elem.IntVal)))
		} else {
			ret = append(ret, elem.StrVal)
		}
	}
	return strings.Join(ret, delimiter)
}

// GetSetInANotInB returns a set of elements that are in set a but not in set b
func GetSetInANotInB[T comparable](a, b sets.Set[T]) sets.Set[T] {
	ret := sets.New[T]()
	for elem := range a {
		if !b.Has(elem) {
			ret.Insert(elem)
		}
	}
	return ret
}

func InsertRange(s sets.Int, start, end int) {
	for i := start; i <= end; i++ {
		s.Insert(i)
	}
}
