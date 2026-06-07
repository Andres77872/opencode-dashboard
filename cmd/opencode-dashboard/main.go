// Package main is the entry point for opencode-dashboard CLI.
// The dashboard provides visibility into OpenCode AI assistant usage patterns
// via both a terminal UI (TUI) and web interface from a single binary.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/claudecode"
	"opencode-dashboard/internal/source/codex"
	opencodesource "opencode-dashboard/internal/source/opencode"
	"opencode-dashboard/internal/stats"
	"opencode-dashboard/internal/store"
	"opencode-dashboard/internal/tui"
	"opencode-dashboard/internal/uninstall"
	"opencode-dashboard/internal/version"
	"opencode-dashboard/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "web":
		return cmdWeb(args[1:])
	case "tui":
		return cmdTUI(args[1:])
	case "version":
		return cmdVersion(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", args[0])
		printUsage()
		return fmt.Errorf("unknown command")
	}
}

func printUsage() {
	fmt.Println(`opencode-dashboard - Analytics dashboard for OpenCode

Usage:
  opencode-dashboard <command> [flags]

Commands:
  web        Run the local web dashboard and API server
  tui        Run the local terminal dashboard
  version    Print version and build metadata
  uninstall  Remove dashboard-owned local files

Global help:
  opencode-dashboard help
  opencode-dashboard <command> --help

Examples:
  opencode-dashboard web
  opencode-dashboard web --port 9090 --channel latest
  opencode-dashboard web --db ~/.local/share/opencode/opencode-beta.db --no-open
  opencode-dashboard web --source opencode
  opencode-dashboard tui --channel stable
  opencode-dashboard version
  opencode-dashboard uninstall --dry-run

Web flags:
  --port <n>     Bind localhost port (default: 7450)
  --db <path>    Use an explicit OpenCode SQLite database path
  --channel <c>  Resolve a channel-specific OpenCode DB (stable/latest/beta/custom)
  --source <id>  Initial data source (opencode, claude_code, or codex; default: opencode)
  --claude-home <dir>  Claude Code config directory for future claude_code registration
  --codex-home <dir>   Codex config directory for codex registration
  --no-open      Do not launch the browser automatically

TUI flags:
  --db <path>    Use an explicit OpenCode SQLite database path
  --channel <c>  Resolve a channel-specific OpenCode DB

Uninstall flags:
  --dry-run      Show the removal plan only
  --force        Skip the confirmation prompt`)
}

func cmdWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	port := fs.Int("port", web.DefaultPort, "localhost port to bind")
	dbPath := fs.String("db", "", "explicit OpenCode SQLite database path")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	channel := fs.String("channel", "", "channel-specific OpenCode database to use")
	sourceFlag := fs.String("source", string(source.SourceOpenCode), "initial data source: opencode, claude_code, or codex")
	claudeHome := fs.String("claude-home", "", "explicit Claude Code config directory")
	codexHome := fs.String("codex-home", "", "explicit Codex config directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: opencode-dashboard web [--port <n>] [--db <path>] [--channel <name>] [--source <id>] [--claude-home <dir>] [--codex-home <dir>] [--no-open]\n\n")
		fmt.Fprintf(fs.Output(), "Starts the local web dashboard and serves the API on http://%s:<port>.\n", web.DefaultHost)
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("web does not accept positional arguments")
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("--port must be between 1 and 65535")
	}
	selectedSource, err := parseSourceSelection(*sourceFlag)
	if err != nil {
		return err
	}
	claudeSelection := config.ResolveClaudeHome(*claudeHome)
	codexSelection := config.ResolveCodexHome(*codexHome)

	selection, err := resolveDBSelection(*dbPath, *channel)
	if err != nil {
		return err
	}

	ctx := context.Background()
	st, openErr := openValidatedStore(ctx, selection.Path)
	if openErr != nil && selectedSource == source.SourceOpenCode {
		return openErr
	}
	if openErr != nil {
		st = nil
	}
	registry, err := buildWebRegistry(st, selection, selectedSource, claudeSelection, *claudeHome, codexSelection, *codexHome)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		return err
	}
	defer registry.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	addr := web.DefaultHost + ":" + strconv.Itoa(*port)
	server := web.NewServer(addr, registry, logger)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	serverErr := make(chan error, 1)

	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	serverURL := (&url.URL{Scheme: "http", Host: addr}).String()
	fmt.Printf("opencode-dashboard %s\n", version.BuildInfo())
	fmt.Printf("web server: %s\n", serverURL)
	fmt.Printf("api base:   %s/api/v1\n", serverURL)
	fmt.Printf("database:   %s\n", selection.Path)
	fmt.Printf("db source:  %s\n", selection.Source)
	fmt.Printf("source:     %s\n", selectedSource)
	if selectedSource == source.SourceClaudeCode || *claudeHome != "" || os.Getenv(config.EnvClaudeConfigDir) != "" {
		fmt.Printf("claude:    %s (%s)\n", claudeSelection.Path, claudeSelection.Source)
	}
	if selectedSource == source.SourceCodex || *codexHome != "" || os.Getenv(config.EnvCodexHome) != "" {
		fmt.Printf("codex:     %s (%s)\n", codexSelection.Path, codexSelection.Source)
	}
	if web.HasAssets() {
		fmt.Println("frontend:   embedded assets")
	} else {
		fmt.Println("frontend:   placeholder page only (web assets not built yet)")
	}

	if !*noOpen {
		if err := web.OpenBrowser(serverURL); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to open browser: %v\n", err)
		} else {
			fmt.Printf("browser:    opened %s\n", serverURL)
		}
	}
	fmt.Println("ready:      press Ctrl+C to stop")

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		return fmt.Errorf("web server failed: %w", err)
	case <-signalCtx.Done():
		fmt.Fprintln(os.Stderr, "shutting down web server...")
	}

	if err := web.GracefulShutdown(context.Background(), server); err != nil {
		return err
	}

	return nil
}

