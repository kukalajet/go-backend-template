// Command smoke-api exercises the running API through its published port.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

type errorBody struct {
	Error struct {
		Code   string   `json:"code"`
		Fields []string `json:"fields"`
	} `json:"error"`
}

type user struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
}

type client struct {
	baseURL string
	http    *http.Client
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:8000", "API address")
	email := flag.String("email", "smoke@example.com", "email of the user to create")
	flag.Parse()

	c := client{baseURL: *baseURL, http: &http.Client{Timeout: 10 * time.Second}}
	if err := run(c, *email); err != nil {
		fmt.Fprintln(os.Stderr, "smoke-api:", err)
		os.Exit(1)
	}
	fmt.Println("smoke-api: passed")
}

func run(c client, email string) error {
	body := fmt.Sprintf(`{"email":%q,"displayName":"Smoke"}`, email)

	var created user
	if err := c.expect(http.MethodPost, "/users", body, http.StatusCreated, &created); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if created.ID == "" || created.Email != email || !strings.HasSuffix(created.CreatedAt, "Z") {
		return fmt.Errorf("create returned %+v", created)
	}

	var fetched user
	if err := c.expect(http.MethodGet, "/users/"+created.ID, "", http.StatusOK, &fetched); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	if fetched != created {
		return fmt.Errorf("fetched %+v, want %+v", fetched, created)
	}

	var listed struct {
		Users []user `json:"users"`
	}
	if err := c.expect(http.MethodGet, "/users?limit=1", "", http.StatusOK, &listed); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	if len(listed.Users) != 1 || listed.Users[0] != created {
		return fmt.Errorf("listed %+v, want %+v", listed.Users, created)
	}

	failures := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
		fields []string
	}{
		{"duplicate email", http.MethodPost, "/users", body, http.StatusConflict, "USER_ALREADY_EXISTS", nil},
		{"body too large", http.MethodPost, "/users", `{"email":"` + strings.Repeat("a", 1<<20) + `"}`, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", nil},
		{"bad uuid", http.MethodGet, "/users/not-a-uuid", "", http.StatusBadRequest, "INVALID_REQUEST", []string{"id"}},
		{"malformed json", http.MethodPost, "/users", `{`, http.StatusBadRequest, "INVALID_REQUEST", nil},
		{"invalid fields", http.MethodPost, "/users", `{"email":"bad","displayName":" "}`, http.StatusBadRequest, "INVALID_REQUEST", []string{"email", "displayName"}},
		{"bad limit", http.MethodGet, "/users?limit=0", "", http.StatusBadRequest, "INVALID_REQUEST", []string{"limit"}},
		{"not found", http.MethodGet, "/users/00000000-0000-7000-8000-000000000000", "", http.StatusNotFound, "USER_NOT_FOUND", nil},
	}
	for _, failure := range failures {
		var got errorBody
		if err := c.expect(failure.method, failure.path, failure.body, failure.status, &got); err != nil {
			return fmt.Errorf("%s: %w", failure.name, err)
		}
		if got.Error.Code != failure.code || !slices.Equal(got.Error.Fields, failure.fields) {
			return fmt.Errorf("%s: got %+v", failure.name, got.Error)
		}
	}

	return nil
}

func (c client) expect(method, path, body string, status int, out any) error {
	req, err := http.NewRequestWithContext(context.Background(), method, c.baseURL+path, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != status {
		return fmt.Errorf("status %d, want %d: %s", resp.StatusCode, status, raw)
	}
	if !json.Valid(raw) {
		return errors.New("response is not JSON: " + string(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
