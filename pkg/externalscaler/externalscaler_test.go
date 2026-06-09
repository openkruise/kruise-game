package externalscaler

import (
	"fmt"
	"math"
	"strconv"
	"testing"
)

func TestParseWTBDThreshold(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantThresh float64
		wantPct    bool
		wantErr    bool
	}{
		{
			name:       "valid absolute integer",
			input:      "10",
			wantThresh: 10,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "valid absolute integer 1",
			input:      "1",
			wantThresh: 1,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.1",
			input:      "0.1",
			wantThresh: 0.1,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.5",
			input:      "0.5",
			wantThresh: 0.5,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:       "valid percentage 0.99",
			input:      "0.99",
			wantThresh: 0.99,
			wantPct:    true,
			wantErr:    false,
		},
		{
			name:    "invalid - zero",
			input:   "0",
			wantErr: true,
		},
		{
			name:    "invalid - negative",
			input:   "-5",
			wantErr: true,
		},
		{
			name:    "invalid - not a number",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "invalid - empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:       "float >= 1 treated as absolute",
			input:      "3.7",
			wantThresh: 4, // math.Ceil(3.7)
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "1.0 treated as absolute 1",
			input:      "1.0",
			wantThresh: 1,
			wantPct:    false,
			wantErr:    false,
		},
		{
			name:       "large absolute value",
			input:      "100",
			wantThresh: 100,
			wantPct:    false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotThresh, gotPct, err := parseWTBDThreshold(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseWTBDThreshold(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if gotPct != tt.wantPct {
				t.Errorf("parseWTBDThreshold(%q) isPercentage = %v, want %v", tt.input, gotPct, tt.wantPct)
			}
			if math.Abs(gotThresh-tt.wantThresh) > 1e-9 {
				t.Errorf("parseWTBDThreshold(%q) threshold = %v, want %v", tt.input, gotThresh, tt.wantThresh)
			}
		})
	}
}

func TestHandleMinNum(t *testing.T) {
	tests := []struct {
		name      string
		totalNum  int
		noneNum   int
		minNumStr string
		wantMin   int
		wantErr   bool
	}{
		{
			name:      "invalid minNumStr - not a number",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "abc",
			wantMin:   0,
			wantErr:   true,
		},
		{
			name:      "empty minNumStr - no scale up needed",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - delta <= 0, no scale up needed",
			totalNum:  10,
			noneNum:   5,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - delta > 0, scale up needed",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "0.5",
			wantMin:   8,
			wantErr:   false,
		},
		{
			name:      "percentage - delta > 0, minNum > totalNum",
			totalNum:  5,
			noneNum:   1,
			minNumStr: "0.8",
			wantMin:   16,
			wantErr:   false,
		},
		{
			name:      "percentage - exact match, no scale up",
			totalNum:  20,
			noneNum:   10,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "percentage - slightly below, scale up by 1",
			totalNum:  19,
			noneNum:   9,
			minNumStr: "0.5",
			wantMin:   10,
			wantErr:   false,
		},
		{
			name:      "integer - minNum >= 1",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "3",
			wantMin:   3,
			wantErr:   false,
		},
		{
			name:      "integer - minNum >= 1, float string",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "3.1",
			wantMin:   4,
			wantErr:   false,
		},
		{
			name:      "integer - minNum is 1",
			totalNum:  10,
			noneNum:   0,
			minNumStr: "1",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "invalid n - zero",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "0",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "invalid n - negative",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "-1",
			wantMin:   0,
			wantErr:   true,
		},
		{
			name:      "invalid n - percentage >= 1 (e.g. 1.0 treated as integer 1)",
			totalNum:  10,
			noneNum:   2,
			minNumStr: "1.0",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 0, noneNum is 0",
			totalNum:  0,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   0,
			wantErr:   false,
		},
		{
			name:      "integer - totalNum is 0, noneNum is 0",
			totalNum:  0,
			noneNum:   0,
			minNumStr: "5",
			wantMin:   5,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 1, noneNum is 0, minNumStr 0.5",
			totalNum:  1,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   1,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum is 2, noneNum is 0, minNumStr 0.5",
			totalNum:  2,
			noneNum:   0,
			minNumStr: "0.5",
			wantMin:   2,
			wantErr:   false,
		},
		{
			name:      "percentage - totalNum 100, noneNum 10, minNumStr 0.2",
			totalNum:  100,
			noneNum:   10,
			minNumStr: "0.2",
			wantMin:   23,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMin, err := handleMinNum(tt.totalNum, tt.noneNum, tt.minNumStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("handleMinNum() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				// If wantErr is true, we don't need to check gotMin
				return
			}
			if gotMin != tt.wantMin {
				// For debugging float calculations
				if n, parseErr := strconv.ParseFloat(tt.minNumStr, 32); parseErr == nil && n > 0 && n < 1 {
					delta := (float64(tt.totalNum)*n - float64(tt.noneNum)) / (1 - n)
					fmt.Printf("Debug for %s: totalNum=%d, noneNum=%d, minNumStr=%s, n=%f, delta=%f, ceil(delta)=%f, calculatedMinNum=%d\n",
						tt.name, tt.totalNum, tt.noneNum, tt.minNumStr, n, delta, math.Ceil(delta), int(math.Ceil(delta))+tt.noneNum)
				}
				t.Errorf("handleMinNum() = %v, want %v", gotMin, tt.wantMin)
			}
		})
	}
}