func cmdTUI(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dbPath := fs.String("db", "", "explicit OpenCode SQLite database path")
	channel := fs.String("channel", "", "channel-specific OpenCode database to use")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: opencode-dashboard tui [--db <path>] [--channel <name>]\n\n")
		fmt.Fprintln(fs.Output(), "Starts the local terminal dashboard against a validated OpenCode database.")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("tui does not accept positional arguments")
	}

	selection, err := resolveDBSelection(*dbPath, *channel)
	if err != nil {
		return err
	}

	st, err := openValidatedStore(context.Background(), selection.Path)
	if err != nil {
		return err
	}
	defer st.Close()

	return tui.Run(st, tui.Options{
		DBPath:   selection.Path,
		DBSource: selection.Source,
		Version:  version.BuildInfo(),
	})
}

func cmdVersion(args []string) error {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: opencode-dashboard version")
		return nil
	}
	if len(args) != 0 {
		return fmt.Errorf("version does not accept arguments")
	}

	fmt.Printf("opencode-dashboard %s\n", version.BuildInfo())
	return nil
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dryRun := fs.Bool("dry-run", false, "show the removal plan only")
	force := fs.Bool("force", false, "skip confirmation")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: opencode-dashboard uninstall [--dry-run] [--force]\n\n")
		fmt.Fprintln(fs.Output(), "Removes dashboard-owned local files only. OpenCode databases and config are never touched.")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("uninstall does not accept positional arguments")
	}

	plan, err := uninstall.Plan()
	if err != nil {
		return err
	}

	printUninstallPlan(plan)
	if *dryRun {
		fmt.Println("dry-run: no files were removed")
		return nil
	}
	if !plan.HasRemovals() {
		fmt.Println("nothing to remove")
		return nil
	}

	if !*force {
		confirmed, err := confirmRemoval(os.Stdin)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("uninstall cancelled")
			return nil
		}
	}

	result, err := uninstall.Execute(plan)
	for _, target := range result.Removed {
		fmt.Printf("removed: %s (%s)\n", target.Path, target.Kind)
	}
	for _, target := range result.Skipped {
		if target.Reason == "" {
			continue
		}
		fmt.Printf("kept:    %s (%s: %s)\n", target.Path, target.Kind, target.Reason)
	}
	if err != nil {
		return err
	}

	if len(result.Removed) == 0 {
		fmt.Println("nothing was removed")
		return nil
	}

	fmt.Println("uninstall complete")
	return nil
}

type dbSelection struct {
	Path   string
	Source string
}

func resolveDBSelection(flagDB string, channel string) (dbSelection, error) {
	selection, err := config.ResolveOpenCodeDB(flagDB, channel)
	if err != nil {
		return dbSelection{}, err
	}
	return dbSelection{Path: selection.Path, Source: selection.Source}, nil
}

func parseSourceSelection(value string) (source.SourceID, error) {
	selected := strings.TrimSpace(value)
	if selected == "" {
		return source.SourceOpenCode, nil
	}
	switch source.SourceID(selected) {
	case source.SourceOpenCode, source.SourceClaudeCode, source.SourceCodex:
		return source.SourceID(selected), nil
	case source.SourceID("both"):
		return "", fmt.Errorf("--source=both is unsupported in v1; select one source at a time")
	default:
		return "", fmt.Errorf("invalid --source %q (supported: opencode, claude_code, codex)", selected)
	}
}

