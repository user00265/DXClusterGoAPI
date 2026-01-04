package frontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/user00265/dxclustergoapi/spot"
)

type mockCache struct {
	spots []spot.Spot
}

func (m *mockCache) GetAllSpots() []spot.Spot {
	return m.spots
}

func (m *mockCache) AddSpot(newSpot spot.Spot) bool {
	m.spots = append(m.spots, newSpot)
	return true
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	// Path normalization middleware
	router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		originalPath := path

		// Collapse multiple slashes
		for strings.Contains(path, "//") {
			path = strings.ReplaceAll(path, "//", "/")
		}

		// Remove trailing slash (except for root)
		if len(path) > 1 && strings.HasSuffix(path, "/") {
			path = strings.TrimSuffix(path, "/")
		}

		// If path was modified, we need to re-route the request
		if path != originalPath {
			c.Request.URL.Path = path
			router.HandleContext(c)
			c.Abort()
			return
		}

		c.Next()
	})

	cache := &mockCache{
		spots: []spot.Spot{
			{Spotted: "TEST1", Band: "20m"},
			{Spotted: "TEST2", Band: "40m"},
		},
	}

	SetupRoutes(router.Group("/"), cache, nil, nil)
	return router
}

func TestSpotsEndpointVariations(t *testing.T) {
	router := setupTestRouter()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"spots without slash", "/spots", http.StatusOK},
		{"spots with trailing slash", "/spots/", http.StatusOK},
		{"spots with double slash", "//spots", http.StatusOK},
		{"spots with double slash and trailing", "//spots/", http.StatusOK},
		{"spots with multiple slashes", "///spots", http.StatusOK},
		{"spots with multiple slashes and trailing", "///spots/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("GET %s returned status %d, want %d. Body: %s",
					tt.path, w.Code, tt.wantStatus, w.Body.String())
			}

			// Verify no redirect
			if w.Code >= 300 && w.Code < 400 {
				t.Errorf("GET %s returned redirect %d, should return 200", tt.path, w.Code)
			}
		})
	}
}
