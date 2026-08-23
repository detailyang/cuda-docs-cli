package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/detailyang/cuda-docs-cli/internal/nvidia"
	"github.com/detailyang/cuda-docs-cli/internal/oauth"
)

type DocsClient interface {
	ListTools(context.Context) ([]nvidia.Tool, error)
	CallTool(context.Context, string, map[string]any) (nvidia.ToolResult, error)
}

type AuthManager interface {
	Login(context.Context, oauth.LoginOptions) error
	Clear() error
}

type App struct {
	In            io.Reader
	Out           io.Writer
	Err           io.Writer
	Version       string
	NewDocsClient func(context.Context) (DocsClient, error)
	Auth          AuthManager
}

func NewApp(in io.Reader, out, errorOutput io.Writer, version string) (App, error) {
	authManager, err := oauth.NewManager(out)
	if err != nil {
		return App{}, err
	}
	return App{
		In:      in,
		Out:     out,
		Err:     errorOutput,
		Version: version,
		Auth:    authManager,
		NewDocsClient: func(context.Context) (DocsClient, error) {
			return nvidia.NewClient(authManager.Resource, authManager, &http.Client{Timeout: 60 * time.Second}), nil
		},
	}, nil
}

func (app App) Run(ctx context.Context, arguments []string) int {
	if app.Out == nil {
		app.Out = io.Discard
	}
	if app.Err == nil {
		app.Err = io.Discard
	}
	if len(arguments) == 0 {
		app.printUsage()
		return 2
	}
	var err error
	switch arguments[0] {
	case "help", "-h", "--help":
		app.printUsage()
		return 0
	case "version", "--version":
		fmt.Fprintln(app.Out, app.Version)
		return 0
	case "login":
		err = app.login(ctx, arguments[1:])
	case "logout":
		if app.Auth == nil {
			err = errors.New("authentication is unavailable")
		} else if err = app.Auth.Clear(); err == nil {
			fmt.Fprintln(app.Out, "Logged out.")
		}
	case "tools":
		err = app.tools(ctx, arguments[1:])
	case "search":
		err = app.search(ctx, arguments[1:])
	case "call":
		err = app.call(ctx, arguments[1:])
	default:
		err = fmt.Errorf("unknown command %q", arguments[0])
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(app.Err, "error: %v\n", err)
		return 1
	}
	return 0
}

func (app App) login(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(flagOutput(arguments, app.Out, app.Err))
	port := flags.Int("port", 8765, "local OAuth callback port")
	noBrowser := flags.Bool("no-browser", false, "print the login URL without opening a browser")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: cuda-docs login [--port 8765] [--no-browser]")
	}
	if app.Auth == nil {
		return errors.New("authentication is unavailable")
	}
	if err := app.Auth.Login(ctx, oauth.LoginOptions{Port: *port, OpenBrowser: !*noBrowser, Timeout: 5 * time.Minute}); err != nil {
		return err
	}
	fmt.Fprintln(app.Out, "Logged in.")
	return nil
}

func (app App) tools(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("tools", flag.ContinueOnError)
	flags.SetOutput(flagOutput(arguments, app.Out, app.Err))
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: cuda-docs tools [--json]")
	}
	client, err := app.client(ctx)
	if err != nil {
		return err
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(app.Out, tools)
	}
	for _, tool := range tools {
		fmt.Fprintf(app.Out, "%s\t%s\n", tool.Name, tool.Description)
	}
	return nil
}

func (app App) search(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(flagOutput(arguments, app.Out, app.Err))
	asJSON := flags.Bool("json", false, "print raw JSON")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(flags.Args(), " "))
	if query == "" {
		return errors.New("usage: cuda-docs search [--json] <query>")
	}
	client, err := app.client(ctx)
	if err != nil {
		return err
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	name, toolArguments, err := nvidia.ChooseSearchInvocation(tools, query)
	if err != nil {
		return err
	}
	result, err := client.CallTool(ctx, name, toolArguments)
	if err != nil {
		return err
	}
	return app.renderToolResult(result, *asJSON)
}

func (app App) call(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("call", flag.ContinueOnError)
	flags.SetOutput(flagOutput(arguments, app.Out, app.Err))
	asJSON := flags.Bool("json", false, "print raw JSON")
	encodedArguments := flags.String("args", "{}", "tool arguments as a JSON object")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: cuda-docs call [--args JSON] [--json] <tool-name>")
	}
	var toolArguments map[string]any
	if err := json.Unmarshal([]byte(*encodedArguments), &toolArguments); err != nil {
		return fmt.Errorf("parse --args: %w", err)
	}
	if toolArguments == nil {
		return errors.New("--args must be a JSON object")
	}
	client, err := app.client(ctx)
	if err != nil {
		return err
	}
	result, err := client.CallTool(ctx, flags.Arg(0), toolArguments)
	if err != nil {
		return err
	}
	return app.renderToolResult(result, *asJSON)
}

func (app App) client(ctx context.Context) (DocsClient, error) {
	if app.NewDocsClient == nil {
		return nil, errors.New("documentation client is unavailable")
	}
	return app.NewDocsClient(ctx)
}

func (app App) renderToolResult(result nvidia.ToolResult, asJSON bool) error {
	var texts []string
	for _, content := range result.Content {
		if content.Type == "text" && content.Text != "" {
			texts = append(texts, content.Text)
		}
	}
	if result.IsError {
		if len(texts) > 0 {
			return errors.New(strings.Join(texts, "\n"))
		}
		return errors.New("remote documentation query failed")
	}
	if asJSON {
		return writeJSON(app.Out, result)
	}
	if len(texts) > 0 {
		fmt.Fprintln(app.Out, strings.Join(texts, "\n"))
		return nil
	}
	return writeJSON(app.Out, result)
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func flagOutput(arguments []string, helpOutput, errorOutput io.Writer) io.Writer {
	for _, argument := range arguments {
		if argument == "-h" || argument == "--help" {
			return helpOutput
		}
	}
	return errorOutput
}

func (app App) printUsage() {
	fmt.Fprintln(app.Out, `cuda-docs queries NVIDIA CUDA documentation from a normal CLI.

Usage:
  cuda-docs login [--port 8765] [--no-browser]
  cuda-docs logout
  cuda-docs search [--json] <query>
  cuda-docs tools [--json]
  cuda-docs call [--args JSON] [--json] <tool-name>
  cuda-docs version`)
}