func buildWebRegistry(st *store.Store, selection dbSelection, startup source.SourceID, claudeSelection config.PathSelection, explicitClaudeHome string, codexSelection config.PathSelection, explicitCodexHome string) (*source.Registry, error) {
	registry := source.NewRegistry(source.SourceOpenCode)
	registry.SetStartupID(startup)
	if st != nil {
		if err := registry.Register(opencodesource.New(st, opencodesource.WithPath(selection.Path, selection.Source))); err != nil {
			return nil, err
		}
	} else if err := registry.RegisterUnavailable(source.SourceInfo{
		ID:         source.SourceOpenCode,
		Label:      "OpenCode",
		Kind:       "sqlite",
		Path:       selection.Path,
		PathSource: selection.Source,
		ReadOnly:   true,
		LocalOnly:  true,
		Capabilities: []string{
			"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config",
		},
		Diagnostics: source.SourceDiagnostics{
			Status: "unavailable",
			Reason: "OpenCode database is not available or schema is invalid",
		},
		CostPolicy: source.CostPolicy{Status: string(stats.CostMissing), Currency: "USD", Note: "OpenCode database is unavailable"},
		Privacy:    source.PrivacyInfo{ReadOnly: true, LocalOnly: true, Redaction: true},
	}); err != nil {
		return nil, err
	}

	claude := claudecode.New(claudecode.Options{ClaudeHome: claudeSelection.Path, PathSource: claudeSelection.Source})
	claudeInfo := claude.Info(context.Background())
	claudeConfigured := startup == source.SourceClaudeCode || explicitClaudeHome != "" || os.Getenv(config.EnvClaudeConfigDir) != ""
	if claudeInfo.Available {
		if err := registry.Register(claude); err != nil {
			return nil, err
		}
	} else if claudeConfigured {
		if err := registry.RegisterUnavailable(claudeInfo); err != nil {
			return nil, err
		}
	}

	codexSrc := codex.New(codex.Options{CodexHome: codexSelection.Path, PathSource: codexSelection.Source})
	codexInfo := codexSrc.Info(context.Background())
	codexConfigured := startup == source.SourceCodex || explicitCodexHome != "" || os.Getenv(config.EnvCodexHome) != ""
	if codexInfo.Available {
		if err := registry.Register(codexSrc); err != nil {
			return nil, err
		}
	} else if codexConfigured {
		if err := registry.RegisterUnavailable(codexInfo); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func openValidatedStore(ctx context.Context, dbPath string) (*store.Store, error) {
	if err := config.ValidateDBPath(dbPath); err != nil {
		return nil, err
	}

	st, err := store.Connect(ctx, dbPath)
	if err != nil {
		return nil, err
	}

	if st.IsValidSchema() {
		return st, nil
	}

	schema := st.Schema()
	_ = st.Close()
	return nil, fmt.Errorf("%w: missing required tables: %s", store.ErrInvalidSchema, missingTables(schema))
}

func missingTables(schema store.SchemaInfo) string {
	missing := make([]string, 0, 5)
	if !schema.HasSession {
		missing = append(missing, "session")
	}
	if !schema.HasMessage {
		missing = append(missing, "message")
	}
	if !schema.HasProject {
		missing = append(missing, "project")
	}
	if !schema.HasWorkspace {
		missing = append(missing, "workspace")
	}
	if !schema.HasPart {
		missing = append(missing, "part")
	}
	if len(missing) == 0 {
		return "unknown"
	}
	return strings.Join(missing, ", ")
}

func printUninstallPlan(plan *uninstall.RemovalPlan) {
	if plan == nil {
		return
	}

	fmt.Println("Removal plan:")
	for _, target := range plan.Targets {
		status := "keep"
		if target.Exists && target.Remove {
			status = "remove"
		} else if !target.Exists {
			status = "missing"
		}

		fmt.Printf("- [%s] %s: %s\n", status, target.Kind, target.Path)
		if target.Reason != "" {
			fmt.Printf("    reason: %s\n", target.Reason)
		}
	}
	for _, note := range plan.Notes {
		fmt.Printf("- note: %s\n", note)
	}
}

func confirmRemoval(input *os.File) (bool, error) {
	fmt.Print("Proceed with removal? [y/N]: ")

	reader := bufio.NewReader(input)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, syscall.EINTR) {
		if !errors.Is(err, io.EOF) {
			return false, err
		}
	}

	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}
