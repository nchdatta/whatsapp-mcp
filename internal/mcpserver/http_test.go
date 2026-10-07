package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthorize(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	var reached string
	h := authorize(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = r.URL.Path
	}))
	cases := []struct {
		path, auth string
		want       int
	}{
		{"/mcp/" + token, "", 200},
		{"/mcp", "Bearer " + token, 200},
		{"/mcp", "", 401},
		{"/mcp", "Bearer wrong", 401},
		{"/mcp/wrong", "", 401},
		{"/mcp/" + token + "x", "", 401},
		{"/", "", 404},
		{"/" + token, "", 404},
	}
	for _, c := range cases {
		reached = ""
		req := httptest.NewRequest("POST", c.path, nil)
		if c.auth != "" {
			req.Header.Set("Authorization", c.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %q: got %d, want %d", c.path, c.auth, rec.Code, c.want)
		}
		if c.want == 200 && reached != "/mcp" {
			t.Errorf("%s: handler saw path %q", c.path, reached)
		}
	}
}
