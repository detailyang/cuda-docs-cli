package cli_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/detailyang/cuda-docs-cli/internal/cli"
	"github.com/detailyang/cuda-docs-cli/internal/nvidia"
)

type fakeClient struct {
	result nvidia.ToolResult
}

func (f fakeClient) ListTools(context.Context) ([]nvidia.Tool, error) {
	return []nvidia.Tool{{
		Name:        "search_docs",
		Description: "Search CUDA docs",
		InputSchema: nvidia.InputSchema{
			Properties: map[string]nvidia.Property{"query": {Type: "string"}},
			Required:   []string{"query"},
		},
	}}, nil
}

func (f fakeClient) CallTool(context.Context, string, map[string]any) (nvidia.ToolResult, error) {
	return f.result, nil
}

func TestSearchPrintsTextResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.App{
		Out: &stdout,
		Err: &stderr,
		NewDocsClient: func(context.Context) (cli.DocsClient, error) {
			return fakeClient{result: nvidia.ToolResult{Content: []nvidia.Content{{Type: "text", Text: "Use cudaEventElapsedTime."}}}}, nil
		},
	}

	code := app.Run(context.Background(), []string{"search", "measure kernel time"})
	if code != 0 {
		t.Fatalf("exit code = %d; stderr = %s", code, stderr.String())
	}
	if got := stdout.String(); got != "Use cudaEventElapsedTime.\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestSearchReturnsFailureForRemoteToolError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.App{
		Out: &stdout,
		Err: &stderr,
		NewDocsClient: func(context.Context) (cli.DocsClient, error) {
			return fakeClient{result: nvidia.ToolResult{IsError: true, Content: []nvidia.Content{{Type: "text", Text: "query rejected"}}}}, nil
		},
	}

	code := app.Run(context.Background(), []string{"search", "bad query"})
	if code != 1 {
		t.Fatalf("exit code = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); got != "error: query rejected\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestToolsCanPrintAdvertisedSchemaAsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.App{
		Out: &stdout,
		Err: &stderr,
		NewDocsClient: func(context.Context) (cli.DocsClient, error) {
			return fakeClient{}, nil
		},
	}

	code := app.Run(context.Background(), []string{"tools", "--json"})
	if code != 0 {
		t.Fatalf("exit code = %d; stderr = %s", code, stderr.String())
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte(`"name": "search_docs"`)) {
		t.Fatalf("stdout = %q", got)
	}
}

func TestVersionDoesNotConstructANetworkClient(t *testing.T) {
	var stdout bytes.Buffer
	app := cli.App{Out: &stdout, Err: &bytes.Buffer{}, Version: "v1.2.3"}

	code := app.Run(context.Background(), []string{"version"})
	if code != 0 || stdout.String() != "v1.2.3\n" {
		t.Fatalf("code = %d, stdout = %q", code, stdout.String())
	}
}

func TestSubcommandHelpIsSuccessful(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := cli.App{Out: &stdout, Err: &stderr}

	code := app.Run(context.Background(), []string{"search", "--help"})
	if code != 0 {
		t.Fatalf("exit code = %d; stderr = %q", code, stderr.String())
	}
}
