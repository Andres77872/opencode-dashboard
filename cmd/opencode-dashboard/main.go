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
	"sync"
	"syscall"
	"time"

	usagecache "opencode-dashboard/internal/cache"
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
  --cache-db <path>  Use an explicit dashboard cache SQLite database path
  --rebuild-cache    Remove the dashboard usage cache before starting
  --no-cache     Run without the dashboard cache
  --channel <c>  Resolve a channel-specific OpenCode DB (stable/latest/beta/custom)
  --source <id>  Initial data source (opencode, claude_code, or codex; default: opencode)
  --claude-home <dir>  Claude Code config directory for future claude_code registration
  --codex-home <dir>   Codex config directory for codex registration
  --no-open      Do not launch the browser automatically

TUI flags:
  --db <path>    Use an explicit OpenCode SQLite database path
  --cache-db <path>  Use an explicit dashboard cache SQLite database path
  --rebuild-cache    Remove the dashboard usage cache before starting
  --no-cache     Run without the dashboard cache
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
	cacheDBPath := fs.String("cache-db", "", "explicit dashboard cache SQLite database path")
	rebuildCache := fs.Bool("rebuild-cache", false, "remove the dashboard usage cache before serving")
	noCache := fs.Bool("no-cache", false, "run without the dashboard usage cache")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	channel := fs.String("channel", "", "channel-specific OpenCode database to use")
	sourceFlag := fs.String("source", string(source.SourceOpenCode), "initial data source: opencode, claude_code, or codex")
	claudeHome := fs.String("claude-home", "", "explicit Claude Code config directory")
	codexHome := fs.String("codex-home", "", "explicit Codex config directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: opencode-dashboard web [--port <n>] [--db <path>] [--cache-db <path>] [--rebuild-cache] [--no-cache] [--channel <name>] [--source <id>] [--claude-home <dir>] [--codex-home <dir>] [--no-open]\n\n")
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
	if err := validateCacheFlags(*noCache, *rebuildCache); err != nil {
		return err
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, openErr := openValidatedStore(ctx, selection.Path)
	if openErr != nil && selectedSource == source.SourceOpenCode {
		return openErr
	}
	if openErr != nil {
		st = nil
	}
	cacheSelection := config.ResolveCacheDB(*cacheDBPath)
	cacheRuntime, err := openCacheRuntime(ctx, cacheSelection, *rebuildCache, *noCache)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		return err
	}
	cacheRuntime.SetLogger(logger)
	registry, err := buildWebRegistry(cacheRuntime, st, selection, selectedSource, claudeSelection, *claudeHome, codexSelection, *codexHome)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		return err
	}
	defer cacheRuntime.Close()
	defer registry.Close()

	addr := web.DefaultHost + ":" + strconv.Itoa(*port)
	server := web.NewServerWithCache(addr, registry, logger, cacheRuntime)
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
	printCacheStartup(cacheRuntime, cacheSelection)
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
	cacheDBPath := fs.String("cache-db", "", "explicit dashboard cache SQLite database path")
	rebuildCache := fs.Bool("rebuild-cache", false, "remove the dashboard usage cache before running")
	noCache := fs.Bool("no-cache", false, "run without the dashboard usage cache")
	channel := fs.String("channel", "", "channel-specific OpenCode database to use")
	sourceFlag := fs.String("source", string(source.SourceOpenCode), "initial data source: opencode, claude_code, or codex")
	claudeHome := fs.String("claude-home", "", "explicit Claude Code config directory")
	codexHome := fs.String("codex-home", "", "explicit Codex config directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: opencode-dashboard tui [--db <path>] [--cache-db <path>] [--rebuild-cache] [--no-cache] [--channel <name>] [--source <id>] [--claude-home <dir>] [--codex-home <dir>]\n\n")
		fmt.Fprintln(fs.Output(), "Starts the local terminal dashboard. Switch source (S) and time range (T) live.")
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
	if err := validateCacheFlags(*noCache, *rebuildCache); err != nil {
		return err
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

	cacheSelection := config.ResolveCacheDB(*cacheDBPath)
	cacheRuntime, err := openCacheRuntime(ctx, cacheSelection, *rebuildCache, *noCache)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		return err
	}
	registry, err := buildWebRegistry(cacheRuntime, st, selection, selectedSource, claudeSelection, *claudeHome, codexSelection, *codexHome)
	if err != nil {
		if st != nil {
			_ = st.Close()
		}
		return err
	}
	defer cacheRuntime.Close()
	defer registry.Close()

	return tui.Run(registry, tui.Options{Version: version.BuildInfo()})
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

