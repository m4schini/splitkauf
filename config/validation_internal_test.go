// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestValidateApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		app  AppConfig
		// wantErrs lists the sentinel errors expected, in the exact order
		// validateApp appends them.
		wantErrs []error
		// wantSubstr, when non-empty, must appear in at least one returned
		// error message.
		wantSubstr string
	}{
		{
			name:     "valid with log level debug",
			app:      AppConfig{Name: ServiceName, LogLevel: "debug"},
			wantErrs: nil,
		},
		{
			name:     "valid with log level info",
			app:      AppConfig{Name: ServiceName, LogLevel: "info"},
			wantErrs: nil,
		},
		{
			name:     "valid with log level warn",
			app:      AppConfig{Name: ServiceName, LogLevel: "warn"},
			wantErrs: nil,
		},
		{
			name:     "valid with log level error",
			app:      AppConfig{Name: ServiceName, LogLevel: "error"},
			wantErrs: nil,
		},
		{
			name: "other fields are not validated here",
			app: AppConfig{
				Name:        ServiceName,
				Version:     "not-a-semver",
				Environment: "nonsense",
				Debug:       true,
				LogLevel:    "info",
				BaseURL:     ":://not a url",
			},
			wantErrs: nil,
		},
		{
			name:     "whitespace-only name is accepted (no trimming)",
			app:      AppConfig{Name: " ", LogLevel: "info"},
			wantErrs: nil,
		},
		{
			name:     "missing name only",
			app:      AppConfig{Name: "", LogLevel: "info"},
			wantErrs: []error{errAppNameRequired},
		},
		{
			name:       "empty log level only",
			app:        AppConfig{Name: ServiceName, LogLevel: ""},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got ""`,
		},
		{
			name:       "unknown log level only",
			app:        AppConfig{Name: ServiceName, LogLevel: "verbose"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "verbose"`,
		},
		{
			name:       "log level is case sensitive: INFO",
			app:        AppConfig{Name: ServiceName, LogLevel: "INFO"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "INFO"`,
		},
		{
			name:       "log level is case sensitive: Debug",
			app:        AppConfig{Name: ServiceName, LogLevel: "Debug"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "Debug"`,
		},
		{
			name:       "log level is case sensitive: WARN",
			app:        AppConfig{Name: ServiceName, LogLevel: "WARN"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "WARN"`,
		},
		{
			name:       "near miss log level warning",
			app:        AppConfig{Name: ServiceName, LogLevel: "warning"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "warning"`,
		},
		{
			name:       "near miss log level err",
			app:        AppConfig{Name: ServiceName, LogLevel: "err"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "err"`,
		},
		{
			name:       "unsupported log level trace",
			app:        AppConfig{Name: ServiceName, LogLevel: "trace"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "trace"`,
		},
		{
			name:       "unsupported log level fatal",
			app:        AppConfig{Name: ServiceName, LogLevel: "fatal"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got "fatal"`,
		},
		{
			name:       "log level is not trimmed",
			app:        AppConfig{Name: ServiceName, LogLevel: " info"},
			wantErrs:   []error{errLogLevelInvalid},
			wantSubstr: `got " info"`,
		},
		{
			name:       "zero value fails both checks, name error first",
			app:        AppConfig{},
			wantErrs:   []error{errAppNameRequired, errLogLevelInvalid},
			wantSubstr: `got ""`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := testCase.app

			errs := validateApp(&app)

			if len(errs) != len(testCase.wantErrs) {
				t.Fatalf("validateApp() = %v (%d errors), want %d errors",
					errs, len(errs), len(testCase.wantErrs))
			}

			for i, wantErr := range testCase.wantErrs {
				if !errors.Is(errs[i], wantErr) {
					t.Errorf("validateApp()[%d] = %v, want errors.Is(err, %v)", i, errs[i], wantErr)
				}
			}

			if testCase.wantSubstr == "" {
				return
			}

			if !containsErrSubstring(errs, testCase.wantSubstr) {
				t.Errorf("validateApp() = %v, want an error containing %q", errs, testCase.wantSubstr)
			}
		})
	}
}

// containsErrSubstring reports whether any error in errs has a message
// containing want.
func containsErrSubstring(errs []error, want string) bool {
	for _, err := range errs {
		if strings.Contains(err.Error(), want) {
			return true
		}
	}

	return false
}

func TestValidateServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		server  ServerConfig
		wantErr bool
	}{
		{
			name:    "zero value port is invalid",
			server:  ServerConfig{},
			wantErr: true,
		},
		{
			name:    "port 0 is invalid",
			server:  ServerConfig{Host: "127.0.0.1", Port: 0},
			wantErr: true,
		},
		{
			name:    "negative port is invalid",
			server:  ServerConfig{Host: "127.0.0.1", Port: -1},
			wantErr: true,
		},
		{
			name:    "port above max is invalid",
			server:  ServerConfig{Host: "127.0.0.1", Port: 65536},
			wantErr: true,
		},
		{
			name:    "port 1 is valid",
			server:  ServerConfig{Host: "127.0.0.1", Port: 1},
			wantErr: false,
		},
		{
			name:    "port 65535 is valid",
			server:  ServerConfig{Host: "127.0.0.1", Port: 65535},
			wantErr: false,
		},
		{
			name:    "typical port is valid",
			server:  ServerConfig{Host: "0.0.0.0", Port: 8080},
			wantErr: false,
		},
		{
			name:    "empty host is not validated",
			server:  ServerConfig{Host: "", Port: 8080},
			wantErr: false,
		},
		{
			name:    "arbitrary host is not validated",
			server:  ServerConfig{Host: "not a host!!", Port: 8080},
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := testCase.server

			errs := validateServer(&server)

			if !testCase.wantErr {
				if len(errs) != 0 {
					t.Fatalf("validateServer() = %v, want no errors", errs)
				}

				return
			}

			if len(errs) != 1 {
				t.Fatalf("validateServer() returned %d errors (%v), want exactly 1", len(errs), errs)
			}

			if !errors.Is(errs[0], errServerPortRange) {
				t.Errorf("validateServer() error = %v, want it to wrap errServerPortRange", errs[0])
			}

			wantSuffix := fmt.Sprintf(", got %d", server.Port)
			if !strings.Contains(errs[0].Error(), wantSuffix) {
				t.Errorf("validateServer() error message = %q, want it to contain %q", errs[0].Error(), wantSuffix)
			}
		})
	}
}

func TestValidateServerValidReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	server := ServerConfig{Host: "localhost", Port: 8080}

	if errs := validateServer(&server); len(errs) != 0 {
		t.Fatalf("validateServer() = %v, want empty result", errs)
	}
}

func TestValidateMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		metrics    MetricsConfig
		serverPort int
		wantErrs   []error
	}{
		{
			name:       "disabled with garbage port and empty path is valid",
			metrics:    MetricsConfig{Enabled: false, Port: 0, Path: ""},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name:       "disabled with negative port and bad path is valid",
			metrics:    MetricsConfig{Enabled: false, Port: -1, Path: "metrics"},
			serverPort: -1,
			wantErrs:   nil,
		},
		{
			name:       "enabled complete is valid",
			metrics:    MetricsConfig{Enabled: true, Host: "127.0.0.1", Port: 9090, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name:       "port lower boundary is valid",
			metrics:    MetricsConfig{Enabled: true, Port: 1, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name:       "port upper boundary is valid",
			metrics:    MetricsConfig{Enabled: true, Port: 65535, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name:       "root path is valid",
			metrics:    MetricsConfig{Enabled: true, Port: 9090, Path: "/"},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name:       "port zero is out of range",
			metrics:    MetricsConfig{Enabled: true, Port: 0, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortRange},
		},
		{
			name:       "port above maximum is out of range",
			metrics:    MetricsConfig{Enabled: true, Port: 65536, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortRange},
		},
		{
			name:       "negative port is out of range",
			metrics:    MetricsConfig{Enabled: true, Port: -1, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortRange},
		},
		{
			name:       "port equal to server port conflicts",
			metrics:    MetricsConfig{Enabled: true, Port: 8080, Path: "/metrics"},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortConflict},
		},
		{
			name:       "shared out of range port reports range and conflict",
			metrics:    MetricsConfig{Enabled: true, Port: 0, Path: "/metrics"},
			serverPort: 0,
			wantErrs:   []error{errMetricsPortRange, errMetricsPortConflict},
		},
		{
			name:       "empty path is rejected",
			metrics:    MetricsConfig{Enabled: true, Port: 9090, Path: ""},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPathFormat},
		},
		{
			name:       "path without leading slash is rejected",
			metrics:    MetricsConfig{Enabled: true, Port: 9090, Path: "metrics"},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPathFormat},
		},
		{
			name:       "all checks failing report three errors in order",
			metrics:    MetricsConfig{Enabled: true, Port: 0, Path: ""},
			serverPort: 0,
			wantErrs:   []error{errMetricsPortRange, errMetricsPortConflict, errMetricsPathFormat},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			metrics := testCase.metrics

			errs := validateMetrics(&metrics, testCase.serverPort)
			if len(errs) != len(testCase.wantErrs) {
				t.Fatalf("validateMetrics() = %v (%d errors), want %d errors",
					errs, len(errs), len(testCase.wantErrs))
			}

			for i, wantErr := range testCase.wantErrs {
				if !errors.Is(errs[i], wantErr) {
					t.Errorf("validateMetrics()[%d] = %v, want error matching %v", i, errs[i], wantErr)
				}
			}
		})
	}
}

// validDatabaseConfig returns a DatabaseConfig that passes validateDatabase.
func validDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "splitkauf",
		Password: "s3cret",
		Name:     "splitkauf",
		SSLMode:  "disable",
	}
}

func TestValidateDatabase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(database *DatabaseConfig)
		wantErrs []error
	}{
		{
			name:   "complete config is valid",
			mutate: func(_ *DatabaseConfig) {},
		},
		{
			name:   "port at minimum is valid",
			mutate: func(database *DatabaseConfig) { database.Port = 1 },
		},
		{
			name:   "port at maximum is valid",
			mutate: func(database *DatabaseConfig) { database.Port = 65535 },
		},
		{
			name:     "port zero is out of range",
			mutate:   func(database *DatabaseConfig) { database.Port = 0 },
			wantErrs: []error{errDatabasePortRange},
		},
		{
			name:     "negative port is out of range",
			mutate:   func(database *DatabaseConfig) { database.Port = -1 },
			wantErrs: []error{errDatabasePortRange},
		},
		{
			name:     "port above maximum is out of range",
			mutate:   func(database *DatabaseConfig) { database.Port = 65536 },
			wantErrs: []error{errDatabasePortRange},
		},
		{
			name:     "missing name is rejected",
			mutate:   func(database *DatabaseConfig) { database.Name = "" },
			wantErrs: []error{errDatabaseNameRequired},
		},
		{
			name:     "missing user is rejected",
			mutate:   func(database *DatabaseConfig) { database.User = "" },
			wantErrs: []error{errDatabaseUserRequired},
		},
		{
			name: "missing name and user are both reported",
			mutate: func(database *DatabaseConfig) {
				database.Name = ""
				database.User = ""
			},
			wantErrs: []error{errDatabaseNameRequired, errDatabaseUserRequired},
		},
		{
			name:   "empty host is not validated",
			mutate: func(database *DatabaseConfig) { database.Host = "" },
		},
		{
			name:   "empty password is not validated",
			mutate: func(database *DatabaseConfig) { database.Password = "" },
		},
		{
			name:   "empty ssl mode is not validated",
			mutate: func(database *DatabaseConfig) { database.SSLMode = "" },
		},
		{
			name:   "garbage ssl mode is not validated",
			mutate: func(database *DatabaseConfig) { database.SSLMode = "not-a-mode" },
		},
		{
			name:   "zero value reports port, name and user in order",
			mutate: func(database *DatabaseConfig) { *database = DatabaseConfig{} },
			wantErrs: []error{
				errDatabasePortRange,
				errDatabaseNameRequired,
				errDatabaseUserRequired,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database := validDatabaseConfig()
			testCase.mutate(&database)

			errs := validateDatabase(&database)
			if len(errs) != len(testCase.wantErrs) {
				t.Fatalf("validateDatabase() = %v (%d errors), want %d errors", errs, len(errs), len(testCase.wantErrs))
			}

			for i, want := range testCase.wantErrs {
				if !errors.Is(errs[i], want) {
					t.Errorf("validateDatabase()[%d] = %v, want error matching %v", i, errs[i], want)
				}
			}
		})
	}
}

func TestValidateDatabasePortErrorWrapsSentinel(t *testing.T) {
	t.Parallel()

	ports := []int{-1, 0, 65536}

	for _, port := range ports {
		t.Run(strconv.Itoa(port), func(t *testing.T) {
			t.Parallel()

			database := validDatabaseConfig()
			database.Port = port

			errs := validateDatabase(&database)
			if len(errs) != 1 {
				t.Fatalf("validateDatabase() = %v, want exactly 1 error", errs)
			}

			if !errors.Is(errs[0], errDatabasePortRange) {
				t.Fatalf("validateDatabase() error = %v, want errors.Is(errDatabasePortRange)", errs[0])
			}

			if errors.Unwrap(errs[0]) == nil {
				t.Errorf("validateDatabase() error = %v, want a wrapped sentinel, got a bare one", errs[0])
			}

			if got := errs[0].Error(); !strings.Contains(got, strconv.Itoa(port)) {
				t.Errorf("validateDatabase() error = %q, want it to mention port %d", got, port)
			}
		})
	}
}

// TestValidateAppValidReturnsNilSlice pins the success return to a nil slice
// rather than a non-nil empty one, for every accepted log level.
func TestValidateAppValidReturnsNilSlice(t *testing.T) {
	t.Parallel()

	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			t.Parallel()

			app := AppConfig{Name: ServiceName, LogLevel: level}

			if errs := validateApp(&app); errs != nil {
				t.Fatalf("validateApp() = %v, want nil slice", errs)
			}
		})
	}
}

// TestValidateAppErrorWrappingShape checks which of the two failures returns a
// bare sentinel and which wraps one with the offending value.
func TestValidateAppErrorWrappingShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		app  AppConfig
		want error
		// wantWrapped reports whether the returned error must wrap want
		// instead of being the sentinel itself.
		wantWrapped bool
		wantSubstr  string
	}{
		{
			name:        "missing name is a bare sentinel",
			app:         AppConfig{Name: "", LogLevel: "info"},
			want:        errAppNameRequired,
			wantWrapped: false,
		},
		{
			name:        "invalid log level wraps the sentinel",
			app:         AppConfig{Name: ServiceName, LogLevel: "verbose"},
			want:        errLogLevelInvalid,
			wantWrapped: true,
			wantSubstr:  `got "verbose"`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := testCase.app

			errs := validateApp(&app)
			if len(errs) != 1 {
				t.Fatalf("validateApp() = %v, want exactly 1 error", errs)
			}

			if !errors.Is(errs[0], testCase.want) {
				t.Fatalf("validateApp() error = %v, want errors.Is(err, %v)", errs[0], testCase.want)
			}

			if gotWrapped := errors.Unwrap(errs[0]) != nil; gotWrapped != testCase.wantWrapped {
				t.Errorf("validateApp() error = %v, wrapped = %t, want wrapped %t",
					errs[0], gotWrapped, testCase.wantWrapped)
			}

			if testCase.wantSubstr == "" {
				return
			}

			if !containsErrSubstring(errs, testCase.wantSubstr) {
				t.Errorf("validateApp() = %v, want an error containing %q", errs, testCase.wantSubstr)
			}
		})
	}
}

// TestValidateAppNilPanics documents the absence of a nil guard. The sole
// caller passes &cfg.App, so this is unreachable in production.
func TestValidateAppNilPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateApp(nil) did not panic, want nil-pointer dereference")
		}
	}()

	validateApp(nil)
}

func TestValidateServerPortRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		server  ServerConfig
		wantErr bool
	}{
		{
			name:    "zero value fails",
			server:  ServerConfig{},
			wantErr: true,
		},
		{
			name:    "port below minimum fails",
			server:  ServerConfig{Host: "localhost", Port: 0},
			wantErr: true,
		},
		{
			name:    "negative port fails",
			server:  ServerConfig{Host: "localhost", Port: -1},
			wantErr: true,
		},
		{
			name:    "port above maximum fails",
			server:  ServerConfig{Host: "localhost", Port: 65536},
			wantErr: true,
		},
		{
			name:    "minimum port is valid",
			server:  ServerConfig{Host: "localhost", Port: 1},
			wantErr: false,
		},
		{
			name:    "maximum port is valid",
			server:  ServerConfig{Host: "localhost", Port: 65535},
			wantErr: false,
		},
		{
			name:    "typical port is valid",
			server:  ServerConfig{Host: "0.0.0.0", Port: 8080},
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := testCase.server

			errs := validateServer(&server)

			if !testCase.wantErr {
				if len(errs) != 0 {
					t.Fatalf("validateServer() = %v, want no errors", errs)
				}

				return
			}

			if len(errs) != 1 {
				t.Fatalf("validateServer() returned %d errors (%v), want exactly 1", len(errs), errs)
			}

			if !errors.Is(errs[0], errServerPortRange) {
				t.Fatalf("validateServer() error = %v, want errors.Is(err, errServerPortRange)", errs[0])
			}

			want := fmt.Sprintf("got %d", server.Port)
			if !strings.Contains(errs[0].Error(), want) {
				t.Fatalf("validateServer() error = %q, want it to contain %q", errs[0].Error(), want)
			}
		})
	}
}

func TestValidateServerIgnoresHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
	}{
		{name: "empty host", host: ""},
		{name: "whitespace host", host: "   "},
		{name: "garbage host", host: "not a host!!"},
		{name: "hostname", host: "localhost"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := ServerConfig{Host: testCase.host, Port: 8080}

			if errs := validateServer(&server); len(errs) != 0 {
				t.Fatalf("validateServer() = %v, want no errors (host is not validated)", errs)
			}
		})
	}
}

func TestValidateServerSuccessReturnsNilSlice(t *testing.T) {
	t.Parallel()

	server := ServerConfig{Host: "localhost", Port: 8080}

	errs := validateServer(&server)
	if errs != nil {
		t.Fatalf("validateServer() = %v, want nil slice on success", errs)
	}
}

// TestValidateMetricsContractGaps covers behaviour of validateMetrics that the
// main TestValidateMetrics table does not exercise: serverPort is only compared,
// never range-checked; only the first byte of Path is inspected; and the
// range+path error pairing without a port conflict.
func TestValidateMetricsContractGaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		metrics    MetricsConfig
		serverPort int
		wantErrs   []error
	}{
		{
			name: "server port out of range low is not validated here",
			metrics: MetricsConfig{
				Enabled: true,
				Host:    "127.0.0.1",
				Port:    9090,
				Path:    "/metrics",
			},
			serverPort: 0,
			wantErrs:   nil,
		},
		{
			name: "server port out of range high is not validated here",
			metrics: MetricsConfig{
				Enabled: true,
				Host:    "127.0.0.1",
				Port:    9090,
				Path:    "/metrics",
			},
			serverPort: 70000,
			wantErrs:   nil,
		},
		{
			name: "server port negative is not validated here",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "/metrics",
			},
			serverPort: -1,
			wantErrs:   nil,
		},
		{
			name: "double slash path is accepted",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "//",
			},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name: "trailing slash path is accepted",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "/metrics/",
			},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name: "path with space after leading slash is accepted",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    9090,
				Path:    "/ metrics",
			},
			serverPort: 8080,
			wantErrs:   nil,
		},
		{
			name: "range and path errors without conflict",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    0,
				Path:    "",
			},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortRange, errMetricsPathFormat},
		},
		{
			name: "high out of range port with slashless path",
			metrics: MetricsConfig{
				Enabled: true,
				Port:    65536,
				Path:    "metrics",
			},
			serverPort: 8080,
			wantErrs:   []error{errMetricsPortRange, errMetricsPathFormat},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			metrics := testCase.metrics

			errs := validateMetrics(&metrics, testCase.serverPort)

			if len(errs) != len(testCase.wantErrs) {
				t.Fatalf("validateMetrics() errors = %v, want %v", errs, testCase.wantErrs)
			}

			for i, wantErr := range testCase.wantErrs {
				if !errors.Is(errs[i], wantErr) {
					t.Errorf("validateMetrics() error[%d] = %v, want %v", i, errs[i], wantErr)
				}
			}
		})
	}
}

func TestValidateDatabaseRequiredErrorsAreBareSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(database *DatabaseConfig)
		want   error
	}{
		{
			name:   "name",
			mutate: func(database *DatabaseConfig) { database.Name = "" },
			want:   errDatabaseNameRequired,
		},
		{
			name:   "user",
			mutate: func(database *DatabaseConfig) { database.User = "" },
			want:   errDatabaseUserRequired,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database := validDatabaseConfig()
			testCase.mutate(&database)

			errs := validateDatabase(&database)
			if len(errs) != 1 {
				t.Fatalf("validateDatabase() = %v, want exactly 1 error", errs)
			}

			if errors.Unwrap(errs[0]) != nil {
				t.Errorf("validateDatabase() error = %v, want the bare sentinel %v", errs[0], testCase.want)
			}

			if !errors.Is(errs[0], testCase.want) {
				t.Errorf("validateDatabase() error = %v, want %v", errs[0], testCase.want)
			}
		})
	}
}
