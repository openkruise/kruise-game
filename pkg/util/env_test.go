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
func TestGetGameServerConcurrentReconciles(t *testing.T) {
	tests := []struct {
		gameServerConcurrentReconciles string
		result                         int
	}{
		{
			gameServerConcurrentReconciles: "",
			result:                         10, // Default value
		},
		{
			gameServerConcurrentReconciles: "20",
			result:                         20,
		},
		{
			gameServerConcurrentReconciles: "invalid",
			result:                         10, // Default value for invalid input
		},
		{
			gameServerConcurrentReconciles: "0",
			result:                         10, // Default value for non-positive input
		},
		{
			gameServerConcurrentReconciles: "-5",
			result:                         10, // Default value for negative input
		},
	}
	defer os.Unsetenv("GAMESERVER_CONCURRENT_RECONCILES")
	for _, test := range tests {
		os.Setenv("GAMESERVER_CONCURRENT_RECONCILES", test.gameServerConcurrentReconciles)
		if GetGameServerConcurrentReconciles() != test.result {
			t.Errorf("expect %v but got %v", test.result, GetGameServerConcurrentReconciles())
		}
	}
}
