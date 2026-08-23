package oauth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/cuda-docs-cli/internal/oauth"
)

func TestAuthorizationURLUsesPKCEAndResourceIndicator(t *testing.T) {
	got, err := oauth.AuthorizationURL(oauth.AuthorizationRequest{
		Endpoint:      "https://api.example.test/authorize",
		ClientID:      "client-1",
		RedirectURI:   "http://127.0.0.1:8765/callback",
		CodeChallenge: "challenge",
		State:         "state-1",
		Resource:      "https://api.example.test/mcp/cuda-docs",
	})
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" {
		t.Fatalf("response_type = %q", query.Get("response_type"))
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q", query.Get("code_challenge_method"))
	}
	if query.Get("resource") != "https://api.example.test/mcp/cuda-docs" {
		t.Fatalf("resource = %q", query.Get("resource"))
	}
}

func TestTokenRefreshesExpiredCredentialsAndKeepsFilePrivate(t *testing.T) {
	fixedTime := time.Unix(1_800_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := request.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q", got)
		}
		if got := request.Form.Get("refresh_token"); got != "refresh-1" {
			t.Errorf("refresh_token = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmtJSON := `{"access_token":"access-2","refresh_token":"refresh-2","token_type":"Bearer","expires_in":3600}`
		_, _ = writer.Write([]byte(fmtJSON))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config", "credentials.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	credentials := oauth.Credentials{
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		Resource:     server.URL + "/mcp/cuda-docs",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    fixedTime.Add(-time.Minute).Unix(),
	}
	encoded, _ := json.Marshal(credentials)
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &oauth.Manager{
		BaseURL:    server.URL,
		Resource:   server.URL + "/mcp/cuda-docs",
		ConfigPath: configPath,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return fixedTime },
	}

	token, err := manager.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token != "access-2" {
		t.Fatalf("token = %q", token)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"refresh_token": "refresh-2"`) {
		t.Fatalf("saved credentials = %s", saved)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("credentials permissions = %o", got)
		}
	}
}

func TestTokenExplainsHowToLoginWhenCredentialsAreMissing(t *testing.T) {
	manager := &oauth.Manager{ConfigPath: filepath.Join(t.TempDir(), "missing.json")}
	_, err := manager.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cuda-docs login") {
		t.Fatalf("Token() error = %v", err)
	}
}

func TestLoginRegistersUsesLoopbackCallbackAndStoresToken(t *testing.T) {
	var sawRegistration, sawExchange bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/register":
			sawRegistration = true
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(writer, `{"client_id":"client-1","client_secret":"secret-1"}`)
		case "/token":
			sawExchange = true
			if err := request.ParseForm(); err != nil {
				t.Error(err)
			}
			if request.Form.Get("code") != "code-1" || request.Form.Get("code_verifier") == "" {
				t.Errorf("token form = %v", request.Form)
			}
			writer.Header().Set("Content-Type", "application/json")
			fmt.Fprint(writer, `{"access_token":"access-1","refresh_token":"refresh-1","token_type":"Bearer","expires_in":3600}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	var output bytes.Buffer
	fixedTime := time.Unix(1_800_000_000, 0)
	manager := &oauth.Manager{
		BaseURL:    server.URL,
		Resource:   server.URL + "/mcp/cuda-docs",
		ConfigPath: filepath.Join(t.TempDir(), "credentials.json"),
		HTTPClient: server.Client(),
		Out:        &output,
		Now:        func() time.Time { return fixedTime },
	}
	manager.OpenURL = func(target string) error {
		parsed, err := url.Parse(target)
		if err != nil {
			return err
		}
		callback := parsed.Query().Get("redirect_uri") + "?code=code-1&state=" + url.QueryEscape(parsed.Query().Get("state"))
		go func() {
			response, getErr := http.Get(callback)
			if getErr == nil {
				response.Body.Close()
			}
		}()
		return nil
	}

	err = manager.Login(context.Background(), oauth.LoginOptions{Port: port, OpenBrowser: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !sawRegistration || !sawExchange {
		t.Fatalf("registration = %v, exchange = %v", sawRegistration, sawExchange)
	}
	if !strings.Contains(output.String(), server.URL+"/authorize") {
		t.Fatalf("login output = %q", output.String())
	}
	token, err := manager.Token(context.Background())
	if err != nil || token != "access-1" {
		t.Fatalf("Token() = %q, %v", token, err)
	}
}
