package integration

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"task_tracker/internal/identity"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/email"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/service"
	"testing"
	"time"

	transporthttp "task_tracker/internal/transport/http"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	baseURL   string
	authSvc   *service.Auth
	emailMock *emailServiceMock
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx := context.Background()

	mysqlC, err := tcmysql.Run(ctx, "mysql:8.4",
		tcmysql.WithDatabase("tasks"),
		tcmysql.WithUsername("tasks"),
		tcmysql.WithPassword("tasks"),
	)
	if err != nil {
		return 0, fmt.Errorf("mysql container: %w", err)
	}
	defer func() { _ = mysqlC.Terminate(ctx) }()

	redisC, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		return 0, fmt.Errorf("redis container: %w", err)
	}
	defer func() { _ = redisC.Terminate(ctx) }()

	dsn, err := mysqlC.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		return 0, fmt.Errorf("mysql dsn: %w", err)
	}
	redisAddr, err := redisC.Endpoint(ctx, "")
	if err != nil {
		return 0, fmt.Errorf("redis endpoint: %w", err)
	}

	db, err := persistence.NewMySQL(persistence.Config{
		DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 5, ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		return 0, fmt.Errorf("mysql pool: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("mysql"); err != nil {
		return 0, err
	}
	if err := goose.Up(db, "../../migrations"); err != nil {
		return 0, fmt.Errorf("migrations: %w", err)
	}

	rdb := cache.NewRedis(cache.Config{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()

	emailMock = newEmailServiceMock()
	defer emailMock.srv.Close()

	log := slog.New(slog.DiscardHandler)
	idp := identity.NewProvider(identity.Config{Secret: strings.Repeat("s", 32), TTL: time.Hour})
	userRepo := persistence.NewUserRepo(db)
	authSvc = service.NewAuth(userRepo, idp)
	emailClient := email.NewClient(email.Config{
		BaseURL: emailMock.srv.URL, Timeout: time.Second, MaxFailures: 3, OpenFor: time.Minute,
	})
	teamsSvc := service.NewTeams(persistence.NewTeamRepo(db), userRepo, emailClient, log)

	h := health.New(health.Config{CheckTimeout: time.Second})
	h.SetReady()

	srv := httptest.NewServer(transporthttp.NewRouter(log, h, authSvc, teamsSvc, idp))
	defer srv.Close()
	baseURL = srv.URL

	return m.Run(), nil
}

func doJSON(t *testing.T, method, path, bearer, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

func decodeJSON(t *testing.T, body string, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(body), v))
}

func register(t *testing.T, email, name, password string) {
	t.Helper()
	resp := doJSON(t, http.MethodPost, "/api/v1/register", "",
		fmt.Sprintf(`{"email":%q,"name":%q,"password":%q}`, email, name, password))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
}
