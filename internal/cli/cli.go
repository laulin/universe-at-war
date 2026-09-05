// Package cli implements the local operational command surface.
package cli

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	appadmin "universeatwar/internal/app/administration"
	appauth "universeatwar/internal/app/authentication"
	appbootstrap "universeatwar/internal/app/bootstrap"
	appeconomy "universeatwar/internal/app/economy"
	appserverstate "universeatwar/internal/app/serverstate"
	appsetup "universeatwar/internal/app/setup"
	appsimulation "universeatwar/internal/app/simulation"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/building"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

const defaultDatabasePath = "universe-at-war.db"

// Runner executes commands without terminating the process, which keeps the
// command surface testable.
type Runner struct {
	Stdout             io.Writer
	Stderr             io.Writer
	Version            string
	DefaultDatabase    string
	DefaultListen      string
	Random             io.Reader
	PasswordParameters auth.Parameters
	ServeHTTP          func(context.Context, *http.Server) error
}

// Run executes one command and returns a process exit code.
func (r Runner) Run(ctx context.Context, arguments []string) int {
	if len(arguments) == 0 {
		r.usage()
		return 2
	}

	switch arguments[0] {
	case "version":
		fmt.Fprintf(r.Stdout, "Universe At War %s\n", r.Version)
		return 0
	case "migrate":
		return r.runMigrate(ctx, arguments[1:])
	case "doctor":
		return r.runDoctor(ctx, arguments[1:])
	case "serve":
		return r.runServe(ctx, arguments[1:])
	case "admin":
		return r.runAdmin(ctx, arguments[1:])
	case "help", "-h", "--help":
		r.usage()
		return 0
	default:
		fmt.Fprintf(r.Stderr, "unknown command %q\n", arguments[0])
		r.usage()
		return 2
	}
}

func (r Runner) runMigrate(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", r.databaseDefault(), "path to the SQLite database")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "migrate: unexpected positional arguments")
		return 2
	}

	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("migrate", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return r.commandError("migrate", err)
	}
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return r.commandError("migrate", err)
	}
	fmt.Fprintf(r.Stdout, "migrations: ok (schema version %d)\n", version)
	return 0
}

func (r Runner) runDoctor(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", r.databaseDefault(), "path to the SQLite database")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "doctor: unexpected positional arguments")
		return 2
	}

	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("doctor", err)
	}
	defer database.Close()
	version, err := database.SchemaVersion(ctx)
	if err != nil {
		return r.commandError("doctor", errors.New("database is not migrated"))
	}
	if err := database.CheckIntegrity(ctx); err != nil {
		return r.commandError("doctor", err)
	}
	fmt.Fprintf(r.Stdout, "schema version: %d\nintegrity: ok\n", version)
	return 0
}

