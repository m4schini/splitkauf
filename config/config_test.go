// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/m4schini/splitkauf/config"
)

const (
	testHost     = "localhost"
	testPassword = "secret"
)

// Static bases for the lookup failures lookupConfigValue reports below.
var (
	errConfigKeyNotFound  = errors.New("no loaded config field matches key")
	errConfigKeyAmbiguous = errors.New("config key is ambiguous")
)

func TestDatabaseConfigDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.DatabaseConfig
		want string
	}{
		{
			name: "full config",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     config.ServiceName,
				Password: testPassword,
				Name:     config.ServiceName,
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='secret' dbname='splitkauf' sslmode='disable'",
		},
		{
			// An empty password must stay quoted so the parser does not swallow
			// the following dbname keyword into an unquoted empty value.
			name: "empty password stays quoted",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     config.ServiceName,
				Password: "",
				Name:     config.ServiceName,
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='' dbname='splitkauf' sslmode='disable'",
		},
		{
			name: "special characters are escaped",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     "spl it",
				Password: `pa'ss\word`,
				Name:     config.ServiceName,
				SSLMode:  "require",
			},
			want: `host='localhost' port='5432' user='spl it' password='pa\'ss\\word' dbname='splitkauf' sslmode='require'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.cfg.DSN(); got != tt.want {
				t.Errorf("DSN() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// Load is guarded by a sync.Once and publishes into the package global
// config.C, so at most ONE Load scenario is observable per process. Every
// scenario therefore runs in its own re-executed copy of this test binary:
// the parent builds an isolated working directory plus a scrubbed environment,
// the child calls Load and reports what happened as a single JSON line.
const (
	loadChildEnv      = "GO_TEST_SPLITKAUF_LOAD_CHILD"
	loadConcurrentEnv = "GO_TEST_SPLITKAUF_LOAD_GOROUTINES"
	loadResultMarker  = "SPLITKAUF-LOAD-RESULT:"
)

// loadReport is the child -> parent protocol.
type loadReport struct {
	// FirstErr / SecondErr are the messages of two sequential Load calls
	// ("" when the call returned nil). The second call documents the Once
	// semantics: it always returns nil, even when the first one failed.
	FirstErr  string `json:"firstErr"`
	SecondErr string `json:"secondErr"`
	// ConcurrentErrs holds one entry per goroutine in concurrency mode.
	ConcurrentErrs []string `json:"concurrentErrs"`
	// ConfigNil records whether the global config.C is still nil.
	ConfigNil bool `json:"configNil"`
	// Values is config.C flattened to dotted leaf paths, so the test can
	// assert on documented configuration keys instead of hard-coding Go
	// field names.
	Values map[string]string `json:"values"`
}

// TestLoadChildProcess is not a test of its own: it is the payload executed by
// the subprocesses spawned from TestLoad. It skips during a normal test run.
//
//nolint:paralleltest // the sole test running inside a re-exec'd child process; there is nothing to run beside it
func TestLoadChildProcess(t *testing.T) {
	if os.Getenv(loadChildEnv) != "1" {
		t.Skip("only runs as a re-executed child of TestLoad")
	}

	rep := loadReport{Values: map[string]string{}}

	if n, err := strconv.Atoi(os.Getenv(loadConcurrentEnv)); err == nil && n > 1 {
		errs := make([]string, n)
		start := make(chan struct{})

		var wg sync.WaitGroup
		for i := range errs {
			wg.Add(1)

			go func(i int) {
				defer wg.Done()

				<-start

				if err := config.Load(); err != nil {
					errs[i] = err.Error()
				}
			}(i)
		}

		close(start)
		wg.Wait()

		rep.ConcurrentErrs = errs
	} else {
		if err := config.Load(); err != nil {
			rep.FirstErr = err.Error()
		}

		if err := config.Load(); err != nil {
			rep.SecondErr = err.Error()
		}
	}

	rep.ConfigNil = config.C == nil
	if config.C != nil {
		flattenLoadedConfig("", reflect.ValueOf(config.C), rep.Values)
	}

	encoded, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshaling report: %v", err)
	}

	//nolint:forbidigo // stdout is the IPC channel back to the parent test process, not debug output
	fmt.Println(loadResultMarker + string(encoded))
}

func TestLoad(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	const (
		defaultsYAML  = "" // marker for "no config file at all"
		badYAML       = "app:\n  name: [unterminated\n   oops: :\n"
		emptyNamesCfg = "app:\n  name: \"\"\ndatabase:\n  name: \"\"\n"
	)

	tests := []struct {
		name string
		// files are written relative to the child's working directory.
		files map[string]string
		// env is layered on top of a scrubbed environment.
		env map[string]string
		// wantErrParts are substrings that must all appear in the error of
		// the first Load call. Empty means Load must succeed.
		wantErrParts []string
		// notWantErrParts must NOT appear in that error.
		notWantErrParts []string
		// wantValues are dotted config paths (matched against the tail of the
		// loaded field path) and their expected values.
		wantValues map[string]string
	}{
		{
			name: "defaults only",
			wantValues: map[string]string{
				"app.name":              "splitkauf",
				"app.log_level":         "info",
				"server.host":           "0.0.0.0",
				"server.port":           "8080",
				"metrics.enabled":       "false",
				"metrics.port":          "9090",
				"metrics.path":          "/metrics",
				"database.host":         "localhost",
				"database.port":         "5432",
				"database.user":         "splitkauf",
				"database.password":     "splitkauf",
				"database.name":         "splitkauf",
				"database.sslmode":      "disable",
				"oidc.issuer":           "",
				"oidc.client_id":        "",
				"oidc.client_secret":    "",
				"oidc.redirect_url":     "",
				"auth.session.lifetime": "168h0m0s",
				"cookie_secure":         "true",
			},
		},
		{
			name: "env overrides defaults including deeply nested keys",
			env: map[string]string{
				"SPLITKAUF_APP_LOG_LEVEL":           "debug",
				"SPLITKAUF_SERVER_PORT":             "9999",
				"SPLITKAUF_DATABASE_HOST":           "db.internal",
				"SPLITKAUF_AUTH_OIDC_ISSUER":        "https://idp.example.com",
				"SPLITKAUF_AUTH_OIDC_CLIENT_ID":     "client",
				"SPLITKAUF_AUTH_OIDC_CLIENT_SECRET": "shh",
				"SPLITKAUF_AUTH_OIDC_REDIRECT_URL":  "https://app.example.com/callback",
			},
			wantValues: map[string]string{
				"app.log_level":      "debug",
				"server.port":        "9999",
				"database.host":      "db.internal",
				"oidc.issuer":        "https://idp.example.com",
				"oidc.client_id":     "client",
				"oidc.client_secret": "shh",
				"oidc.redirect_url":  "https://app.example.com/callback",
				// untouched keys keep their defaults
				"server.host":   "0.0.0.0",
				"database.port": "5432",
			},
		},
		{
			name: "config file is merged and env wins over it",
			files: map[string]string{
				"config/config.yaml": "app:\n  name: from-file\nserver:\n  port: 1234\ndatabase:\n  name: filedb\n",
			},
			env: map[string]string{
				"SPLITKAUF_SERVER_PORT": "4321",
			},
			wantValues: map[string]string{
				"app.name":      "from-file",
				"server.port":   "4321",
				"database.name": "filedb",
				"app.log_level": "info",
			},
		},
		{
			// Documents current (buggy) behavior: the not-found check compares
			// against a zero-value viper.ConfigFileNotFoundError, which never
			// matches, so read errors of ANY kind are swallowed. If someone
			// fixes the inversion this test fails loudly.
			name: "malformed config file is silently ignored",
			files: map[string]string{
				"config/config.yaml": badYAML,
			},
			wantValues: map[string]string{
				"app.name":    "splitkauf",
				"server.port": "8080",
			},
		},
		{
			name: "non numeric port fails to unmarshal",
			env: map[string]string{
				"SPLITKAUF_SERVER_PORT": "notanumber",
			},
			wantErrParts: []string{"unmarshaling config"},
		},
		{
			name: "unparseable session lifetime fails to unmarshal",
			env: map[string]string{
				"SPLITKAUF_AUTH_SESSION_LIFETIME": "forever",
			},
			wantErrParts: []string{"unmarshaling config"},
		},
		{
			name:  "validation reports every violated rule at once",
			files: map[string]string{"config/config.yaml": emptyNamesCfg},
			env: map[string]string{
				"SPLITKAUF_APP_LOG_LEVEL": "verbose",
				"SPLITKAUF_SERVER_PORT":   "0",
			},
			wantErrParts: []string{
				"config validation",
				"config validation failed",
				"app.name",
				"app.log_level",
				"server.port",
				"database.name",
			},
		},
		{
			name: "port boundaries 1 and 65535 are accepted",
			env: map[string]string{
				"SPLITKAUF_SERVER_PORT":   "1",
				"SPLITKAUF_DATABASE_PORT": "65535",
			},
			wantValues: map[string]string{
				"server.port":   "1",
				"database.port": "65535",
			},
		},
		{
			name: "server port above range is rejected",
			env: map[string]string{
				"SPLITKAUF_SERVER_PORT": "65536",
			},
			wantErrParts:    []string{"config validation failed", "server.port"},
			notWantErrParts: []string{"database.port"},
		},
		{
			name: "database port zero is rejected",
			env: map[string]string{
				"SPLITKAUF_DATABASE_PORT": "0",
			},
			wantErrParts:    []string{"config validation failed", "database.port"},
			notWantErrParts: []string{"server.port"},
		},
		{
			name: "empty database user is rejected",
			files: map[string]string{
				"config/config.yaml": "database:\n  user: \"\"\n",
			},
			wantErrParts: []string{"config validation failed", "database.user"},
		},
		{
			// metrics rules only apply when metrics are enabled: a path without
			// a leading slash and a port colliding with the server port pass.
			name: "disabled metrics skip their validation rules",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: false\n  port: 8080\n  path: metrics\n",
			},
			wantValues: map[string]string{
				"metrics.enabled": "false",
				"metrics.port":    "8080",
				"metrics.path":    "metrics",
			},
		},
		{
			name: "enabled metrics surface port conflict and bad path",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 8080\n  path: metrics\n",
			},
			wantErrParts: []string{"config validation failed", "metrics.port", "metrics.path"},
		},
		{
			name: "enabled metrics reject out of range port",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 70000\n  path: /metrics\n",
			},
			wantErrParts: []string{"config validation failed", "metrics.port"},
		},
		{
			name: "oidc issuer without client details requires all three fields",
			env: map[string]string{
				"SPLITKAUF_AUTH_OIDC_ISSUER": "https://idp.example.com",
			},
			wantErrParts: []string{
				"config validation failed",
				"client_id",
				"client_secret",
				"redirect_url",
			},
			notWantErrParts: []string{"server.port", "database.name"},
		},
		{
			// Dev-auth mode: no issuer means the other OIDC fields stay optional.
			name: "empty oidc issuer imposes no auth requirements",
			files: map[string]string{
				"config/config.yaml": "auth:\n  oidc:\n    issuer: \"\"\n    client_id: \"\"\n" +
					"    client_secret: \"\"\n    redirect_url: \"\"\n",
			},
			wantValues: map[string]string{
				"oidc.issuer":    "",
				"oidc.client_id": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rep := runLoadInChild(t, tt.files, tt.env, 0)

			wantFailure := len(tt.wantErrParts) > 0
			switch {
			case wantFailure && rep.FirstErr == "":
				t.Fatalf("Load() = nil, want error containing %q", tt.wantErrParts)
			case !wantFailure && rep.FirstErr != "":
				t.Fatalf("Load() = %q, want nil", rep.FirstErr)
			}

			for _, part := range tt.wantErrParts {
				if !strings.Contains(rep.FirstErr, part) {
					t.Errorf("Load() error =\n  %q\nwant it to contain\n  %q", rep.FirstErr, part)
				}
			}

			for _, part := range tt.notWantErrParts {
				if strings.Contains(rep.FirstErr, part) {
					t.Errorf("Load() error =\n  %q\nwant it NOT to contain\n  %q", rep.FirstErr, part)
				}
			}

			if rep.ConfigNil != wantFailure {
				t.Errorf("config.C == nil is %v after Load(), want %v", rep.ConfigNil, wantFailure)
			}

			// sync.Once semantics: the second call always reports nil, even
			// after a failed first call that left config.C nil. Callers that
			// treat "nil error" as "config.C is usable" break here.
			if rep.SecondErr != "" {
				t.Errorf("second Load() = %q, want nil (sync.Once skips the closure)", rep.SecondErr)
			}

			if wantFailure {
				return
			}

			assertConfigValues(t, rep.Values, tt.wantValues)
		})
	}
}

// TestLoadConcurrentFirstCall does not fit the table: it needs many goroutines
// racing on the very first Load of a fresh process. Run the package with -race
// to make it meaningful — the child inherits the race detector from the parent
// binary.
func TestLoadConcurrentFirstCall(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	const goroutines = 16

	rep := runLoadInChild(t, nil, nil, goroutines)

	if got := len(rep.ConcurrentErrs); got != goroutines {
		t.Fatalf("got %d goroutine results, want %d", got, goroutines)
	}

	for i, msg := range rep.ConcurrentErrs {
		if msg != "" {
			t.Errorf("goroutine %d: Load() = %q, want nil", i, msg)
		}
	}

	if rep.ConfigNil {
		t.Fatal("config.C is nil after concurrent Load(), want a populated config")
	}

	assertConfigValues(t, rep.Values, map[string]string{
		"app.name":    "splitkauf",
		"server.port": "8080",
	})
}

// requireNoHostConfig skips when the machine running the tests has a system
// wide config that Load would merge into every scenario.
func requireNoHostConfig(t *testing.T) {
	t.Helper()

	for _, path := range []string{"/etc/splitkauf/config.yaml", "/etc/splitkauf/config.yml"} {
		if _, err := os.Stat(path); err == nil {
			t.Skipf("host has %s; Load would merge it into every scenario", path)
		}
	}
}

// runLoadInChild re-executes this test binary with an isolated working
// directory and a scrubbed environment, and returns the child's report.
// goroutines > 1 switches the child into concurrency mode.
func runLoadInChild(t *testing.T, files, env map[string]string, goroutines int) loadReport {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}

	// Load searches ./config relative to the working directory, and this repo
	// ships config/config.yaml — running from the repo root would silently
	// merge the real project config, so the child runs from a temp dir.
	dir := t.TempDir()
	writeConfigFiles(t, dir, files)

	//nolint:gosec // exe is os.Executable(), the current test binary re-exec'd, not attacker input
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestLoadChildProcess$", "-test.count=1")
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		// point XDG_CONFIG_HOME at an empty dir so a developer's real
		// ~/.config/splitkauf/config.yaml cannot leak in
		"XDG_CONFIG_HOME=" + t.TempDir(),
		loadChildEnv + "=1",
	}

	if goroutines > 1 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%d", loadConcurrentEnv, goroutines))
	}

	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process error = %v\noutput:\n%s", err, out)
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), loadResultMarker)
		if !ok {
			continue
		}

		var rep loadReport
		if err := json.Unmarshal([]byte(payload), &rep); err != nil {
			t.Fatalf("decoding child report %q: %v", payload, err)
		}

		return rep
	}

	t.Fatalf("child produced no %q line; output:\n%s", loadResultMarker, out)

	return loadReport{}
}

func writeConfigFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
}

// flattenLoadedConfig renders a config struct as dotted leaf paths keyed by the
// mapstructure tag (falling back to the lowercased field name).
func flattenLoadedConfig(prefix string, v reflect.Value, out map[string]string) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			out[prefix] = "<nil>"

			return
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		out[prefix] = fmt.Sprintf("%v", v.Interface())

		return
	}

	typ := v.Type()
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}

		name, _, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
		if name == "" {
			name = strings.ToLower(field.Name)
		}

		key := name
		if prefix != "" {
			key = prefix + "." + name
		}

		flattenLoadedConfig(key, v.Field(i), out)
	}
}

func assertConfigValues(t *testing.T, values, want map[string]string) {
	t.Helper()

	for key, wantVal := range want {
		gotVal, err := lookupConfigValue(values, key)
		if err != nil {
			t.Errorf("%v", err)

			continue
		}

		if !configValuesEqual(gotVal, wantVal) {
			t.Errorf("config %s = %q, want %q", key, gotVal, wantVal)
		}
	}
}

// lookupConfigValue resolves a documented config key against the flattened
// struct. Matching is done on the tail of the path and ignores underscores and
// case, so the assertions stay tied to configuration keys rather than to the
// exact Go field names and nesting.
func lookupConfigValue(values map[string]string, key string) (string, error) {
	want := normalizeConfigKey(key)

	var matches []string

	for k := range values {
		norm := normalizeConfigKey(k)
		if norm == want || strings.HasSuffix(norm, "."+want) {
			matches = append(matches, k)
		}
	}

	sort.Strings(matches)

	switch len(matches) {
	case 1:
		return values[matches[0]], nil
	case 0:
		known := make([]string, 0, len(values))
		for k := range values {
			known = append(known, k)
		}

		sort.Strings(known)

		return "", fmt.Errorf("%w %q; loaded fields: %v", errConfigKeyNotFound, key, known)
	default:
		return "", fmt.Errorf("%w %q, matches %v", errConfigKeyAmbiguous, key, matches)
	}
}

func normalizeConfigKey(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "_", ""))
}

// configValuesEqual compares durations by value ("168h" == "168h0m0s") and
// everything else literally.
func configValuesEqual(got, want string) bool {
	if got == want {
		return true
	}

	gotDur, gotErr := time.ParseDuration(got)
	wantDur, wantErr := time.ParseDuration(want)

	return gotErr == nil && wantErr == nil && gotDur == wantDur
}

// loadRuleCase is the shared shape of the focused Load tables below. They reuse
// the TestLoad harness (runLoadInChild) but keep one rule group per table so a
// failure names the rule that broke.
type loadRuleCase struct {
	name  string
	files map[string]string
	env   map[string]string
	// wantErrParts are substrings that must all appear in the first Load error;
	// empty means Load must succeed.
	wantErrParts []string
	// notWantErrParts must NOT appear in that error.
	notWantErrParts []string
	// wantValues are dotted config paths and their expected values, checked
	// only when Load is expected to succeed.
	wantValues map[string]string
}

// runLoadRuleCase executes one loadRuleCase in a pristine child process and
// applies the shared error/value assertions.
func runLoadRuleCase(t *testing.T, testCase loadRuleCase) {
	t.Helper()

	rep := runLoadInChild(t, testCase.files, testCase.env, 0)

	wantFailure := len(testCase.wantErrParts) > 0
	switch {
	case wantFailure && rep.FirstErr == "":
		t.Fatalf("Load() = nil, want error containing %q", testCase.wantErrParts)
	case !wantFailure && rep.FirstErr != "":
		t.Fatalf("Load() = %q, want nil", rep.FirstErr)
	}

	assertLoadErrParts(t, rep.FirstErr, testCase.wantErrParts, testCase.notWantErrParts)

	if rep.ConfigNil != wantFailure {
		t.Errorf("config.C == nil is %v after Load(), want %v", rep.ConfigNil, wantFailure)
	}

	if wantFailure {
		return
	}

	assertConfigValues(t, rep.Values, testCase.wantValues)
}

// assertLoadErrParts checks the joined validation message mentions every rule
// the case expects and none of the rules it must not have tripped.
func assertLoadErrParts(t *testing.T, got string, want, notWant []string) {
	t.Helper()

	for _, part := range want {
		if !strings.Contains(got, part) {
			t.Errorf("Load() error =\n  %q\nwant it to contain\n  %q", got, part)
		}
	}

	for _, part := range notWant {
		if strings.Contains(got, part) {
			t.Errorf("Load() error =\n  %q\nwant it NOT to contain\n  %q", got, part)
		}
	}
}

// TestLoadMetricsRules pins down the metrics rules individually: the enabled
// happy path, each failure mode on its own (so a single rule cannot hide behind
// another), and the port boundaries.
func TestLoadMetricsRules(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	tests := []loadRuleCase{
		{
			name: "enabled with distinct port and rooted path",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 9090\n  path: /metrics\n",
			},
			wantValues: map[string]string{
				"metrics.enabled": "true",
				"metrics.port":    "9090",
				"metrics.path":    "/metrics",
				"server.port":     "8080",
			},
		},
		{
			// Only the conflict rule may fire: the path is valid and the port
			// is inside the allowed range, it just equals server.port.
			name: "port colliding with the server port is the only violation",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 8080\n  path: /metrics\n",
			},
			wantErrParts:    []string{"config validation failed", "metrics.port"},
			notWantErrParts: []string{"metrics.path"},
		},
		{
			name: "empty path is rejected",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 9090\n  path: \"\"\n",
			},
			wantErrParts:    []string{"config validation failed", "metrics.path"},
			notWantErrParts: []string{"metrics.port"},
		},
		{
			name: "port lower bound is accepted",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 1\n  path: /metrics\n",
			},
			wantValues: map[string]string{"metrics.port": "1"},
		},
		{
			name: "port upper bound is accepted",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 65535\n  path: /metrics\n",
			},
			wantValues: map[string]string{"metrics.port": "65535"},
		},
		{
			name: "port zero is rejected",
			files: map[string]string{
				"config/config.yaml": "metrics:\n  enabled: true\n  port: 0\n  path: /metrics\n",
			},
			wantErrParts: []string{"config validation failed", "metrics.port"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runLoadRuleCase(t, tt)
		})
	}
}

// TestLoadLogLevelRule covers the log level rule on its own, including that the
// rejected value is echoed back to the operator.
func TestLoadLogLevelRule(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	tests := []loadRuleCase{
		{
			name:       "debug is accepted",
			env:        map[string]string{"SPLITKAUF_APP_LOG_LEVEL": "debug"},
			wantValues: map[string]string{"app.log_level": "debug"},
		},
		{
			name:       "warn is accepted",
			env:        map[string]string{"SPLITKAUF_APP_LOG_LEVEL": "warn"},
			wantValues: map[string]string{"app.log_level": "warn"},
		},
		{
			name:       "error is accepted",
			env:        map[string]string{"SPLITKAUF_APP_LOG_LEVEL": "error"},
			wantValues: map[string]string{"app.log_level": "error"},
		},
		{
			name: "trace is rejected and the offending value is reported",
			env:  map[string]string{"SPLITKAUF_APP_LOG_LEVEL": "trace"},
			wantErrParts: []string{
				"config validation failed",
				"app.log_level",
				"trace",
			},
			notWantErrParts: []string{"server.port"},
		},
		{
			// Levels are matched exactly, so casing is not normalised away.
			name: "uppercase level is rejected",
			env:  map[string]string{"SPLITKAUF_APP_LOG_LEVEL": "INFO"},
			wantErrParts: []string{
				"config validation failed",
				"app.log_level",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runLoadRuleCase(t, tt)
		})
	}
}

// TestLoadTypedEnvDecoding covers the decode hooks on the success side: the
// duration string that TestLoad only exercises with a broken value, plus the
// bool and int conversions that AutomaticEnv relies on defaults to discover.
func TestLoadTypedEnvDecoding(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	tests := []loadRuleCase{
		{
			name: "session lifetime duration string is decoded",
			env:  map[string]string{"SPLITKAUF_AUTH_SESSION_LIFETIME": "30m"},
			wantValues: map[string]string{
				"auth.session.lifetime": "30m0s",
			},
		},
		{
			name: "cookie secure bool is decoded",
			env:  map[string]string{"SPLITKAUF_AUTH_SESSION_COOKIE_SECURE": "false"},
			wantValues: map[string]string{
				"cookie_secure": "false",
			},
		},
		{
			name: "database credentials come from the environment",
			env: map[string]string{
				"SPLITKAUF_DATABASE_NAME":     "otherdb",
				"SPLITKAUF_DATABASE_USER":     "otheruser",
				"SPLITKAUF_DATABASE_PASSWORD": testPassword,
				"SPLITKAUF_DATABASE_SSL_MODE": "require",
			},
			wantValues: map[string]string{
				"database.name":     "otherdb",
				"database.user":     "otheruser",
				"database.password": testPassword,
				"database.sslmode":  "require",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runLoadRuleCase(t, tt)
		})
	}
}

const loadRememberEnv = "GO_TEST_SPLITKAUF_LOAD_REMEMBER"

// TestLoadRememberChildProcess is the payload of
// TestLoadRemembersFirstResult; it skips during a normal run. It cannot use the
// shared child because it has to mutate the environment *between* the two Load
// calls, which only the child process can do.
func TestLoadRememberChildProcess(t *testing.T) {
	if os.Getenv(loadRememberEnv) != "1" {
		t.Skip("only runs as a re-executed child of TestLoadRemembersFirstResult")
	}

	if err := config.Load(); err != nil {
		t.Fatalf("first Load() = %v, want nil", err)
	}

	first := config.C
	if first == nil {
		t.Fatal("config.C is nil after a successful Load()")
	}

	if first.Server.Port != 8080 {
		t.Fatalf("Server.Port = %v, want the default 8080", first.Server.Port)
	}

	// Anything the environment says from now on is invisible: the sync.Once
	// has already fired and the closure never runs again.
	t.Setenv("SPLITKAUF_SERVER_PORT", "9999")
	t.Setenv("SPLITKAUF_APP_LOG_LEVEL", "trace")

	if err := config.Load(); err != nil {
		t.Errorf("second Load() = %v, want nil", err)
	}

	if config.C != first {
		t.Error("second Load() replaced config.C, want the pointer published by the first call")
	}

	if config.C.Server.Port != 8080 {
		t.Errorf("Server.Port = %v after the second Load(), want 8080", config.C.Server.Port)
	}
}

// TestLoadRemembersFirstResult documents the memoisation: a second Load neither
// re-reads the environment nor republishes config.C.
func TestLoadRemembersFirstResult(t *testing.T) {
	t.Parallel()

	requireNoHostConfig(t)

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}

	//nolint:gosec // exe is os.Executable(), the current test binary re-exec'd, not attacker input
	cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestLoadRememberChildProcess$", "-test.count=1")
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"XDG_CONFIG_HOME=" + t.TempDir(),
		loadRememberEnv + "=1",
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process error = %v\noutput:\n%s", err, out)
	}
}
