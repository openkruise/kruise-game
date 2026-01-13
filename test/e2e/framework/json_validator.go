package framework

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ValidateJSONLogs reads a log file line-by-line and validates each line is valid JSON
// with expected fields: time, level, severity_number, msg, and structured code attributes.
// Returns an error with the first invalid line number if validation fails.
func ValidateJSONLogs(logPath string) error {
	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", logPath, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	validLines := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip comment lines (pod headers from CollectManagerLogs)
		if len(line) > 0 && line[0] == '#' {
			continue
		}

		// Skip empty lines
		if len(line) == 0 {
			continue
		}

		validLines++

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			return fmt.Errorf("line %d is not valid JSON: %w\nLine content: %s", lineNum, err, line)
		}

		// Validate required fields
		requiredFields := []string{"time", "level", "severity_number", "msg"}
		for _, field := range requiredFields {
			if _, ok := logEntry[field]; !ok {
				return fmt.Errorf("line %d missing required field '%s'\nLine content: %s", lineNum, field, line)
			}
		}

		// Validate timestamp format
		timeValue, _ := logEntry["time"].(string)
		if _, err := time.Parse(time.RFC3339Nano, timeValue); err != nil {
			return fmt.Errorf("line %d has invalid time: %v\nLine content: %s", lineNum, err, line)
		}

		// Validate level formats
		level, _ := logEntry["level"].(string)
		if level != strings.ToUpper(level) {
			return fmt.Errorf("line %d level must be uppercase: %s\nLine content: %s", lineNum, level, line)
		}

		switch v := logEntry["severity_number"].(type) {
		case float64:
			if v <= 0 {
				return fmt.Errorf("line %d severity_number must be positive\nLine content: %s", lineNum, line)
			}
		case int:
			if v <= 0 {
				return fmt.Errorf("line %d severity_number must be positive\nLine content: %s", lineNum, line)
			}
		default:
			return fmt.Errorf("line %d severity_number must be numeric, got %T\nLine content: %s", lineNum, logEntry["severity_number"], line)
		}

		code, ok := logEntry["code"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("line %d missing code object\nLine content: %s", lineNum, line)
		}
		if _, ok := code["function"]; !ok {
			return fmt.Errorf("line %d code object missing 'function' field\nLine content: %s", lineNum, line)
		}
		if _, ok := code["filepath"]; !ok {
			return fmt.Errorf("line %d code object missing 'filepath' field\nLine content: %s", lineNum, line)
		}
		if _, ok := code["lineno"]; !ok {
			return fmt.Errorf("line %d code object missing 'lineno' field\nLine content: %s", lineNum, line)
		}

	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read log file %s: %w", logPath, err)
	}

	if validLines == 0 {
		return fmt.Errorf("log file %s is empty (no valid log lines)", logPath)
	}

	return nil
}