type cacheRuntime struct {
	mu             sync.Mutex
	store          *usagecache.Store
	path           string
	source         string
	registry       *source.Registry
	order          []source.SourceID
	live           map[source.SourceID]source.Source
	cached         map[source.SourceID]bool
	pendingInitial []source.SourceID
	job            cacheJobState
	disabled       bool
	logger         *slog.Logger
}

// SetLogger directs cache runtime and store activity logs to l.
func (c *cacheRuntime) SetLogger(l *slog.Logger) {
	if c == nil || l == nil {
		return
	}
	c.logger = l
	if c.store != nil {
		c.store.SetLogger(l)
	}
}

func (c *cacheRuntime) log() *slog.Logger {
	if c == nil || c.logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.logger
}

type cacheJobState struct {
	Running         bool
	Status          string
	Mode            usagecache.SyncMode
	Target          string
	CurrentSourceID source.SourceID
	Total           int
	Completed       int
	Phase           string
	ItemsDone       int64
	ItemsTotal      int64
	SafeCutoffMS    int64
	StartedAtMS     int64
	UpdatedAtMS     int64
	FinishedAtMS    int64
	Error           string
	Logs            []web.CacheLogEntry
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

func validateCacheFlags(noCache, rebuildCache bool) error {
	if noCache && rebuildCache {
		return fmt.Errorf("--no-cache cannot be combined with --rebuild-cache")
	}
	return nil
}

func openCacheRuntime(ctx context.Context, selection config.PathSelection, rebuild bool, disabled bool) (*cacheRuntime, error) {
	runtime := &cacheRuntime{
		path:     selection.Path,
		source:   selection.Source,
		live:     make(map[source.SourceID]source.Source),
		cached:   make(map[source.SourceID]bool),
		disabled: disabled,
	}
	if disabled {
		return runtime, nil
	}
	if rebuild {
		if err := removeCacheDB(selection.Path); err != nil {
			return nil, err
		}
	}
	cacheStore, err := usagecache.Open(ctx, selection.Path)
	if err != nil {
		return nil, err
	}
	runtime.store = cacheStore
	return runtime, nil
}

func printCacheStartup(cache *cacheRuntime, selection config.PathSelection) {
	switch {
	case cache == nil || cache.disabled:
		fmt.Println("cache:      disabled")
	case cache.hasCachedSources():
		fmt.Printf("cache:      %s (%s, active for ready sources)\n", selection.Path, selection.Source)
	default:
		fmt.Printf("cache:      %s (%s, sync available in web UI)\n", selection.Path, selection.Source)
	}
}

func (c *cacheRuntime) hasCachedSources() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cached := range c.cached {
		if cached {
			return true
		}
	}
	return false
}

func (c *cacheRuntime) Close() error {
	if c == nil || c.store == nil {
		return nil
	}
	return c.store.Close()
}

