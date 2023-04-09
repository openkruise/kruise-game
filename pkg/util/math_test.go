package util

import "testing"

func TestMin(t *testing.T) {
	tests := []struct {
		a      int
		b      int
		result int
	}{
		{
			a:      1,
			b:      0,
			result: 0,
		},
	}

	for _, test := range tests {
		expect := test.result
		actual := Min(test.a, test.b)
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}