func (r Runner) runServe(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", r.databaseDefault(), "path to the SQLite database")
	listenAddress := flags.String("listen", r.listenDefault(), "HTTP listen address")
	secureCookie := flags.Bool("secure-cookie", false, "require HTTPS for browser cookies")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "serve: unexpected positional arguments")
		return 2
	}
	if _, _, err := net.SplitHostPort(*listenAddress); err != nil {
		return r.commandError("serve", fmt.Errorf("invalid listen address: %w", err))
	}
	if !isLoopbackAddress(*listenAddress) && !*secureCookie {
		fmt.Fprintln(r.Stderr, "WARNING: serving HTTP on a non-loopback address without secure cookies or TLS")
	}

	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("serve", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return r.commandError("serve", err)
	}

	random := r.randomSource()
	parameters := r.passwordParameters()
	passwords := auth.NewPasswordHasher(parameters, random)
	clock := appclock.System{}
	bootstrap := appbootstrap.Service{
		Clock:      clock,
		Passwords:  passwords,
		Secrets:    auth.NewSecretGenerator(random, 32),
		Repository: storagesqlite.NewBootstrapRepository(database.Write()),
		Version:    r.Version,
	}
	bootstrapResult, err := bootstrap.Initialize(ctx)
	if err != nil {
		return r.commandError("serve", err)
	}
	if bootstrapResult.Created {
		fmt.Fprintf(r.Stdout, "Bootstrap administrator created.\nUsername: %s\nBootstrap password: %s\nThis password will not be shown again.\n", bootstrapResult.Username, bootstrapResult.Password)
	}

	authentication := appauth.Service{
		Clock:       clock,
		Passwords:   passwords,
		Tokens:      auth.NewSecretGenerator(random, 32),
		Repository:  storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()),
		SessionLife: 12 * time.Hour,
	}
	states := appserverstate.Service{
		Repository: storagesqlite.NewServerStateRepository(database.Read(), database.Write()),
	}
	setup := appsetup.Service{
		Clock:      clock,
		Repository: storagesqlite.NewSetupRepository(database.Write()),
	}
	economy := appeconomy.Service{
		Clock:      clock,
		Repository: storagesqlite.NewEconomyRepository(database.Write()),
		Catalogue:  building.DefaultCatalogue(),
	}
	worker := appsimulation.NewWorker(clock, &economy)
	economy.Wake = worker.Wake
	workerContext, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(workerContext) }()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: authentication,
		ServerState:    states,
		CSRFSecrets:    auth.NewSecretGenerator(random, 32),
		Setup:          setup,
		Economy:        economy,
		SecureCookies:  *secureCookie,
	})
	if err != nil {
		return r.commandError("serve", err)
	}
	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	serve := r.ServeHTTP
	if serve == nil {
		serve = serveUntilCancelled
	}
	fmt.Fprintf(r.Stdout, "Listening on http://%s\n", *listenAddress)
	if err := serve(ctx, server); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return r.commandError("serve", err)
	}
	stopWorker()
	if err := <-workerErrors; err != nil {
		return r.commandError("simulation", err)
	}
	return 0
}

func (r Runner) runAdmin(ctx context.Context, arguments []string) int {
	if len(arguments) == 0 || arguments[0] != "reset-password" {
		fmt.Fprintln(r.Stderr, "Usage: universe-at-war admin reset-password [options]")
		return 2
	}
	flags := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", r.databaseDefault(), "path to the SQLite database")
	username := flags.String("username", "admin", "administrator username")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "admin reset-password: unexpected positional arguments")
		return 2
	}
	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("admin reset-password", err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		return r.commandError("admin reset-password", err)
	}
	random := r.randomSource()
	service := appadmin.PasswordResetService{
		Clock:      appclock.System{},
		Passwords:  auth.NewPasswordHasher(r.passwordParameters(), random),
		Secrets:    auth.NewSecretGenerator(random, 32),
		Repository: storagesqlite.NewAdministrationRepository(database.Write()),
	}
	result, err := service.Reset(ctx, *username)
	if err != nil {
		return r.commandError("admin reset-password", err)
	}
	fmt.Fprintf(r.Stdout, "Administrator password reset.\nUsername: %s\nNew password: %s\nThis password will not be shown again.\n", result.Username, result.Password)
	return 0
}

func (r Runner) commandError(command string, err error) int {
	fmt.Fprintf(r.Stderr, "%s: %v\n", command, err)
	return 1
}

func (r Runner) usage() {
	fprintln(r.Stderr, `Usage: universe-at-war <command> [options]

Commands:
  serve    start the local Universe At War server
  migrate  apply embedded SQLite migrations
  doctor   verify database schema and integrity
  admin reset-password
           replace an administrator password from the local machine
  version  print application version`)
}

func (r Runner) databaseDefault() string {
	if strings.TrimSpace(r.DefaultDatabase) != "" {
		return r.DefaultDatabase
	}
	return defaultDatabasePath
}

func (r Runner) listenDefault() string {
	if strings.TrimSpace(r.DefaultListen) != "" {
		return r.DefaultListen
	}
	return "127.0.0.1:8080"
}

func (r Runner) randomSource() io.Reader {
	if r.Random != nil {
		return r.Random
	}
	return cryptorand.Reader
}

func (r Runner) passwordParameters() auth.Parameters {
	if r.PasswordParameters != (auth.Parameters{}) {
		return r.PasswordParameters
	}
	return auth.DefaultParameters()
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func serveUntilCancelled(ctx context.Context, server *http.Server) error {
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownContext)
		case <-stopped:
		}
	}()
	err := server.ListenAndServe()
	close(stopped)
	return err
}

func fprintln(writer io.Writer, value string) {
	_, _ = fmt.Fprintln(writer, value)
}