func buildWebRegistry(cache *cacheRuntime, st *store.Store, selection dbSelection, startup source.SourceID, claudeSelection config.PathSelection, explicitClaudeHome string, codexSelection config.PathSelection, explicitCodexHome string) (*source.Registry, error) {
	registry := source.NewRegistry(source.SourceOpenCode)
	registry.SetStartupID(startup)
	if cache != nil {
		cache.registry = registry
	}
	if st != nil {
		openSrc := opencodesource.New(st, opencodesource.WithPath(selection.Path, selection.Source))
		if err := registerCachedSource(context.Background(), registry, cache, openSrc); err != nil {
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
		if err := registerCachedSource(context.Background(), registry, cache, claude); err != nil {
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
		if err := registerCachedSource(context.Background(), registry, cache, codexSrc); err != nil {
			return nil, err
		}
	} else if codexConfigured {
		if err := registry.RegisterUnavailable(codexInfo); err != nil {
			return nil, err
		}
	}

	cache.startPendingInitialSync()
	return registry, nil
}

func registerCachedSource(ctx context.Context, registry *source.Registry, cache *cacheRuntime, src source.Source) error {
	if cache != nil {
		cache.rememberSource(src)
	}
	if cache == nil || cache.store == nil || cache.disabled {
		return registry.Register(src)
	}
	need, err := cache.store.NeedsSync(ctx, src)
	if err != nil {
		return err
	}
	if need.Needed && need.Status.Status != "ready" {
		// Cache is empty or unhealthy: serve live raw reads for now and let the
		// startup background sync swap in the cached wrapper when it finishes.
		cache.queueInitialSync(sourceIDOf(src))
		return registry.Register(src)
	}
	// A ready cache is served even when the source fingerprint changed: reads
	// gap-fill the window since the finality cutoff from raw data on demand.
	cache.markCached(sourceIDOf(src))
	return registry.Register(usagecache.WrapSource(cache.store, src))
}

func (c *cacheRuntime) queueInitialSync(id source.SourceID) {
	if c == nil || id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingInitial = append(c.pendingInitial, id)
}

// startPendingInitialSync launches a background sync for sources that had no
// usable cache at startup, so the dashboard becomes cache-backed without a
// manual resync.
func (c *cacheRuntime) startPendingInitialSync() {
	if c == nil || c.disabled || c.store == nil {
		return
	}
	cutoff := usagecache.DefaultSafeCutoff(time.Now().UTC())
	c.mu.Lock()
	if c.job.Running || len(c.pendingInitial) == 0 {
		c.mu.Unlock()
		return
	}
	targets := make([]source.Source, 0, len(c.pendingInitial))
	for _, id := range c.pendingInitial {
		if src := c.live[id]; src != nil {
			targets = append(targets, src)
		}
	}
	c.pendingInitial = nil
	if len(targets) == 0 {
		c.mu.Unlock()
		return
	}
	c.startJobLocked("startup", targets, usagecache.SyncModeIncremental, cutoff)
	c.mu.Unlock()
	ids := make([]string, 0, len(targets))
	for _, src := range targets {
		ids = append(ids, string(sourceIDOf(src)))
	}
	c.log().Info("cache: starting initial background consolidation; views serve live data until it finishes",
		"sources", strings.Join(ids, ","))
	go c.runSyncJob(targets, usagecache.SyncModeIncremental, cutoff)
}

func (c *cacheRuntime) rememberSource(src source.Source) {
	id := sourceIDOf(src)
	if c == nil || id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live == nil {
		c.live = make(map[source.SourceID]source.Source)
	}
	if c.cached == nil {
		c.cached = make(map[source.SourceID]bool)
	}
	if _, ok := c.live[id]; !ok {
		c.order = append(c.order, id)
	}
	c.live[id] = src
}

func (c *cacheRuntime) markCached(id source.SourceID) {
	if c == nil || id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached == nil {
		c.cached = make(map[source.SourceID]bool)
	}
	c.cached[id] = true
}

func sourceIDOf(src source.Source) source.SourceID {
	if src == nil {
		return ""
	}
	return src.Info(context.Background()).ID
}

func (c *cacheRuntime) Status(ctx context.Context) (web.CacheStatusResponse, error) {
	if c == nil {
		return web.CacheStatusResponse{Enabled: false}, nil
	}
	resp := web.CacheStatusResponse{
		Enabled: !c.disabled && c.store != nil,
		Path:    c.path,
		Source:  c.source,
	}
	if !resp.Enabled {
		return resp, nil
	}
	// Snapshot the job before reading per-source flags: the job marks sources
	// cached before it finishes, so a "complete" snapshot can never be paired
	// with stale cached=false flags read afterwards.
	resp.Sync = c.jobSnapshot()
	for _, src := range c.sourceSnapshot() {
		info := src.Info(ctx)
		item := web.CacheSourceStatus{
			SourceID:  string(info.ID),
			Label:     info.Label,
			Available: info.Available,
			Cached:    c.isCached(info.ID),
		}
		if item.Cached {
			resp.Active = true
		}
		if info.Available {
			need, err := c.store.NeedsSync(ctx, src)
			if err != nil {
				item.NeedsSync = true
				item.Status = "error"
				item.Reason = err.Error()
			} else {
				// A ready cache self-heals on read (gap-fill), so a fingerprint
				// mismatch alone does not require a manual sync.
				item.NeedsSync = need.Needed && need.Status.Status != "ready"
				if item.NeedsSync {
					item.Reason = need.Reason
				}
				item.Status = need.Status.Status
				item.LastSyncedMS = need.Status.LastSynced
				item.SafeCutoffMS = need.Status.LastSafeCutoff
				item.FreshThroughMS = need.Status.FreshThrough
				if fill, ok := c.store.FillState(item.SourceID); ok {
					item.FillAttemptMS = fill.AttemptMS
					item.FillError = fill.ErrMsg
				}
				if item.LastSyncedMS > resp.LastUpdatedMS {
					resp.LastUpdatedMS = item.LastSyncedMS
				}
			}
		} else {
			item.Status = "unavailable"
			item.Reason = info.Diagnostics.Reason
		}
		resp.Sources = append(resp.Sources, item)
	}
	if resp.Sync != nil && resp.Sync.UpdatedAtMS > resp.LastUpdatedMS {
		resp.LastUpdatedMS = resp.Sync.UpdatedAtMS
	}
	return resp, nil
}

func (c *cacheRuntime) Sync(ctx context.Context, selected string, modeValue string) (web.CacheStatusResponse, error) {
	if c == nil || c.disabled || c.store == nil {
		return web.CacheStatusResponse{Enabled: false}, fmt.Errorf("dashboard cache is disabled")
	}
	mode, err := parseCacheSyncMode(modeValue)
	if err != nil {
		return web.CacheStatusResponse{}, err
	}
	cutoff := usagecache.DefaultSafeCutoff(time.Now().UTC())
	c.mu.Lock()
	if c.job.Running {
		c.mu.Unlock()
		return c.Status(ctx)
	}
	targets, err := c.syncTargetsLocked(selected)
	if err != nil {
		c.mu.Unlock()
		return web.CacheStatusResponse{}, err
	}
	c.startJobLocked(selected, targets, mode, cutoff)
	c.mu.Unlock()

	status, err := c.Status(ctx)
	if err != nil {
		return web.CacheStatusResponse{}, err
	}
	go c.runSyncJob(targets, mode, cutoff)
	return status, nil
}

func parseCacheSyncMode(value string) (usagecache.SyncMode, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch usagecache.SyncMode(value) {
	case "", usagecache.SyncModeIncremental:
		return usagecache.SyncModeIncremental, nil
	case usagecache.SyncModeRebuild:
		return usagecache.SyncModeRebuild, nil
	default:
		return "", fmt.Errorf("invalid cache sync mode %q", value)
	}
}

// runSyncJob consolidates each target in turn. One failing or unavailable
// source no longer aborts the rest: its error is logged and collected, the
// remaining sources still sync, and the job finishes with the combined error.
func (c *cacheRuntime) runSyncJob(targets []source.Source, mode usagecache.SyncMode, cutoff time.Time) {
	ctx := context.Background()
	start := time.Now()
	var errs []error
	for _, src := range targets {
		info := src.Info(ctx)
		if !info.Available {
			c.log().Warn("cache job: skipping unavailable source", "source", info.ID, "reason", info.Diagnostics.Reason)
			c.mu.Lock()
			c.job.Completed++
			c.job.UpdatedAtMS = nowMilli()
			c.appendLogLocked("warn", info.ID, fmt.Sprintf("Skipped %s: unavailable (%s)", info.Label, info.Diagnostics.Reason))
			c.mu.Unlock()
			errs = append(errs, fmt.Errorf("%s is unavailable: %s", info.Label, info.Diagnostics.Reason))
			continue
		}
		c.setCurrentSource(info.ID, syncStartMessage(info.Label, mode))
		c.log().Info("cache job: syncing source", "source", info.ID, "mode", mode)
		report, err := c.store.SyncSourceWithOptions(ctx, src, usagecache.SyncOptions{
			Mode:     mode,
			Cutoff:   cutoff,
			Progress: c.updateJobProgress,
		})
		if err != nil {
			c.log().Error("cache job: source sync failed", "source", info.ID, "error", err)
			c.mu.Lock()
			c.job.Completed++
			c.job.UpdatedAtMS = nowMilli()
			c.appendLogLocked("error", info.ID, fmt.Sprintf("Failed %s: %v", info.Label, err))
			c.mu.Unlock()
			errs = append(errs, fmt.Errorf("%s: %w", info.Label, err))
			continue
		}
		c.logSyncReport(info.ID, info.Label, report)
		if c.registry != nil {
			if err := c.registry.Register(usagecache.WrapSource(c.store, src)); err != nil {
				c.log().Error("cache job: registering cached source failed", "source", info.ID, "error", err)
				c.mu.Lock()
				c.job.Completed++
				c.job.UpdatedAtMS = nowMilli()
				c.appendLogLocked("error", info.ID, fmt.Sprintf("Failed to activate cache for %s: %v", info.Label, err))
				c.mu.Unlock()
				errs = append(errs, fmt.Errorf("%s: %w", info.Label, err))
				continue
			}
		}
		c.mu.Lock()
		c.cached[info.ID] = true
		c.job.Completed++
		c.job.UpdatedAtMS = nowMilli()
		c.appendLogLocked("info", info.ID, fmt.Sprintf("Finished %s", info.Label))
		c.mu.Unlock()
		c.log().Info("cache job: source done", "source", info.ID, "messages", report.Messages, "tools", report.Tools)
	}
	err := errors.Join(errs...)
	if err != nil {
		c.log().Error("cache job finished with errors", "targets", len(targets), "failed", len(errs), "duration", time.Since(start).Round(time.Millisecond), "error", err)
	} else {
		c.log().Info("cache job complete", "targets", len(targets), "duration", time.Since(start).Round(time.Millisecond))
	}
	c.finishJob(err)
}

// updateJobProgress records page-level progress for the source currently being
// consolidated. It only touches the numeric fields — progress never spams the
// job log list.
func (c *cacheRuntime) updateJobProgress(p usagecache.SyncProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.job.Phase = p.Phase
	c.job.ItemsDone = p.Done
	c.job.ItemsTotal = p.Total
	c.job.UpdatedAtMS = nowMilli()
}

func syncStartMessage(label string, mode usagecache.SyncMode) string {
	if mode == usagecache.SyncModeRebuild {
		return fmt.Sprintf("Clearing and rebuilding %s cache", label)
	}
	return fmt.Sprintf("Resyncing %s cache", label)
}

func (c *cacheRuntime) logSyncReport(sourceID source.SourceID, label string, report usagecache.SyncReport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	window := "from the beginning"
	if !report.Since.IsZero() {
		window = "after " + formatCacheLogTime(report.Since)
	}
	if !report.FreshThrough.IsZero() {
		window += " through " + formatCacheLogTime(report.FreshThrough)
	}
	if report.Mode == usagecache.SyncModeRebuild {
		c.appendLogLocked("info", sourceID, fmt.Sprintf("Rebuilt %s cache %s", label, window))
	} else {
		c.appendLogLocked("info", sourceID, fmt.Sprintf("Resynced %s cache %s", label, window))
	}
	if report.Messages == 0 {
		c.appendLogLocked("info", sourceID, "No eligible messages found in the sync window")
	} else {
		c.appendLogLocked("info", sourceID, fmt.Sprintf("Consolidated %d messages and %d tool events", report.Messages, report.Tools))
	}
	if !report.FreshThrough.IsZero() {
		c.appendLogLocked("info", sourceID, fmt.Sprintf("Consolidated through %s; newer activity loads live from the source", formatCacheLogTime(report.FreshThrough)))
	}
}

func formatCacheLogTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
}

func (c *cacheRuntime) sourceSnapshot() []source.Source {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]source.Source, 0, len(c.order))
	for _, id := range c.order {
		if src := c.live[id]; src != nil {
			out = append(out, src)
		}
	}
	return out
}

