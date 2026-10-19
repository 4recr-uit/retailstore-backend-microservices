package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type ChaosController struct {
	mu              sync.RWMutex
	latency         time.Duration
	errorStatus     int
	isLatencyOn     bool
	isErrorStatusOn bool
	isHealthy       bool
}

func NewChaosController() *ChaosController {
	return &ChaosController{
		latency:         0,
		errorStatus:     0,
		isLatencyOn:     false,
		isErrorStatusOn: false,
		isHealthy:       true,
	}
}

// ChaosMiddleware applies latency or error status to requests
func (cc *ChaosController) ChaosMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isChaosControlPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		cc.mu.RLock()
		latencyOn := cc.isLatencyOn
		latency := cc.latency
		errorStatusOn := cc.isErrorStatusOn
		errorStatus := cc.errorStatus
		cc.mu.RUnlock()

		if latencyOn && latency > 0 {
			time.Sleep(latency)
		}

		if errorStatusOn && errorStatus > 0 {
			c.AbortWithStatus(errorStatus)
			return
		}

		c.Next()
	}
}

// SetupChaosRoutes registers chaos endpoints on the engine
func (cc *ChaosController) SetupChaosRoutes(r *gin.Engine) {
	chaos := r.Group("/chaos")
	{
		chaos.POST("/latency/:ms", cc.setLatency)
		chaos.DELETE("/latency", cc.disableLatency)

		chaos.POST("/status/:code", cc.setErrorStatus)
		chaos.DELETE("/status", cc.disableErrorStatus)

		chaos.GET("/status", cc.getChaosStatus)

		chaos.POST("/health", cc.setHealth)
		chaos.DELETE("/health", cc.disableHealth)
	}
}

func (cc *ChaosController) setLatency(c *gin.Context) {
	ms := c.Param("ms")
	latency, err := strconv.Atoi(ms)
	if err != nil || latency < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid latency value. Please provide a positive integer in milliseconds.",
		})
		return
	}

	cc.mu.Lock()
	cc.latency = time.Duration(latency) * time.Millisecond
	cc.isLatencyOn = true
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Latency set to " + ms + "ms",
	})
}

func (cc *ChaosController) setErrorStatus(c *gin.Context) {
	code := c.Param("code")
	status, err := strconv.Atoi(code)
	if err != nil || status < 100 || status > 599 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid HTTP status code. Please provide a valid status code (100-599).",
		})
		return
	}

	cc.mu.Lock()
	cc.errorStatus = status
	cc.isErrorStatusOn = true
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Error status code set to " + code,
	})
}

func (cc *ChaosController) setHealth(c *gin.Context) {
	cc.mu.Lock()
	cc.isHealthy = false
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Health check endpoint disabled",
	})
}

func (cc *ChaosController) disableLatency(c *gin.Context) {
	cc.mu.Lock()
	cc.isLatencyOn = false
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Latency disabled",
	})
}

func (cc *ChaosController) disableErrorStatus(c *gin.Context) {
	cc.mu.Lock()
	cc.isErrorStatusOn = false
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Error status disabled",
	})
}

func (cc *ChaosController) disableHealth(c *gin.Context) {
	cc.mu.Lock()
	cc.isHealthy = true
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Health endpoint enabled",
	})
}

func (cc *ChaosController) getChaosStatus(c *gin.Context) {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"latency": map[string]interface{}{
			"enabled": cc.isLatencyOn,
			"value":   cc.latency.Milliseconds(),
		},
		"error_status": map[string]interface{}{
			"enabled": cc.isErrorStatusOn,
			"code":    cc.errorStatus,
		},
		"healthy": cc.isHealthy,
	})
}

func (cc *ChaosController) IsHealthy() bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.isHealthy
}

func isChaosControlPath(path string) bool {
	return len(path) >= 6 && path[:6] == "/chaos"
}
