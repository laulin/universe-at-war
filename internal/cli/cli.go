// Package cli implements the local operational command surface.
package cli

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"universeatwar/internal/ai"
	appacs "universeatwar/internal/app/acs"
	appactivity "universeatwar/internal/app/activity"
	appadmin "universeatwar/internal/app/administration"
	appai "universeatwar/internal/app/ai"
	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appbattlesimulation "universeatwar/internal/app/battlesimulation"
	appbootstrap "universeatwar/internal/app/bootstrap"
	appchat "universeatwar/internal/app/chat"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appgalaxy "universeatwar/internal/app/galaxy"
	appjumpgate "universeatwar/internal/app/jumpgate"
	appmoderation "universeatwar/internal/app/moderation"
	appphalanx "universeatwar/internal/app/phalanx"
	appranking "universeatwar/internal/app/ranking"
	appregistration "universeatwar/internal/app/registration"
	appreports "universeatwar/internal/app/reports"
	appresearch "universeatwar/internal/app/research"
	appserverstate "universeatwar/internal/app/serverstate"
	appsetup "universeatwar/internal/app/setup"
	appshipyard "universeatwar/internal/app/shipyard"
	appsimulation "universeatwar/internal/app/simulation"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/catalogue"
	seeds "universeatwar/internal/domain/random"
	"universeatwar/internal/observability"
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
	LogLevel           string
	ServeHTTP          func(context.Context, *http.Server) error
}