func (c *cacheRuntime) isCached(id source.SourceID) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cached[id]
}

func (c *cacheRuntime) syncTargetsLocked(selected string) ([]source.Source, error) {
	id := strings.TrimSpace(selected)
	if id == "" || id == "all" {
		targets := make([]source.Source, 0, len(c.order))
		for _, sourceID := range c.order {
			if src := c.live[sourceID]; src != nil {
				targets = append(targets, src)
			}
		}
		return targets, nil
	}
	sourceID := source.SourceID(id)
	src := c.live[sourceID]
	if src == nil {
		return nil, fmt.Errorf("invalid source %q", id)
	}
	return []source.Source{src}, nil
}

func (c *cacheRuntime) startJobLocked(selected string, targets []source.Source, mode usagecache.SyncMode, cutoff time.Time) {
	now := nowMilli()
	target := strings.TrimSpace(selected)
	if target == "" {
		target = "all"
	}
	message := fmt.Sprintf("Started incremental database resync for %s", target)
	if mode == usagecache.SyncModeRebuild {
		message = fmt.Sprintf("Started clear-and-rebuild database sync for %s", target)
	}
	c.job = cacheJobState{
		Running:      true,
		Status:       "running",
		Mode:         mode,
		Target:       target,
		Total:        len(targets),
		SafeCutoffMS: cutoff.UTC().UnixMilli(),
		StartedAtMS:  now,
		UpdatedAtMS:  now,
		Logs: []web.CacheLogEntry{{
			TimeMS:  now,
			Level:   "info",
			Message: message,
		}, {
			TimeMS:  now,
			Level:   "info",
			Message: fmt.Sprintf("Finalizing content through %s; newer activity stays mirrored from raw data", formatCacheLogTime(cutoff)),
		}},
	}
}

