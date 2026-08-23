package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultBaseURL  = "https://api.copilot.nsight.ngc.nvidia.com"
	DefaultResource = DefaultBaseURL + "/mcp/cuda-docs"
)

type AuthorizationRequest struct {
	Endpoint      string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	State         string
	Resource      string
}

func AuthorizationURL(request AuthorizationRequest) (string, error) {
	endpoint, err := url.Parse(request.Endpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", request.ClientID)
	query.Set("redirect_uri", request.RedirectURI)
	query.Set("code_challenge", request.CodeChallenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", request.State)
	query.Set("resource", request.Resource)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	RedirectURI  string `json:"redirect_uri"`
	Resource     string `json:"resource"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
}

type Manager struct {
	BaseURL    string
	Resource   string
	ConfigPath string
	HTTPClient *http.Client
	Out        io.Writer
	OpenURL    func(string) error
	Now        func() time.Time
}

type LoginOptions struct {
	Port        int
	OpenBrowser bool
	Timeout     time.Duration
}

func DefaultConfigPath() (string, error) {
	if override := os.Getenv("CUDA_DOCS_CONFIG_DIR"); override != "" {
		return filepath.Join(override, "credentials.json"), nil
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(directory, "cuda-docs-cli", "credentials.json"), nil
}

func NewManager(out io.Writer) (*Manager, error) {
	configPath, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	resource := os.Getenv("CUDA_DOCS_ENDPOINT")
	if resource == "" {
		resource = DefaultResource
	}
	baseURL := strings.TrimSuffix(resource, "/mcp/cuda-docs")
	return &Manager{
		BaseURL:    baseURL,
		Resource:   resource,
		ConfigPath: configPath,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Out:        out,
		OpenURL:    openBrowser,
		Now:        time.Now,
	}, nil
}

func (manager *Manager) Login(ctx context.Context, options LoginOptions) error {
	if options.Port == 0 {
		options.Port = 8765
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", options.Port)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", options.Port))
	if err != nil {
		return fmt.Errorf("listen for OAuth callback: %w", err)
	}
	defer listener.Close()

	credentials, _ := manager.load()
	if credentials.ClientID == "" || credentials.RedirectURI != redirectURI {
		credentials, err = manager.register(ctx, redirectURI)
		if err != nil {
			return err
		}
		if err := manager.save(credentials); err != nil {
			return err
		}
	}

	verifier, err := randomString(48)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	state, err := randomString(24)
	if err != nil {
		return err
	}
	authorizationURL, err := AuthorizationURL(AuthorizationRequest{
		Endpoint:      manager.BaseURL + "/authorize",
		ClientID:      credentials.ClientID,
		RedirectURI:   redirectURI,
		CodeChallenge: challenge,
		State:         state,
		Resource:      manager.Resource,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(manager.Out, "Open this URL to sign in:\n%s\n", authorizationURL)
	if options.OpenBrowser {
		if err := manager.OpenURL(authorizationURL); err != nil {
			fmt.Fprintf(manager.Out, "Could not open a browser automatically: %v\n", err)
		}
	}

	codeChannel := make(chan string, 1)
	errorChannel := make(chan error, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
	server.Handler = callbackHandler(state, codeChannel, errorChannel)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errorChannel <- serveErr
		}
	}()
	defer server.Shutdown(context.Background())

	waitContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	var code string
	select {
	case code = <-codeChannel:
	case callbackErr := <-errorChannel:
		return fmt.Errorf("OAuth callback: %w", callbackErr)
	case <-waitContext.Done():
		return fmt.Errorf("OAuth login timed out: %w", waitContext.Err())
	}

	token, err := manager.exchange(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {credentials.ClientID},
		"client_secret": {credentials.ClientSecret},
		"resource":      {manager.Resource},
	})
	if err != nil {
		return err
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.RefreshToken
	credentials.TokenType = token.TokenType
	credentials.ExpiresAt = manager.now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
	return manager.save(credentials)
}

func callbackHandler(state string, codes chan<- string, failures chan<- error) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != state {
			http.Error(writer, "OAuth state mismatch", http.StatusBadRequest)
			failures <- errors.New("state mismatch")
			return
		}
		if remoteError := query.Get("error"); remoteError != "" {
			http.Error(writer, remoteError, http.StatusBadRequest)
			failures <- errors.New(remoteError)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(writer, "missing authorization code", http.StatusBadRequest)
			failures <- errors.New("missing authorization code")
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(writer, "Login complete. You can close this tab and return to the terminal.")
		codes <- code
	})
}

func (manager *Manager) Token(ctx context.Context) (string, error) {
	credentials, err := manager.load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("not logged in; run: cuda-docs login")
		}
		return "", err
	}
	if credentials.AccessToken == "" {
		return "", errors.New("not logged in; run: cuda-docs login")
	}
	if credentials.ExpiresAt > manager.now().Add(30*time.Second).Unix() {
		return credentials.AccessToken, nil
	}
	if credentials.RefreshToken == "" {
		return "", errors.New("login expired; run: cuda-docs login")
	}
	token, err := manager.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credentials.RefreshToken},
		"client_id":     {credentials.ClientID},
		"client_secret": {credentials.ClientSecret},
		"resource":      {credentials.Resource},
	})
	if err != nil {
		return "", fmt.Errorf("refresh login: %w", err)
	}
	credentials.AccessToken = token.AccessToken
	if token.RefreshToken != "" {
		credentials.RefreshToken = token.RefreshToken
	}
	credentials.TokenType = token.TokenType
	credentials.ExpiresAt = manager.now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix()
	if err := manager.save(credentials); err != nil {
		return "", err
	}
	return credentials.AccessToken, nil
}

func (manager *Manager) Clear() error {
	err := os.Remove(manager.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (manager *Manager) register(ctx context.Context, redirectURI string) (Credentials, error) {
	payload := map[string]any{
		"client_name":                "NVIDIA CUDA Docs CLI",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "client_secret_post",
	}
	var response struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := manager.postJSON(ctx, manager.BaseURL+"/register", payload, &response); err != nil {
		return Credentials{}, fmt.Errorf("register OAuth client: %w", err)
	}
	if response.ClientID == "" {
		return Credentials{}, errors.New("register OAuth client: response omitted client_id")
	}
	return Credentials{
		ClientID:     response.ClientID,
		ClientSecret: response.ClientSecret,
		RedirectURI:  redirectURI,
		Resource:     manager.Resource,
	}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (manager *Manager) exchange(ctx context.Context, values url.Values) (tokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, manager.BaseURL+"/token", strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := manager.client().Do(request)
	if err != nil {
		return tokenResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return tokenResponse{}, responseError(response)
	}
	var token tokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return tokenResponse{}, fmt.Errorf("decode token: %w", err)
	}
	if token.AccessToken == "" {
		return tokenResponse{}, errors.New("token response omitted access_token")
	}
	if token.ExpiresIn == 0 {
		token.ExpiresIn = 3600
	}
	return token, nil
}

func (manager *Manager) postJSON(ctx context.Context, endpoint string, payload any, destination any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := manager.client().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return json.NewDecoder(response.Body).Decode(destination)
}

func responseError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
}

func (manager *Manager) load() (Credentials, error) {
	file, err := os.Open(manager.ConfigPath)
	if err != nil {
		return Credentials{}, err
	}
	defer file.Close()
	var credentials Credentials
	if err := json.NewDecoder(file).Decode(&credentials); err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	return credentials, nil
}

func (manager *Manager) save(credentials Credentials) error {
	directory := filepath.Dir(manager.ConfigPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*")
	if err != nil {
		return fmt.Errorf("create credentials file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(credentials); err != nil {
		temporary.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, manager.ConfigPath); err != nil {
		return fmt.Errorf("replace credentials: %w", err)
	}
	return nil
}

func (manager *Manager) client() *http.Client {
	if manager.HTTPClient != nil {
		return manager.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (manager *Manager) now() time.Time {
	if manager.Now != nil {
		return manager.Now()
	}
	return time.Now()
}

func randomString(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secure random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
