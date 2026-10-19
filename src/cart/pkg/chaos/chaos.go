package chaos

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Controller manages dynamic chaos testing rules (latency, errors, health toggle).
type Controller struct {
	mu              sync.RWMutex
	latency         time.Duration
	errorStatus     int
	isLatencyOn     bool
	isErrorStatusOn bool
	isHealthy       bool
}

// NewController creates and initializes a Controller.
func NewController() *Controller {
	return &Controller{
		latency:         0,
		errorStatus:     0,
		isLatencyOn:     false,
		isErrorStatusOn: false,
		isHealthy:       true,
	}
}

// Middleware returns a Gin middleware injecting configured latency or errors.
func (cc *Controller) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/chaos") {
			c.Next()
			return
		}

		cc.mu.RLock()
		latencyOn := cc.isLatencyOn
		lat := cc.latency
		errOn := cc.isErrorStatusOn
		errCode := cc.errorStatus
		cc.mu.RUnlock()

		if latencyOn && lat > 0 {
			time.Sleep(lat)
		}

		if errOn && errCode > 0 {
			c.AbortWithStatus(errCode)
			return
		}

		c.Next()
	}
}

// RegisterRoutes registers the /chaos management endpoints.
func (cc *Controller) RegisterRoutes(r *gin.Engine) {
	chaosGroup := r.Group("/chaos")
	{
		chaosGroup.POST("/latency/:ms", cc.setLatency)
		chaosGroup.DELETE("/latency", cc.disableLatency)

		chaosGroup.POST("/status/:code", cc.setErrorStatus)
		chaosGroup.DELETE("/status", cc.disableErrorStatus)

		chaosGroup.GET("/status", cc.getChaosStatus)

		chaosGroup.POST("/health", cc.setUnhealthy)
		chaosGroup.DELETE("/health", cc.setHealthy)
	}
}

func (cc *Controller) setLatency(c *gin.Context) {
	msParam := c.Param("ms")
	latency, err := strconv.Atoi(msParam)
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
		"message": "Latency set to " + msParam + "ms",
	})
}

func (cc *Controller) disableLatency(c *gin.Context) {
	cc.mu.Lock()
	cc.isLatencyOn = false
	cc.latency = 0
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Latency disabled",
	})
}

func (cc *Controller) setErrorStatus(c *gin.Context) {
	codeParam := c.Param("code")
	code, err := strconv.Atoi(codeParam)
	if err != nil || code < 100 || code > 599 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid HTTP status code. Please provide a valid status code (100-599).",
		})
		return
	}

	cc.mu.Lock()
	cc.errorStatus = code
	cc.isErrorStatusOn = true
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Error status code set to " + codeParam,
	})
}

func (cc *Controller) disableErrorStatus(c *gin.Context) {
	cc.mu.Lock()
	cc.isErrorStatusOn = false
	cc.errorStatus = 0
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Error status disabled",
	})
}

func (cc *Controller) setUnhealthy(c *gin.Context) {
	cc.mu.Lock()
	cc.isHealthy = false
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Health check endpoint disabled",
	})
}

func (cc *Controller) setHealthy(c *gin.Context) {
	cc.mu.Lock()
	cc.isHealthy = true
	cc.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": "Health endpoint enabled",
	})
}

func (cc *Controller) getChaosStatus(c *gin.Context) {
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

// IsHealthy returns true if health checks are enabled and not disabled via chaos.
func (cc *Controller) IsHealthy() bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.isHealthy
}