func (c *cacheRuntime) setCurrentSource(id source.SourceID, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.job.CurrentSourceID = id
	c.job.Phase = ""
	c.job.ItemsDone = 0
	c.job.ItemsTotal = 0
	c.job.UpdatedAtMS = nowMilli()
	c.appendLogLocked("info", id, message)
}

func (c *cacheRuntime) finishJob(syncErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := nowMilli()
	c.job.Running = false
	c.job.FinishedAtMS = now
	c.job.UpdatedAtMS = now
	c.job.CurrentSourceID = ""
	c.job.Phase = ""
	c.job.ItemsDone = 0
	c.job.ItemsTotal = 0
	if syncErr != nil {
		c.job.Status = "error"
		c.job.Error = syncErr.Error()
		c.appendLogLocked("error", "", syncErr.Error())
		return
	}
	c.job.Status = "complete"
	c.appendLogLocked("info", "", "Database sync complete")
}

func (c *cacheRuntime) appendLogLocked(level string, sourceID source.SourceID, message string) {
	entry := web.CacheLogEntry{
		TimeMS:   nowMilli(),
		Level:    level,
		SourceID: string(sourceID),
		Message:  message,
	}
	c.job.Logs = append(c.job.Logs, entry)
	const maxCacheLogs = 100
	if len(c.job.Logs) > maxCacheLogs {
		c.job.Logs = c.job.Logs[len(c.job.Logs)-maxCacheLogs:]
	}
}

