package middleware

import (
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
)

// Color codes for terminal output
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"
	White   = "\033[97m"
)

// ColorizedLogger returns a middleware that logs HTTP requests with colors
func ColorizedLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			
			// Process request
			err := next(c)
			if err != nil {
				c.Error(err)
			}
			
			// Calculate request duration
			latency := time.Since(start)
			
			// Get request details
			req := c.Request()
			res := c.Response()
			method := req.Method
			path := req.URL.Path
			status := res.Status
			
			// Color code based on HTTP method
			methodColor := getMethodColor(method)
			
			// Color code based on status
			statusColor := getStatusColor(status)
			
			// Format latency
			latencyStr := formatLatency(latency)
			latencyColor := getLatencyColor(latency)
			
			// Build log message
			fmt.Printf("%s%s%s %s%3d%s %s%-7s%s %s%s\n",
				Gray, start.Format("15:04:05"), Reset,
				statusColor, status, Reset,
				methodColor, method, Reset,
				latencyColor, latencyStr, Reset,
				path,
			)
			
			return err
		}
	}
}

// getMethodColor returns the color for HTTP method
func getMethodColor(method string) string {
	switch method {
	case "GET":
		return Cyan
	case "POST":
		return Green
	case "PUT":
		return Yellow
	case "DELETE":
		return Red
	case "PATCH":
		return Magenta
	case "HEAD":
		return Blue
	case "OPTIONS":
		return White
	default:
		return Reset
	}
}

// getStatusColor returns the color for HTTP status code
func getStatusColor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return Green
	case status >= 300 && status < 400:
		return Cyan
	case status >= 400 && status < 500:
		return Yellow
	case status >= 500:
		return Red
	default:
		return Reset
	}
}

// getLatencyColor returns the color for latency
func getLatencyColor(latency time.Duration) string {
	switch {
	case latency < 100*time.Millisecond:
		return Green
	case latency < 500*time.Millisecond:
		return Yellow
	case latency < 1*time.Second:
		return Magenta
	default:
		return Red
	}
}

// formatLatency formats the latency duration
func formatLatency(latency time.Duration) string {
	switch {
	case latency < time.Microsecond:
		return fmt.Sprintf("%3dns", latency.Nanoseconds())
	case latency < time.Millisecond:
		return fmt.Sprintf("%3dµs", latency.Microseconds())
	case latency < time.Second:
		return fmt.Sprintf("%3dms", latency.Milliseconds())
	default:
		return fmt.Sprintf("%.2fs", latency.Seconds())
	}
}
