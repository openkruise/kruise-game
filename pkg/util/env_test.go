package util

import (
	"os"
	"testing"
	"time"
)

func TestGetNetworkTotalWaitTime(t *testing.T) {
	tests := []struct {
		networkTotalWaitTime string
		result               time.Duration
	}{
		{
			networkTotalWaitTime: "60",
			result:               60 * time.Second,
		},
	}

	for _, test := range tests {
		os.Setenv("NETWORK_TOTAL_WAIT_TIME", test.networkTotalWaitTime)
		if GetNetworkTotalWaitTime() != test.result {
			t.Errorf("expect %v but got %v", test.result, GetNetworkTotalWaitTime())
		}
	}
}

func TestGetNetworkIntervalTime(t *testing.T) {
	tests := []struct {
		networkIntervalTime string
		result              time.Duration
	}{
		{
			networkIntervalTime: "5",
			result:              5 * time.Second,
		},
	}

	for _, test := range tests {
		os.Setenv("NETWORK_PROBE_INTERVAL_TIME", test.networkIntervalTime)
		if GetNetworkIntervalTime() != test.result {
			t.Errorf("expect %v but got %v", test.result, GetNetworkIntervalTime())
		}
	}
}
