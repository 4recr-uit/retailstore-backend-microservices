package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestNewError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		status     int
		errMessage string
	}{
		{
			name:       "400 Bad Request",
			status:     http.StatusBadRequest,
			errMessage: "invalid parameter",
		},
		{
			name:       "404 Not Found",
			status:     http.StatusNotFound,
			errMessage: "resource not found",
		},
		{
			name:       "500 Internal Server Error",
			status:     http.StatusInternalServerError,
			errMessage: "unexpected server failure",
		},
		{
			name:       "503 Service Unavailable",
			status:     http.StatusServiceUnavailable,
			errMessage: "search service is not available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			NewError(c, tt.status, errors.New(tt.errMessage))

			assert.Equal(t, tt.status, w.Code)

			var httpErr HTTPError
			err := json.Unmarshal(w.Body.Bytes(), &httpErr)
			assert.NoError(t, err)
			assert.Equal(t, tt.status, httpErr.Code)
			assert.Equal(t, tt.errMessage, httpErr.Message)
		})
	}
}
