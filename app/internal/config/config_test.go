package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lookup turns a map into the getenv shape Load expects.
func lookup(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// validCA writes a placeholder CA file and returns the environment that
// satisfies every required value.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	return map[string]string{
		EnvJMAPURL:      "https://mail.example.test/jmap/",
		EnvJMAPHostname: "mail.example.test",
		EnvCAFile:       ca,
		EnvDBPassword:   "local-test-password",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(lookup(validEnv(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.JMAPURL.String(), "https://mail.example.test/jmap/"; got != want {
		t.Errorf("JMAPURL = %q, want %q", got, want)
	}
	if got, want := cfg.JMAPHostname, "mail.example.test"; got != want {
		t.Errorf("JMAPHostname = %q, want %q", got, want)
	}
	if got, want := cfg.ListenAddr, DefaultListenAddr; got != want {
		t.Errorf("ListenAddr = %q, want %q", got, want)
	}
	if got, want := cfg.RequestTimeout, DefaultRequestTimeout; got != want {
		t.Errorf("RequestTimeout = %v, want %v", got, want)
	}
	if got, want := cfg.PageSizeDefault, DefaultPageSizeDefault; got != want {
		t.Errorf("PageSizeDefault = %d, want %d", got, want)
	}
	if got, want := cfg.PageSizeMax, DefaultPageSizeMax; got != want {
		t.Errorf("PageSizeMax = %d, want %d", got, want)
	}
	if got, want := cfg.BodyMaxBytes, int64(DefaultBodyMaxBytes); got != want {
		t.Errorf("BodyMaxBytes = %d, want %d", got, want)
	}
	if got, want := cfg.DBHost, DefaultDBHost; got != want {
		t.Errorf("DBHost = %q, want %q", got, want)
	}
	if got, want := cfg.DBPort, DefaultDBPort; got != want {
		t.Errorf("DBPort = %d, want %d", got, want)
	}
	if got, want := cfg.DBName, DefaultDBName; got != want {
		t.Errorf("DBName = %q, want %q", got, want)
	}
	if got, want := cfg.DBUser, DefaultDBUser; got != want {
		t.Errorf("DBUser = %q, want %q", got, want)
	}
	if got, want := cfg.DBPassword, "local-test-password"; got != want {
		t.Errorf("DBPassword = %q, want %q", got, want)
	}
	if got, want := cfg.IdempotencyTTL, DefaultIdempotencyTTL; got != want {
		t.Errorf("IdempotencyTTL = %v, want %v", got, want)
	}
	if got, want := cfg.IdempotencyLease, DefaultIdempotencyLease; got != want {
		t.Errorf("IdempotencyLease = %v, want %v", got, want)
	}
	if got, want := cfg.SendMaxRecipients, DefaultSendMaxRecipients; got != want {
		t.Errorf("SendMaxRecipients = %d, want %d", got, want)
	}
	if got, want := cfg.SendMaxBytes, int64(DefaultSendMaxBytes); got != want {
		t.Errorf("SendMaxBytes = %d, want %d", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := validEnv(t)
	env[EnvJMAPURL] = "https://mailflow-stalwart/jmap" // no trailing slash
	env[EnvJMAPHostname] = "mail.mailflow.test"
	env[EnvListenAddr] = "0.0.0.0:8080"
	env[EnvRequestTimeout] = "3s"
	env[EnvPageSizeDefault] = "10"
	env[EnvPageSizeMax] = "50"
	env[EnvBodyMaxBytes] = "1024"
	env[EnvDBHost] = "postgres.internal"
	env[EnvDBPort] = "6543"
	env[EnvDBName] = "mailflow_state"
	env[EnvDBUser] = "mailflow_app"
	env[EnvDBPassword] = "p@ss&word|with\\odd-characters"
	env[EnvIdempotencyTTL] = "48h"
	env[EnvIdempotencyLease] = "5m"
	env[EnvSendMaxRecipients] = "10"
	env[EnvSendMaxBytes] = "4096"

	cfg, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.JMAPURL.String(), "https://mailflow-stalwart/jmap/"; got != want {
		t.Errorf("JMAPURL = %q, want normalized %q", got, want)
	}
	if got, want := cfg.ListenAddr, "0.0.0.0:8080"; got != want {
		t.Errorf("ListenAddr = %q, want %q", got, want)
	}
	if got, want := cfg.RequestTimeout, 3*time.Second; got != want {
		t.Errorf("RequestTimeout = %v, want %v", got, want)
	}
	if got, want := cfg.PageSizeDefault, 10; got != want {
		t.Errorf("PageSizeDefault = %d, want %d", got, want)
	}
	if got, want := cfg.PageSizeMax, 50; got != want {
		t.Errorf("PageSizeMax = %d, want %d", got, want)
	}
	if got, want := cfg.BodyMaxBytes, int64(1024); got != want {
		t.Errorf("BodyMaxBytes = %d, want %d", got, want)
	}
	if got, want := cfg.DBHost, "postgres.internal"; got != want {
		t.Errorf("DBHost = %q, want %q", got, want)
	}
	if got, want := cfg.DBPort, 6543; got != want {
		t.Errorf("DBPort = %d, want %d", got, want)
	}
	if got, want := cfg.DBName, "mailflow_state"; got != want {
		t.Errorf("DBName = %q, want %q", got, want)
	}
	if got, want := cfg.DBUser, "mailflow_app"; got != want {
		t.Errorf("DBUser = %q, want %q", got, want)
	}
	if got, want := cfg.DBPassword, "p@ss&word|with\\odd-characters"; got != want {
		t.Errorf("DBPassword = %q, want the value verbatim", got)
	}
	if got, want := cfg.IdempotencyTTL, 48*time.Hour; got != want {
		t.Errorf("IdempotencyTTL = %v, want %v", got, want)
	}
	if got, want := cfg.IdempotencyLease, 5*time.Minute; got != want {
		t.Errorf("IdempotencyLease = %v, want %v", got, want)
	}
	if got, want := cfg.SendMaxRecipients, 10; got != want {
		t.Errorf("SendMaxRecipients = %d, want %d", got, want)
	}
	if got, want := cfg.SendMaxBytes, int64(4096); got != want {
		t.Errorf("SendMaxBytes = %d, want %d", got, want)
	}
}

func TestLoadRejects(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name   string
		mutate func(t *testing.T, env map[string]string)
		want   string // substring the error must name
	}{
		{
			name:   "missing JMAP URL",
			mutate: func(_ *testing.T, env map[string]string) { delete(env, EnvJMAPURL) },
			want:   EnvJMAPURL + ": required",
		},
		{
			name:   "plain HTTP URL",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPURL] = "http://mail.example.test/jmap/" },
			want:   "must use https",
		},
		{
			name:   "URL without host",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPURL] = "https:///jmap/" },
			want:   "missing host",
		},
		{
			name: "URL with credentials",
			mutate: func(_ *testing.T, env map[string]string) {
				env[EnvJMAPURL] = "https://user:pass@mail.example.test/jmap/"
			},
			want: "must not contain credentials",
		},
		{
			name:   "URL with query",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPURL] = "https://mail.example.test/jmap/?x=1" },
			want:   "must not contain a query",
		},
		{
			name:   "relative URL",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPURL] = "/jmap/" },
			want:   "must use https",
		},
		{
			name:   "missing hostname",
			mutate: func(_ *testing.T, env map[string]string) { delete(env, EnvJMAPHostname) },
			want:   EnvJMAPHostname + ": required",
		},
		{
			name:   "hostname with scheme",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPHostname] = "https://mail.example.test" },
			want:   "bare hostname",
		},
		{
			name:   "hostname with port",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvJMAPHostname] = "mail.example.test:443" },
			want:   "must not include a port",
		},
		{
			name:   "missing CA file",
			mutate: func(_ *testing.T, env map[string]string) { delete(env, EnvCAFile) },
			want:   EnvCAFile + ": required",
		},
		{
			name:   "CA file does not exist",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvCAFile] = filepath.Join(dir, "absent.pem") },
			want:   EnvCAFile + ":",
		},
		{
			name:   "CA path is a directory",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvCAFile] = dir },
			want:   "not a regular file",
		},
		{
			name:   "listen address without port",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvListenAddr] = "127.0.0.1" },
			want:   EnvListenAddr + ": must be host:port",
		},
		{
			name:   "listen address with bad port",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvListenAddr] = "127.0.0.1:http" },
			want:   "invalid port",
		},
		{
			name:   "timeout is not a duration",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvRequestTimeout] = "ten seconds" },
			want:   EnvRequestTimeout + ": not a duration",
		},
		{
			name:   "timeout is zero",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvRequestTimeout] = "0s" },
			want:   "must be positive",
		},
		{
			name:   "page size is not a number",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvPageSizeDefault] = "many" },
			want:   EnvPageSizeDefault + ": not an integer",
		},
		{
			name:   "page size is zero",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvPageSizeMax] = "0" },
			want:   "between 1 and 1000",
		},
		{
			name: "default page size exceeds maximum",
			mutate: func(_ *testing.T, env map[string]string) {
				env[EnvPageSizeDefault] = "80"
				env[EnvPageSizeMax] = "20"
			},
			want: "exceeds",
		},
		{
			name:   "body cap is not a number",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvBodyMaxBytes] = "1MiB" },
			want:   EnvBodyMaxBytes + ": not an integer",
		},
		{
			name:   "body cap is negative",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvBodyMaxBytes] = "-1" },
			want:   "must be positive",
		},
		{
			name:   "missing database password",
			mutate: func(_ *testing.T, env map[string]string) { delete(env, EnvDBPassword) },
			want:   EnvDBPassword + ": required",
		},
		{
			name:   "database host carries a port",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvDBHost] = "db:5432" },
			want:   EnvDBHost + ":",
		},
		{
			name:   "database host is a URL",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvDBHost] = "postgres://db" },
			want:   EnvDBHost + ":",
		},
		{
			name:   "database port is not a number",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvDBPort] = "five" },
			want:   EnvDBPort + ": not an integer",
		},
		{
			name:   "database port is out of range",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvDBPort] = "70000" },
			want:   EnvDBPort + ": must be 1-65535",
		},
		{
			name:   "database name carries whitespace",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvDBName] = "mail flow" },
			want:   EnvDBName + ": must not contain",
		},
		{
			name:   "idempotency lease is shorter than the request timeout",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvIdempotencyLease] = "5s" },
			want:   EnvIdempotencyLease + ":",
		},
		{
			name: "idempotency retention is shorter than the lease",
			mutate: func(_ *testing.T, env map[string]string) {
				env[EnvIdempotencyLease] = "10m"
				env[EnvIdempotencyTTL] = "5m"
			},
			want: EnvIdempotencyTTL + ":",
		},
		{
			name:   "send recipient cap is not a number",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvSendMaxRecipients] = "many" },
			want:   EnvSendMaxRecipients + ": not an integer",
		},
		{
			name:   "send body cap exceeds the hard limit",
			mutate: func(_ *testing.T, env map[string]string) { env[EnvSendMaxBytes] = "999999999" },
			want:   EnvSendMaxBytes + ": must be between",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := validEnv(t)
			tc.mutate(t, env)
			_, err := Load(lookup(env))
			if err == nil {
				t.Fatalf("Load succeeded, want error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	_, err := Load(lookup(map[string]string{}))
	if err == nil {
		t.Fatal("Load succeeded with an empty environment")
	}
	for _, name := range []string{EnvJMAPURL, EnvJMAPHostname, EnvCAFile, EnvDBPassword} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name %s: %v", name, err)
		}
	}
}