// logger builds the structured logger of the serve command. Logs go to the
// error stream so the bootstrap password stays alone on the standard output.
func (r Runner) logger() (*slog.Logger, error) {
	level := r.LogLevel
	if strings.TrimSpace(level) == "" {
		level = "info"
	}
	return observability.NewLogger(r.Stderr, level)
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
	case "backup":
		return r.runBackup(ctx, arguments[1:])
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

// runBackup writes a verified snapshot of the universe next to the database.
func (r Runner) runBackup(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(r.Stderr)
	databasePath := flags.String("database", r.databaseDefault(), "path to the SQLite database")
	directory := flags.String("directory", "", "directory to write the backup into (default: next to the database)")
	keep := flags.Int("keep", 0, "how many snapshots to keep, oldest removed first (0 keeps all)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(r.Stderr, "backup: unexpected positional arguments")
		return 2
	}
	target := *directory
	if strings.TrimSpace(target) == "" {
		target = filepath.Join(filepath.Dir(*databasePath), "backups")
	}
	database, err := storagesqlite.Open(ctx, *databasePath)
	if err != nil {
		return r.commandError("backup", err)
	}
	defer database.Close()
	if _, err := database.SchemaVersion(ctx); err != nil {
		return r.commandError("backup", errors.New("database is not migrated"))
	}
	snapshot, err := database.Backup(ctx, target, time.Now().UTC())
	if err != nil {
		return r.commandError("backup", err)
	}
	fmt.Fprintf(r.Stdout, "backup: %s\nbytes: %d\nschema version: %d\nintegrity: ok\n",
		snapshot.Path, snapshot.Bytes, snapshot.SchemaVersion)
	removed, err := storagesqlite.PruneBackups(target, *keep)
	if err != nil {
		return r.commandError("backup", err)
	}
	for _, name := range removed {
		fmt.Fprintf(r.Stdout, "removed: %s\n", name)
	}
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
	if version > storagesqlite.LatestSchemaVersion() {
		return r.commandError("doctor", fmt.Errorf(
			"database carries schema %d, newer than the %d this build knows", version, storagesqlite.LatestSchemaVersion()))
	}
	report, err := database.Diagnose(ctx)
	if err != nil {
		return r.commandError("doctor", err)
	}
	fmt.Fprintf(r.Stdout, "schema version: %d\nintegrity: ok\njournal mode: %s\nwritable: %t\nforeign keys: ok\nruleset: %s\n",
		version, report.JournalMode, report.Writable, report.Ruleset)
	if !report.Writable {
		return r.commandError("doctor", errors.New("the database is not writable"))
	}
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

	logger, err := r.logger()
	if err != nil {
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
	invitations := appadmin.InvitationService{
		Clock:      clock,
		Secrets:    auth.NewSecretGenerator(random, 24),
		Repository: storagesqlite.NewInvitationRepository(database.Write()),
	}
	moderation := appmoderation.Service{
		Clock:      clock,
		Repository: storagesqlite.NewModerationRepository(database.Write()),
	}
	backups := appadmin.BackupService{
		Clock:      clock,
		Repository: storagesqlite.NewBackupRepository(database, filepath.Join(filepath.Dir(*databasePath), "backups")),
		Keep:       14,
	}
	dashboard := appadmin.DashboardService{
		Clock:      clock,
		Repository: storagesqlite.NewDashboardRepository(database.Write(), *databasePath, clock.Now().UTC()),
	}
	registration := appregistration.Service{
		Clock:      clock,
		Passwords:  passwords,
		Repository: storagesqlite.NewRegistrationRepository(database.Read(), database.Write()),
	}
	setup := appsetup.Service{
		Clock:      clock,
		Repository: storagesqlite.NewSetupRepository(database.Write()),
	}
	catalogues := catalogue.Default()
	economyRepository := storagesqlite.NewEconomyRepository(database.Write(), catalogues.Buildings)
	researchRepository := storagesqlite.NewResearchRepository(database.Write(), catalogues)
	shipyardRepository := storagesqlite.NewShipyardRepository(database.Write(), catalogues)
	fleetRepository := storagesqlite.NewFleetRepository(database.Write(), catalogues)
	acsRepository := storagesqlite.NewACSRepository(database.Write(), catalogues, fleetRepository)
	aiRepository := storagesqlite.NewAIRepository(database.Write())
	metrics := observability.NewMetrics()
	events := storagesqlite.NewEventProcessor(database.Write(), clock)
	events.Logger = logger
	events.Metrics = metrics
	economyRepository.RegisterHandlers(events)
	researchRepository.RegisterHandlers(events)
	shipyardRepository.RegisterHandlers(events)
	fleetRepository.RegisterHandlers(events)
	acsRepository.RegisterHandlers(events)
	aiRepository.RegisterHandlers(events)
	worker := appsimulation.NewWorker(clock, events)
	worker.Logger = logger
	worker.Metrics = metrics
	economy := appeconomy.Service{
		Clock:      clock,
		Repository: economyRepository,
		Catalogue:  catalogues.Buildings,
		Completer:  events,
		Wake:       worker.Wake,
	}
	research := appresearch.Service{
		Clock:      clock,
		Repository: researchRepository,
		Catalogues: catalogues,
		Completer:  events,
		Wake:       worker.Wake,
	}
	fleet := appfleet.Service{
		Clock:      clock,
		Repository: fleetRepository,
		Catalogues: catalogues,
		Seeds:      seeds.NewSeedGenerator(cryptorand.Reader),
		Completer:  events,
		Wake:       worker.Wake,
	}
	alliance := appalliance.Service{
		Clock:      clock,
		Repository: storagesqlite.NewAllianceRepository(database.Write()),
	}
	chat := appchat.Service{
		Clock:      clock,
		Repository: storagesqlite.NewChatRepository(database.Read(), database.Write()),
		Typing:     &appchat.TypingTracker{},
	}
	operations := appacs.Service{
		Clock:      clock,
		Repository: acsRepository,
		Seeds:      seeds.NewSeedGenerator(cryptorand.Reader),
		Completer:  events,
		Wake:       worker.Wake,
	}
	artificials := appai.Service{
		Clock:      clock,
		Repository: aiRepository,
		Census:     aiRepository,
		Empires:    economy,
		Alliances:  alliance,
		Seeds:      seeds.NewSeedGenerator(cryptorand.Reader),
		Completer:  events,
	}
	galaxy := appgalaxy.Service{Repository: storagesqlite.NewGalaxyRepository(database.Read())}
	ranking := appranking.Service{
		Repository: storagesqlite.NewRankingRepository(database.Read()),
		Catalogues: catalogues,
	}
	sensors := appphalanx.Service{
		Clock:      clock,
		Repository: storagesqlite.NewPhalanxRepository(database.Write(), catalogues),
		Completer:  events,
	}
	gates := appjumpgate.Service{
		Clock:      clock,
		Repository: storagesqlite.NewJumpGateRepository(database.Write(), catalogues),
		Completer:  events,
	}
	reports := appreports.Service{
		Clock:      clock,
		Repository: storagesqlite.NewReportsRepository(database.Read(), database.Write()),
		Completer:  events,
	}
	battleSimulation := appbattlesimulation.Service{Reports: reports, Economy: economy, Catalogues: catalogues}
	shipyard := appshipyard.Service{
		Clock:      clock,
		Repository: shipyardRepository,
		Catalogues: catalogues,
		Completer:  events,
		Wake:       worker.Wake,
	}
	activity := appactivity.Service{
		Repository: storagesqlite.NewActivityRepository(database.Read()),
		Completer:  events,
	}
	worker.Thinker = &ai.Brain{
		Clock:      clock,
		Thinking:   appai.Thinking{Clock: clock, Thought: aiRepository},
		Economy:    economy,
		Research:   research,
		Shipyard:   shipyard,
		Fleet:      fleet,
		Reports:    reports,
		Galaxy:     galaxy,
		Teamwork:   appai.Teamwork{Shared: aiRepository},
		Diplomacy:  alliance,
		Operations: operations,
		Catalogues: catalogues,
		Logger:     logger,
	}
	worker.Populator = appai.Populating{
		Clock:   clock,
		Service: artificials,
		Census:  aiRepository,
	}
	workerContext, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	workerErrors := make(chan error, 1)
	go func() { workerErrors <- worker.Run(workerContext) }()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication:   authentication,
		ServerState:      states,
		Timezone:         states,
		CSRFSecrets:      auth.NewSecretGenerator(random, 32),
		Nonces:           auth.NewSecretGenerator(random, 16),
		Setup:            setup,
		Economy:          economy,
		Research:         research,
		Shipyard:         shipyard,
		Fleet:            fleet,
		Activity:         activity,
		Galaxy:           galaxy,
		Ranking:          ranking,
		Reports:          reports,
		BattleSimulation: battleSimulation,
		Phalanx:          sensors,
		JumpGate:         gates,
		Alliance:         alliance,
		Chat:             chat,
		ACS:              operations,
		Artificials:      artificials,
		Dashboard:        dashboard,
		Invitations:      invitations,
		Moderation:       moderation,
		Backups:          backups,
		Registration:     registration,
		Logger:           logger,
		Metrics:          metrics,
		WakeSimulation:   worker.Wake,
		SecureCookies:    *secureCookie,
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
	logger.Info("server starting", "version", r.Version, "listen", *listenAddress, "database", *databasePath)
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
  backup   write a verified snapshot of the universe
  doctor   verify database schema, integrity, writability and ruleset
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
