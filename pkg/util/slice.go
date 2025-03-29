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
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
)

func IsNumInList(num int, list []int) bool {
	for _, n := range list {
		if num == n {
			return true
		}
	}
	return false
}

func IsStringInList(str string, list []string) bool {
	for _, s := range list {
		if s == str {
			return true
		}
	}
	return false
}

func GetSliceInANotInB(a, b []int) []int {
	var ret []int
	for _, aa := range a {
		if !IsNumInList(aa, b) {
			ret = append(ret, aa)
		}
	}
	return ret
}

func GetSliceInAandInB(a, b []int) []int {
	var ret []int
	for _, aa := range a {
		if IsNumInList(aa, b) {
			ret = append(ret, aa)
		}
	}
	return ret
}

func IntSliceToString(number []int, delimiter string) string {
	return strings.Trim(strings.Replace(fmt.Sprint(number), " ", delimiter, -1), "[]")
}

func Int32SliceToString(number []int32, delimiter string) string {
	return strings.Trim(strings.Replace(fmt.Sprint(number), " ", delimiter, -1), "[]")
}

func StringToIntSlice(str string, delimiter string) []int {
	if str == "" {
		return nil
	}
	strList := strings.Split(str, delimiter)
	if len(strList) == 0 {
		return nil
	}
	var retSlice []int
	for _, item := range strList {
		if item == "" {
			continue
		}
		val, err := strconv.Atoi(item)
		if err != nil {
			continue
		}
		retSlice = append(retSlice, val)
	}
	return retSlice
}

func StringToIntStrSlice(str string, delimiter string) []intstr.IntOrString {
	if str == "" || delimiter == "" {
		return nil
	}
	strList := strings.Split(str, delimiter)
	if len(strList) == 0 {
		return nil
	}
	var retSlice []intstr.IntOrString
	for _, item := range strList {
		if item == "" {
			continue
		}
		val, err := strconv.Atoi(item)
		if err != nil {
			retSlice = append(retSlice, intstr.FromString(strings.TrimSpace(item)))
		} else {
			retSlice = append(retSlice, intstr.FromInt(val))
		}
	}
	return retSlice
}

func StringToInt32Slice(str string, delimiter string) []int32 {
	if str == "" {
		return nil
	}
	strList := strings.Split(str, delimiter)
	if len(strList) == 0 {
		return nil
	}
	var retSlice []int32
	for _, item := range strList {
		if item == "" {
			continue
		}
		val, err := strconv.ParseInt(item, 10, 32)
		if err != nil {
			continue
		}
		retSlice = append(retSlice, int32(val))
	}
	return retSlice
}

func IsSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Ints(a)
	sort.Ints(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func RemoveRepeat(nums []int) []int {
	var result []int
	tempMap := map[int]byte{}
	for _, num := range nums {
		beforeLen := len(tempMap)
		tempMap[num] = 0
		if len(tempMap) == beforeLen {
			continue
		}
		result = append(result, num)
	}
	return result
}

func IsRepeat(nums []int) bool {
	tempMap := map[int]byte{}
	for _, num := range nums {
		beforeLen := len(tempMap)
		tempMap[num] = 0
		if len(tempMap) == beforeLen {
			return true
		}
	}
	return false
}

func IsHasNegativeNum(nums []int) bool {
	for _, num := range nums {
		if num < 0 {
			return true
		}
	}
	return false
}
