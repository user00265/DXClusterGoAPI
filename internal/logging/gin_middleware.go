package logging

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// GinLogger returns a gin.HandlerFunc middleware that logs requests using our logger
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Get status code
		statusCode := c.Writer.Status()

		// Build the query string if present
		if raw != "" {
			path = path + "?" + raw
		}

		// Determine log level based on status code
		msg := fmt.Sprintf("%s %s - %d (%v) - %s",
			c.Request.Method,
			path,
			statusCode,
			latency,
			c.ClientIP(),
		)

		if statusCode >= 500 {
			Error("%s", msg)
		} else if statusCode >= 400 {
			Warn("%s", msg)
		} else {
			Debug("%s", msg) // HTTP requests are debug level
		}
	}
}

// GinRecovery returns a gin.HandlerFunc middleware that recovers from panics
func GinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				Crit("PANIC recovered: %v", err)
				c.AbortWithStatus(500)
			}
		}()
		c.Next()
	}
}
