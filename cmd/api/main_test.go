package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStartupCreatesSchemaAndServesHealth(t *testing.T) {
	raw := os.Getenv("DOCTORCHART_STARTUP_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("DOCTORCHART_STARTUP_TEST_DATABASE_URL absent; disposable PostgreSQL required")
	}
	address, err := url.Parse(raw)
	if err != nil || (address.Scheme != "postgres" && address.Scheme != "postgresql") ||
		address.Path != "/doctorchart_startup_test" || address.User == nil ||
		(address.Hostname() != "localhost" && (net.ParseIP(address.Hostname()) == nil || !net.ParseIP(address.Hostname()).IsLoopback())) {
		t.Fatal("startup test requires a loopback database named doctorchart_startup_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("invalid fixture connection")
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	commandDirectory := filepath.Join(root, "cmd", "api")
	if err := os.MkdirAll(commandDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	password, _ := address.User.Password()
	username := address.User.Username()
	address.User = nil
	address.Scheme = "postgresql"
	dotenv := fmt.Sprintf("SPRING_DATASOURCE_URL=jdbc:%s\nDB_USERNAME='%s'\nDB_PASSWORD='%s'\nGATEWAY_SHARED_SECRET=%s\nSERVICES_BILLING_SHARED_SECRET=%s\nSERVICES_BILLING_URL=http://127.0.0.1:1\nPORT=%d\nSTORAGE_TYPE=local\nSTUDIES_STORAGE_DIR='%s'\n", address.String(), username, password, strings.Repeat("g", 64), strings.Repeat("b", 64), port, filepath.Join(root, "studies"))
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(dotenv), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "doctorchart")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture binary: %v\n%s", err, output)
	}
	logPath := filepath.Join(root, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	processContext, stop := context.WithCancel(ctx)
	command := exec.CommandContext(processContext, binary)
	command.Dir = commandDirectory
	// Exclude developer settings: only the synthetic .env and OS runtime paths.
	command.Env = []string{}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TMP", "TEMP", "HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		stop()
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop()
		// Cancellation intentionally stops this test-owned child.
		command.Wait()
		logFile.Close()
		if t.Failed() {
			if output, err := os.ReadFile(logPath); err == nil {
				t.Log(string(output))
			}
		}
	})
	client := &http.Client{Timeout: time.Second}
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/api/doctorchart/health", port)
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(healthURL)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("startup timed out")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("empty database startup did not serve HTTP health")
	}
	var tables, specialties int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM specialties").Scan(&specialties); err != nil {
		t.Fatal(err)
	}
	if tables != 20 || specialties == 0 {
		t.Fatalf("health served with %d tables and %d specialties", tables, specialties)
	}
}