func (c *cacheRuntime) jobSnapshot() *web.CacheSyncStatus {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobResponseLocked()
}

func (c *cacheRuntime) jobResponseLocked() *web.CacheSyncStatus {
	if c.job.Status == "" && len(c.job.Logs) == 0 {
		return nil
	}
	logs := append([]web.CacheLogEntry(nil), c.job.Logs...)
	return &web.CacheSyncStatus{
		Running:         c.job.Running,
		Status:          c.job.Status,
		Mode:            string(c.job.Mode),
		Target:          c.job.Target,
		CurrentSourceID: string(c.job.CurrentSourceID),
		Total:           c.job.Total,
		Completed:       c.job.Completed,
		CurrentPhase:    c.job.Phase,
		ItemsDone:       c.job.ItemsDone,
		ItemsTotal:      c.job.ItemsTotal,
		SafeCutoffMS:    c.job.SafeCutoffMS,
		StartedAtMS:     c.job.StartedAtMS,
		UpdatedAtMS:     c.job.UpdatedAtMS,
		FinishedAtMS:    c.job.FinishedAtMS,
		Error:           c.job.Error,
		Logs:            logs,
	}
}

func nowMilli() int64 {
	return time.Now().UTC().UnixMilli()
}

func removeCacheDB(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove cache database %q: %w", candidate, err)
		}
	}
	return nil
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
