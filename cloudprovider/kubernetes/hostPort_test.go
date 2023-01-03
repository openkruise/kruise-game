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

package kubernetes

import (
	"testing"
)

func TestSelectPorts(t *testing.T) {
	tests := []struct {
		amountStat []int
		portAmount map[int32]int
		num        int
		shouldIn   []int32
		index      int
	}{
		{
			amountStat: []int{8, 3},
			portAmount: map[int32]int{800: 0, 801: 0, 802: 0, 803: 1, 804: 0, 805: 1, 806: 0, 807: 0, 808: 1, 809: 0, 810: 0},
			num:        2,
			shouldIn:   []int32{800, 801, 802, 804, 806, 807, 809, 810},
			index:      0,
		},
	}

	for _, test := range tests {
		hostPorts, index := selectPorts(test.amountStat, test.portAmount, test.num)
		if index != test.index {
			t.Errorf("expect index %v but got %v", test.index, index)
		}

		for _, hostPort := range hostPorts {
			isIn := false
			for _, si := range test.shouldIn {
				if si == hostPort {
					isIn = true
					break
				}
			}
			if !isIn {
				t.Errorf("hostPort %d not in expect slice: %v", hostPort, test.shouldIn)
			}
		}
	}
}
