package util

import (
	"k8s.io/klog/v2"
	"os"
	"strconv"
	"time"
)

func GetNetworkTotalWaitTime() time.Duration {
	networkTotalWaitTime := 60 * time.Second
	if num := os.Getenv("NETWORK_TOTAL_WAIT_TIME"); len(num) > 0 {
		if p, err := strconv.ParseInt(num, 10, 32); err == nil {
			networkTotalWaitTime = time.Duration(p) * time.Second
		} else {
			klog.Fatalf("failed to convert NETWORK_TOTAL_WAIT_TIME=%v in env: %v", p, err)
		}
	}
	return networkTotalWaitTime
}

func GetNetworkIntervalTime() time.Duration {
	networkIntervalTime := 5 * time.Second
	if num := os.Getenv("NETWORK_PROBE_INTERVAL_TIME"); len(num) > 0 {
		if p, err := strconv.ParseInt(num, 10, 32); err == nil {
			networkIntervalTime = time.Duration(p) * time.Second
		} else {
			klog.Fatalf("failed to convert NETWORK_PROBE_INTERVAL_TIME=%v in env: %v", p, err)
		}
	}
	return networkIntervalTime
}
