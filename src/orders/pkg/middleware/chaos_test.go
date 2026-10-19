package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func setupTestRouter() (*gin.Engine, *ChaosController) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cc := NewChaosController()
	r.Use(cc.ChaosMiddleware())
	cc.SetupChaosRoutes(r)

	r.GET("/normal", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	return r, cc
}

func TestChaosController_DefaultState(t *testing.T) {
	_, cc := setupTestRouter()

	if !cc.IsHealthy() {
		t.Errorf("expected default IsHealthy() to be true")
	}
	if cc.isLatencyOn {
		t.Errorf("expected default isLatencyOn to be false")
	}
	if cc.latency != 0 {
		t.Errorf("expected default latency to be 0, got %v", cc.latency)
	}
	if cc.isErrorStatusOn {
		t.Errorf("expected default isErrorStatusOn to be false")
	}
	if cc.errorStatus != 0 {
		t.Errorf("expected default errorStatus to be 0, got %d", cc.errorStatus)
	}
}

func TestChaosController_Latency(t *testing.T) {
	r, cc := setupTestRouter()

	// 1. Invalid latency: negative value
	req, _ := http.NewRequest(http.MethodPost, "/chaos/latency/-5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative latency, got %d", w.Code)
	}

	// 2. Invalid latency: non-integer
	req, _ = http.NewRequest(http.MethodPost, "/chaos/latency/abc", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-integer latency, got %d", w.Code)
	}

	// 3. Valid latency: 20ms
	req, _ = http.NewRequest(http.MethodPost, "/chaos/latency/20", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid latency, got %d", w.Code)
	}
	if !cc.isLatencyOn || cc.latency != 20*time.Millisecond {
		t.Errorf("expected latency 20ms, got isLatencyOn=%v latency=%v", cc.isLatencyOn, cc.latency)
	}

	// 4. Verify /chaos/* routes bypass latency delay
	chaosStart := time.Now()
	req, _ = http.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	chaosElapsed := time.Since(chaosStart)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /chaos/status, got %d", w.Code)
	}
	if chaosElapsed >= 20*time.Millisecond {
		t.Errorf("expected /chaos/* route to bypass latency, but took %v", chaosElapsed)
	}

	// 5. Verify normal route experiences latency delay
	normalStart := time.Now()
	req, _ = http.NewRequest(http.MethodGet, "/normal", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	normalElapsed := time.Since(normalStart)
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Errorf("expected 200 'ok' from normal route, got code=%d body=%s", w.Code, w.Body.String())
	}
	if normalElapsed < 18*time.Millisecond {
		t.Errorf("expected normal route to take >= 18ms due to latency, took %v", normalElapsed)
	}

	// 6. Disable latency: DELETE /chaos/latency
	req, _ = http.NewRequest(http.MethodDelete, "/chaos/latency", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for DELETE /chaos/latency, got %d", w.Code)
	}
	if cc.isLatencyOn {
		t.Errorf("expected isLatencyOn to be false after disable")
	}

	// Verify normal route is fast again
	fastStart := time.Now()
	req, _ = http.NewRequest(http.MethodGet, "/normal", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	fastElapsed := time.Since(fastStart)
	if fastElapsed >= 15*time.Millisecond {
		t.Errorf("expected normal route to be fast after disabling latency, took %v", fastElapsed)
	}
}

func TestChaosController_ErrorStatus(t *testing.T) {
	r, cc := setupTestRouter()

	// 1. Invalid status code < 100
	req, _ := http.NewRequest(http.MethodPost, "/chaos/status/99", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for status 99, got %d", w.Code)
	}

	// 2. Invalid status code > 599
	req, _ = http.NewRequest(http.MethodPost, "/chaos/status/600", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for status 600, got %d", w.Code)
	}

	// 3. Non-numeric status code
	req, _ = http.NewRequest(http.MethodPost, "/chaos/status/bad", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for status 'bad', got %d", w.Code)
	}

	// 4. Valid status code: 502
	req, _ = http.NewRequest(http.MethodPost, "/chaos/status/502", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid status code, got %d", w.Code)
	}
	if !cc.isErrorStatusOn || cc.errorStatus != 502 {
		t.Errorf("expected isErrorStatusOn=true errorStatus=502, got %v and %d", cc.isErrorStatusOn, cc.errorStatus)
	}

	// 5. Verify /chaos/* routes bypass error status injection
	req, _ = http.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /chaos/* route to bypass error status and return 200, got %d", w.Code)
	}

	// 6. Verify normal routes are aborted with 502
	req, _ = http.NewRequest(http.MethodGet, "/normal", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected normal route to return 502 Bad Gateway, got %d", w.Code)
	}

	// 7. Disable error status: DELETE /chaos/status
	req, _ = http.NewRequest(http.MethodDelete, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for DELETE /chaos/status, got %d", w.Code)
	}
	if cc.isErrorStatusOn {
		t.Errorf("expected isErrorStatusOn to be false after disable")
	}

	// Normal route succeeds again
	req, _ = http.NewRequest(http.MethodGet, "/normal", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected normal route to return 200 after disable, got %d", w.Code)
	}
}

func TestChaosController_HealthAndStatus(t *testing.T) {
	r, cc := setupTestRouter()

	// Initial health
	if !cc.IsHealthy() {
		t.Errorf("expected initial health to be true")
	}

	// Disable health: POST /chaos/health
	req, _ := http.NewRequest(http.MethodPost, "/chaos/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for POST /chaos/health, got %d", w.Code)
	}
	if cc.IsHealthy() {
		t.Errorf("expected IsHealthy() to be false after POST /chaos/health")
	}

	// Check GET /chaos/status when unhealthy
	req, _ = http.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /chaos/status, got %d", w.Code)
	}

	var statusResp struct {
		Latency struct {
			Enabled bool  `json:"enabled"`
			Value   int64 `json:"value"`
		} `json:"latency"`
		ErrorStatus struct {
			Enabled bool `json:"error_status"` // or "enabled"
			Code    int  `json:"code"`
		} `json:"error_status"`
		Healthy bool `json:"healthy"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to unmarshal /chaos/status: %v", err)
	}
	if statusResp.Healthy != false {
		t.Errorf("expected healthy=false in /chaos/status response, got %v", statusResp.Healthy)
	}

	// Restore health: DELETE /chaos/health
	req, _ = http.NewRequest(http.MethodDelete, "/chaos/health", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for DELETE /chaos/health, got %d", w.Code)
	}
	if !cc.IsHealthy() {
		t.Errorf("expected IsHealthy() to be true after DELETE /chaos/health")
	}

	// Check GET /chaos/status after restoring health
	req, _ = http.NewRequest(http.MethodGet, "/chaos/status", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("failed to unmarshal /chaos/status: %v", err)
	}
	if statusResp.Healthy != true {
		t.Errorf("expected healthy=true in /chaos/status response, got %v", statusResp.Healthy)
	}
}
