package middleware

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The request log format had eleven verbs for thirteen arguments, so Go
// appended "%!(EXTRA ...)" -- and because the newline is inside the format,
// the path ended up on the following line with no terminator, corrupting the
// next entry as well. This asserts the arity stays in step.
func TestRequestLogFormat(t *testing.T) {
	const path = "/api/v1/counties/Franklin"

	line := fmt.Sprintf(requestLogFormat,
		Gray, time.Date(2026, 9, 8, 10, 23, 45, 0, time.UTC).Format("15:04:05"), Reset,
		Green, 200, Reset,
		Blue, "GET", Reset,
		Yellow, "1.2ms", Reset,
		path,
	)

	if strings.Contains(line, "%!") {
		t.Fatalf("format/argument mismatch: %q", line)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Errorf("line does not end in a newline: %q", line)
	}
	if strings.Count(line, "\n") != 1 {
		t.Errorf("want exactly one newline, got %d: %q", strings.Count(line, "\n"), line)
	}
	if !strings.Contains(line, path) {
		t.Errorf("path missing from log line: %q", line)
	}
	// The path must follow the newline-free body, not sit after it.
	if idx := strings.Index(line, path); idx > strings.Index(line, "\n") {
		t.Errorf("path appears after the newline: %q", line)
	}

	// Colour must be reset before the path, or it bleeds into later output.
	before := line[:strings.Index(line, path)]
	if !strings.HasSuffix(strings.TrimSuffix(before, " "), Reset) {
		t.Errorf("colour not reset before the path: %q", line)
	}
}
