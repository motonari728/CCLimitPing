package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wavever/CCLimitPing/internal/codexstate"
	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/usage"
)

func fakeCodexCLI(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("native Windows PTY is unsupported")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func quotaResponse(reset int64) string {
	return fmt.Sprintf(`{"plan_type":"pro","rate_limit":{"primary_window":{"used_percent":0,"limit_window_seconds":604800,"reset_at":%d}}}`, reset)
}

func TestVerifiedPingReadsBeforeAndAfterWithoutWaitingMinute(t *testing.T) {
	fakeCodexHome(t)
	fakeCodexCLI(t, "exit 0")
	old := usageHTTPClient
	defer func() { usageHTTPClient = old }()
	reads := 0
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/usage") {
			t.Fatalf("unexpected detail read %s", req.URL.Path)
		}
		reads++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(quotaResponse(time.Now().Add(7 * 24 * time.Hour).Unix()))), Header: make(http.Header)}, nil
	})}
	start := time.Now()
	res, err := NewCodex(config.ProviderConfig{Enabled: true}).Trigger(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatalf("reads=%d", reads)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("manual ping blocked")
	}
	if res.Verification == nil || res.Verification.Weekly.State != codexstate.Unknown || res.Verification.Weekly.DueAt.IsZero() {
		t.Fatalf("%+v", res.Verification)
	}
}

func TestFailedTriggerStillReadsQuotaAfterwards(t *testing.T) {
	fakeCodexHome(t)
	fakeCodexCLI(t, "exit 1")
	old := usageHTTPClient
	defer func() { usageHTTPClient = old }()
	reads := 0
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		reads++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(quotaResponse(time.Now().Add(7 * 24 * time.Hour).Unix()))), Header: make(http.Header)}, nil
	})}
	res, err := NewCodex(config.ProviderConfig{}).Trigger(context.Background(), false)
	if err == nil || reads != 2 || res.Verification == nil {
		t.Fatal(err, reads, res)
	}
}

func TestAccountSwitchReloadsIdentity(t *testing.T) {
	fakeCodexHome(t)
	old := usageHTTPClient
	defer func() { usageHTTPClient = old }()
	expected := "account-123"
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("ChatGPT-Account-Id") != expected {
			t.Fatalf("stale account header: %s", req.Header.Get("ChatGPT-Account-Id"))
		}
		body := quotaResponse(time.Now().Add(7 * 24 * time.Hour).Unix())
		if strings.HasSuffix(req.URL.Path, "reset-credits") {
			body = `{"available_count":0}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	c := NewCodex(config.ProviderConfig{})
	if _, err := c.ReadUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected = "account-new"
	if err := os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"), []byte(`{"tokens":{"access_token":"different-valid-token","account_id":"account-new"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	u, err := c.ReadUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Verification.Weekly.State != codexstate.Unknown {
		t.Fatal(u.Verification)
	}
}

func TestDryRunNeverReadsOrWritesState(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	old := usageHTTPClient
	defer func() { usageHTTPClient = old }()
	usageHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("dry run read usage"); return nil, nil })}
	if _, err := NewCodex(config.ProviderConfig{}).Trigger(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}

func TestAutoRedeemRejectsChangedObservationAccount(t *testing.T) {
	fakeCodexHome(t)
	requests := 0
	useTransport(t, func(req *http.Request) (*http.Response, error) {
		requests++
		t.Fatal("must not spend another account's credit")
		return nil, nil
	})
	u := &usage.Usage{QuotaAccount: "previous-account", ResetCredits: &usage.ResetCredits{Credits: []usage.ResetCredit{
		{Status: "available", ExpiresAt: time.Now().Add(30 * time.Minute)},
	}}}
	_, err := NewCodex(config.ProviderConfig{}).AutoRedeemResetCredit(context.Background(), u)
	if err == nil || requests != 0 {
		t.Fatal(err, requests)
	}
}

func TestAutoRedeemRechecksIdentityOnAuthenticationRetry(t *testing.T) {
	fakeCodexHome(t)
	requests := 0
	useTransport(t, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			t.Fatal("retried redemption against changed account")
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "auth.json"), []byte(`{"tokens":{"access_token":"new-token","account_id":"new-account"}}`), 0600); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	u := &usage.Usage{QuotaAccount: "account-123", ResetCredits: &usage.ResetCredits{Credits: []usage.ResetCredit{
		{Status: "available", ExpiresAt: time.Now().Add(30 * time.Minute)},
	}}}
	_, err := NewCodex(config.ProviderConfig{}).AutoRedeemResetCredit(context.Background(), u)
	if err == nil || requests != 1 {
		t.Fatal(err, requests)
	}
}
