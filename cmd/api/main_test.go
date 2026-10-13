package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The proxy only forwards to the already guarded disposable startup database.
// Disconnecting it simulates PostgreSQL loss for this child without stopping
// the shared test container or changing any database configuration or rows.
func startupDatabaseProxy(t *testing.T, target string) (string, func(bool)) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	blocked := false
	connections := map[net.Conn]bool{}
	setBlocked := func(value bool) {
		mu.Lock()
		defer mu.Unlock()
		blocked = value
		if value {
			for conn := range connections {
				conn.Close()
			}
		}
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			incoming, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if blocked {
				incoming.Close()
				mu.Unlock()
				continue
			}
			connections[incoming] = true
			mu.Unlock()
			workers.Go(func() {
				defer func() {
					incoming.Close()
					mu.Lock()
					delete(connections, incoming)
					mu.Unlock()
				}()
				outgoing, err := net.DialTimeout("tcp", target, time.Second)
				if err != nil {
					return
				}
				defer outgoing.Close()
				copied := make(chan struct{})
				go func() {
					defer close(copied)
					io.Copy(incoming, outgoing)
					incoming.Close()
					outgoing.Close()
				}()
				io.Copy(outgoing, incoming)
				incoming.Close()
				outgoing.Close()
				<-copied
			})
		}
	})
	t.Cleanup(func() { listener.Close(); setBlocked(true); workers.Wait() })
	return listener.Addr().String(), setBlocked
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal("invalid fixture connection")
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	proxy, disconnect := startupDatabaseProxy(t, address.Host)
	backendDatabase := *address
	backendDatabase.Host = proxy
	var billingRequests int
	var billingMu sync.Mutex
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		billingMu.Lock()
		billingRequests++
		billingMu.Unlock()
		if r.URL.Path != "/api/billing/limits/DOCTORCHART" {
			t.Error("unexpected synthetic Billing route")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"BASIC":{"max_patients":50,"max_records_per_patient":100},"PRO":{"max_patients":500,"max_records_per_patient":1000},"UNLIMITED":{"max_patients":-1,"max_records_per_patient":-1}}`)
	}))
	t.Cleanup(billing.Close)
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
	dotenv := fmt.Sprintf("DATABASE_URL='%s'\nGATEWAY_SHARED_SECRET=%s\nSERVICES_BILLING_SHARED_SECRET=%s\nSERVICES_BILLING_URL=%s\nPORT=%d\nSTORAGE_TYPE=local\nSTUDIES_STORAGE_DIR='%s'\n", backendDatabase.String(), strings.Repeat("g", 64), strings.Repeat("b", 64), billing.URL, port, filepath.Join(root, "studies"))
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
	client := &http.Client{Timeout: 2 * time.Second}
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
	if tables != 21 || specialties == 0 {
		t.Fatalf("health served with %d tables and %d specialties", tables, specialties)
	}
	probe := func(path string, expected int) {
		t.Helper()
		started := time.Now()
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/doctorchart/%s", port, path))
		if err != nil {
			t.Fatal("synthetic process probe failed", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != expected {
			t.Fatalf("%s probe status=%d body=%s error=%v", path, response.StatusCode, body, err)
		}
		t.Logf("%s probe status=%d", path, response.StatusCode)
		if path == "health" {
			state := "DOWN"
			if expected == http.StatusOK {
				state = "UP"
			}
			if string(body) != `{"service":"doctorchart","status":"`+state+`"}` || time.Since(started) > 1500*time.Millisecond {
				t.Fatal("readiness response was not fixed and bounded")
			}
		}
	}
	billingMu.Lock()
	initialBillingRequests := billingRequests
	billingMu.Unlock()
	disconnect(true)
	probe("health", http.StatusServiceUnavailable)
	probe("ping", http.StatusOK)
	disconnect(false)
	probe("health", http.StatusOK)
	probe("ping", http.StatusOK)
	billingMu.Lock()
	finalBillingRequests := billingRequests
	billingMu.Unlock()
	if finalBillingRequests != initialBillingRequests {
		t.Fatal("readiness unexpectedly contacted remote Billing")
	}
}
