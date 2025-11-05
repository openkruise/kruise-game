# Performance Testing for Logging

This directory contains scripts for performance testing of the logging component.

## High-Frequency Log Script

The `log_burst.go` script is used to generate a high volume of logs to test the performance of the logging system.

### Execution

To run the script, use the following command:

```bash
go run ./test/perf/log_burst.go
```

### Expected Output

The script will output a summary of the performance test, including:

-   Total number of logs generated
-   Time taken to generate the logs
-   Logs per second
-   CPU usage
-   Memory usage
