package main

import (
	"strings"
	"testing"
	"time"
)

func env(overrides map[string]string) func(string) string {
	return func(name string) string { return overrides[name] }
}

func TestLoadConfigRequiresBrokerCredentialsAndAPassword(t *testing.T) {
	if _, err := loadConfig(env(map[string]string{})); err == nil {
		t.Fatal("accepted empty configuration")
	}
	base := map[string]string{
		"MAILFLOW_DB_PASSWORD":  "local-test-only",
		"MAILFLOW_RABBITMQ_URL": "amqp://user:pass@rabbitmq:5672/",
	}
	if _, err := loadConfig(env(base)); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	for name, value := range map[string]string{
		"MAILFLOW_RABBITMQ_URL": "https://elsewhere/",
		"MAILFLOW_WORKER_POLL":  "0s",
	} {
		cfg := map[string]string{}
		for k, v := range base {
			cfg[k] = v
		}
		cfg[name] = value
		if _, err := loadConfig(env(cfg)); err == nil {
			t.Fatalf("accepted invalid %s=%q", name, value)
		}
	}
}

func TestLoadConfigUsesDocumentedDefaults(t *testing.T) {
	cfg, err := loadConfig(env(map[string]string{
		"MAILFLOW_DB_PASSWORD":  "local-test-only",
		"MAILFLOW_RABBITMQ_URL": "amqp://user:pass@rabbitmq:5672/",
	}))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.db.Host != "db" || cfg.db.Name != "mailflow" || cfg.db.User != "mailflow" || cfg.db.Port != 5432 {
		t.Fatalf("db config = %+v", cfg.db)
	}
	if cfg.queue != defaultQueue || cfg.poll != defaultPoll {
		t.Fatalf("queue = %q poll = %v", cfg.queue, cfg.poll)
	}
	if strings.Contains(cfg.queueURL, cfg.db.Password) {
		t.Fatal("queue URL carries the database password")
	}
	if cfg.poll > time.Minute {
		t.Fatal("poll is unbounded")
	}
}
