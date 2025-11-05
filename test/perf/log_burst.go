package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/openkruise/kruise-game/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	logOptions := logging.NewOptions()
	logOptions.AddFlags(flag.CommandLine)
	flag.Parse()

	result, err := logOptions.Apply(flag.CommandLine)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if result.Warning != "" {
		fmt.Fprintln(os.Stderr, result.Warning)
	}

	logger := zap.New(zap.UseFlagOptions(&logOptions.ZapOptions))

	numLogs := 100000
	startTime := time.Now()

	for i := 0; i < numLogs; i++ {
		logger.Info("This is a test log message", "iteration", i)
	}

	duration := time.Since(startTime)
	logsPerSecond := float64(numLogs) / duration.Seconds()

	fmt.Printf("Generated %d logs in %v\n", numLogs, duration)
	fmt.Printf("Logs per second: %.2f\n", logsPerSecond)
}
