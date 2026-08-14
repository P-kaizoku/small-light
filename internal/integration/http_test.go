package integration

// Full-stack HTTP tests assemble the real wiring (repositories -> cache ->
// services -> handlers -> chi router -> middleware) behind httptest and hit it
// over actual HTTP. This is the closest thing to the deployed binary without
// binding a port.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// apiClient is a tiny helper for authenticated HTTP calls against the stack.
type apiClient struct {
	base string
}

// do sends one request and returns the raw response for assertion.
func (c *apiClient) do(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := noRedirectClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// decodeBody decodes the response body into dst.
func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// registerLogin creates a user and returns its access token.
func registerLogin(t *testing.T, client *apiClient, email string) string {
	t.Helper()

	resp := client.do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"Email":    email,
		"Password": "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201", resp.StatusCode)
	}

	resp = client.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"Email":    email,
		"Password": "password123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Token string `json:"access_token"`
	}
	decodeBody(t, resp, &body)
	if body.Token == "" {
		t.Fatal("login returned an empty access_token")
	}
	return body.Token
}

type linkResp struct {
	ID          string `json:"id"`
	ShortCode   string `json:"short_code"`
	OriginalURL string `json:"original_url"`
	ClickCount  int    `json:"click_count"`
}

// TestFullFlow exercises the entire product surface in one story: register,
// login, shorten, list, get, redirect (cache miss), update (cache eviction),
// redirect (new target), analytics (post-worker flush), delete, and the 404
// after delete. It stays well under the 20/min redirect budget because every
// test starts from a freshly flushed Redis (see setup/clean).
func TestFullFlow(t *testing.T) {
	st := newStack(t)
	client := &apiClient{base: st.ts.URL}
	token := registerLogin(t, client, "flow@test.com")

	// 1. Create a short link.
	resp := client.do(t, http.MethodPost, "/api/v1/links", token, map[string]string{"url": "https://example.com"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201", resp.StatusCode)
	}
	var created linkResp
	decodeBody(t, resp, &created)
	if created.ShortCode == "" || created.OriginalURL != "https://example.com" {
		t.Fatalf("create returned %+v", created)
	}

	// 2. List contains it.
	resp = client.do(t, http.MethodGet, "/api/v1/links", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", resp.StatusCode)
	}
	var listed []linkResp
	decodeBody(t, resp, &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list returned %+v, want 1 link with id %s", listed, created.ID)
	}

	// 3. Get by id.
	resp = client.do(t, http.MethodGet, "/api/v1/links/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", resp.StatusCode)
	}

	// 4. Redirect hits (cache miss -> warmed).
	resp = client.do(t, http.MethodGet, "/"+created.ShortCode, "", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect: status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://example.com" {
		t.Errorf("Location = %q, want %q", loc, "https://example.com")
	}

	// 5. Update the URL, then redirect again — must go to the NEW target,
	// proving the cached entry was evicted by the PATCH.
	resp = client.do(t, http.MethodPatch, "/api/v1/links/"+created.ID, token, map[string]string{"url": "https://new.example.com"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: status = %d, want 200", resp.StatusCode)
	}
	resp = client.do(t, http.MethodGet, "/"+created.ShortCode, "", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect after update: status = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "https://new.example.com" {
		t.Errorf("Location after update = %q, want %q (cache was not evicted?)", loc, "https://new.example.com")
	}

	// 6. Flush the click worker, then analytics must show both redirects.
	flushClicks(t, st)
	resp = client.do(t, http.MethodGet, "/api/v1/links/"+created.ID+"/analytics", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("analytics: status = %d, want 200", resp.StatusCode)
	}
	var analytics struct {
		ClickCount int `json:"click_count"`
	}
	decodeBody(t, resp, &analytics)
	if analytics.ClickCount != 2 {
		t.Errorf("analytics click_count = %d, want 2 (two redirects flushed)", analytics.ClickCount)
	}

	// 7. Delete, then the redirect must 404 and the list must be empty.
	resp = client.do(t, http.MethodDelete, "/api/v1/links/"+created.ID, token, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", resp.StatusCode)
	}
	resp = client.do(t, http.MethodGet, "/"+created.ShortCode, "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("redirect after delete: status = %d, want 404", resp.StatusCode)
	}
	resp = client.do(t, http.MethodGet, "/api/v1/links", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list after delete: status = %d, want 200", resp.StatusCode)
	}
	var after []linkResp
	decodeBody(t, resp, &after)
	if len(after) != 0 {
		t.Errorf("list after delete returned %d links, want 0", len(after))
	}
}

// TestRedirectNotFound asserts an unknown short code 404s publicly (no auth).
func TestRedirectNotFound(t *testing.T) {
	st := newStack(t)
	client := &apiClient{base: st.ts.URL}

	resp := client.do(t, http.MethodGet, "/doesnotexist", "", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// TestAPIRequiresAuth asserts the links subtree rejects unauthenticated
// requests with 401 — the RequireAuth middleware must fire before any handler.
func TestAPIRequiresAuth(t *testing.T) {
	st := newStack(t)
	client := &apiClient{base: st.ts.URL}

	for _, path := range []string{"/api/v1/links", "/api/v1/links/some-id"} {
		resp := client.do(t, http.MethodGet, path, "", nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without token: status = %d, want 401", path, resp.StatusCode)
		}
	}
}

// TestLoginWrongPassword asserts bad credentials 401 without revealing the
// email — the enumeration-prevention contract at the HTTP layer.
func TestLoginWrongPassword(t *testing.T) {
	st := newStack(t)
	client := &apiClient{base: st.ts.URL}
	registerLogin(t, client, "auth@test.com")

	resp := client.do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"Email":    "auth@test.com",
		"Password": "wrongpassword",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with wrong password: status = %d, want 401", resp.StatusCode)
	}
}
