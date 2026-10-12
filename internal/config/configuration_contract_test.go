package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationDirectoryAndSecretPrecedence(t *testing.T) {
	const rootEnv = "GATEWAY_SHARED_SECRET=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nSERVICES_BILLING_SHARED_SECRET=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\nSPRING_DATASOURCE_URL=jdbc:postgresql://localhost:5432/fixture\nDB_USERNAME=fixture\nDB_PASSWORD=fixture\nSERVICES_BILLING_URL=http://localhost:8081\n"
	const canonical = "GATEWAY_SHARED_SECRET"
	const alias = "APP_SECURITY_GATEWAY_SECRET"
	const fileSecret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const otherSecret = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	for _, tc := range []struct {
		name                                                                                                   string
		command, local, malformed, legacyFile, canonicalConflict, process, legacyProcess, empty, absent, equal bool
	}{
		{name: "module root"},
		{name: "command loads module root", command: true},
		{name: "current file takes priority", command: true, local: true},
		{name: "malformed current file prevents fallback", command: true, local: true, malformed: true},
		{name: "legacy file alias", command: true, legacyFile: true},
		{name: "canonical file wins over legacy alias", canonicalConflict: true},
		{name: "process wins over file", command: true, process: true},
		{name: "legacy process wins over canonical file", command: true, legacyProcess: true},
		{name: "canonical process wins over legacy process", process: true, legacyProcess: true},
		{name: "explicitly empty process secret rejected", process: true, empty: true},
		{name: "absent dotenv uses process", absent: true, process: true},
		{name: "gateway and billing must differ", equal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			command := filepath.Join(root, "cmd", "server")
			if err := os.MkdirAll(command, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// Isolate process configuration from local developer credentials.
			names := []string{"GATEWAY_SHARED_SECRET", "APP_SECURITY_GATEWAY_SECRET", "APP_SECURITY_GATEWAYSECRET", "app.security.gateway-secret", "SERVICES_BILLING_SHARED_SECRET", "SERVICES_POINTLY_SHARED_SECRET", "services.billing.shared-secret", "services.pointly.shared-secret", "SERVICES_BILLING_SHAREDSECRET"}
			for _, line := range strings.Split(rootEnv, "\n") {
				if name, _, ok := strings.Cut(line, "="); ok {
					names = append(names, name)
				}
			}
			for _, name := range names {
				t.Setenv(name, "")
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}
			body := rootEnv
			if tc.legacyFile {
				body = strings.Replace(body, canonical+"=", alias+"=", 1)
			}
			if tc.canonicalConflict {
				body += alias + "=" + otherSecret + "\n"
			}
			if tc.equal {
				body = strings.ReplaceAll(body, strings.Repeat("b", 64), fileSecret)
			}
			if !tc.absent {
				if err := os.WriteFile(filepath.Join(root, ".env"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				for _, line := range strings.Split(rootEnv, "\n") {
					if name, value, ok := strings.Cut(line, "="); ok {
						t.Setenv(name, value)
					}
				}
			}
			if tc.local {
				localBody := strings.Replace(body, fileSecret, otherSecret, 1)
				if tc.malformed {
					localBody = canonical + "=\"DO_NOT_EXPOSE_THIS_CREDENTIAL"
				}
				if err := os.WriteFile(filepath.Join(command, ".env"), []byte(localBody), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.legacyProcess {
				t.Setenv(alias, strings.Repeat("d", 64))
			}
			if tc.process {
				value := otherSecret
				if tc.empty {
					value = ""
				}
				t.Setenv(canonical, value)
			}
			if tc.command {
				t.Chdir(command)
			} else {
				t.Chdir(root)
			}
			read := func() (string, error) {
				cfg, err := Load()
				return cfg.GatewaySecret, err
			}
			secret, err := read()
			if tc.malformed || tc.empty || tc.equal {
				if err == nil {
					t.Fatal("invalid configuration accepted")
				}
				if strings.Contains(err.Error(), "DO_NOT_EXPOSE_THIS_CREDENTIAL") || strings.Contains(err.Error(), otherSecret) {
					t.Fatal("error exposed a credential")
				}
				if tc.empty && (!strings.Contains(err.Error(), canonical) || !strings.Contains(err.Error(), ".env")) {
					t.Fatal("missing configuration guidance")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := fileSecret
			if tc.local || tc.process {
				want = otherSecret
			} else if tc.legacyProcess {
				want = strings.Repeat("d", 64)
			}
			if secret != want {
				t.Fatal("secret precedence differs")
			}
		})
	}
}
