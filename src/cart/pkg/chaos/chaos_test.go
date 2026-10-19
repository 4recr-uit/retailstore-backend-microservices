package chaos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestRouter() (*Controller, *gin.Engine) {
	cc := NewController()
	r := gin.New()
	r.Use(cc.Middleware())
	cc.RegisterRoutes(r)

	// Sample business route for testing middleware behavior
	r.GET("/target", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	return cc, r
}

func TestChaosControllerHealth(t *testing.T) {
	cc, r := setupTestRouter()

	// Default IsHealthy() is true
	assert.True(t, cc.IsHealthy())

	// POST /chaos/health disables health
	req := httptest.NewRequest(http.MethodPost, "/chaos/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, cc.IsHealthy())

	// DELETE /chaos/health restores health
	req = httptest.NewRequest(http.MethodDelete, "/chaos/health", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, cc.IsHealthy())
}

func TestChaosLatency(t *testing.T) {
	_, r := setupTestRouter()

	// 1. Negative value -> 400 Bad Request
	req := httptest.NewRequest(http.MethodPost, "/chaos/latency/-10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 2. Non-integer value -> 400 Bad Request
	req = httptest.NewRequest(http.MethodPost, "/chaos/latency/abc", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Valid latency value: set 50ms
	req = httptest.NewRequest(http.MethodPost, "/chaos/latency/50", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 4. Verify middleware delays non-/chaos requests
	start := time.Now()
	req = httptest.NewRequest(http.MethodGet, "/target", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond)

	// 5. Verify middleware bypasses /chaos/* requests without delay
	start = time.Now()
	req = httptest.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	elapsedChaos := time.Since(start)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Less(t, elapsedChaos, 35*time.Millisecond)

	// Check status payload contains latency info
	var statusResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &statusResp)
	require.NoError(t, err)
	latencyMap := statusResp["latency"].(map[string]interface{})
	assert.Equal(t, true, latencyMap["enabled"])
	assert.Equal(t, float64(50), latencyMap["value"])

	// 6. DELETE /chaos/latency disables latency
	req = httptest.NewRequest(http.MethodDelete, "/chaos/latency", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request to /target should now be fast
	start = time.Now()
	req = httptest.NewRequest(http.MethodGet, "/target", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	elapsedFast := time.Since(start)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Less(t, elapsedFast, 35*time.Millisecond)

	// Status shows latency disabled
	req = httptest.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	err = json.Unmarshal(w.Body.Bytes(), &statusResp)
	require.NoError(t, err)
	latencyMap = statusResp["latency"].(map[string]interface{})
	assert.Equal(t, false, latencyMap["enabled"])
	assert.Equal(t, float64(0), latencyMap["value"])
}

func TestChaosErrorStatus(t *testing.T) {
	_, r := setupTestRouter()

	// 1. Non-integer code -> 400 Bad Request
	req := httptest.NewRequest(http.MethodPost, "/chaos/status/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 2. Out-of-range code < 100 -> 400 Bad Request
	req = httptest.NewRequest(http.MethodPost, "/chaos/status/99", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Out-of-range code > 599 -> 400 Bad Request
	req = httptest.NewRequest(http.MethodPost, "/chaos/status/600", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 4. Valid error status: 503
	req = httptest.NewRequest(http.MethodPost, "/chaos/status/503", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 5. Middleware aborts non-/chaos requests with 503
	req = httptest.NewRequest(http.MethodGet, "/target", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// 6. Middleware allows /chaos/status requests
	req = httptest.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var statusResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &statusResp)
	require.NoError(t, err)
	errMap := statusResp["error_status"].(map[string]interface{})
	assert.Equal(t, true, errMap["enabled"])
	assert.Equal(t, float64(503), errMap["code"])

	// 7. DELETE /chaos/status disables error status
	req = httptest.NewRequest(http.MethodDelete, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// /target now returns 200 OK again
	req = httptest.NewRequest(http.MethodGet, "/target", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
