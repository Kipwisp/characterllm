package safehttp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetUserAgent verifies that requests carry the default User-Agent and
// that a configured override wins. The built-in Validate policy is bypassed
// so the plain-httptest server can be dialed.
func TestGetUserAgent(t *testing.T) {
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	validate := func(ctx context.Context, raw string) (string, string, error) {
		return raw, "localhost", nil
	}

	f := &Fetcher{Validate: validate}
	resp, err := f.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	resp.Body.Close()
	if gotUA != defaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, defaultUserAgent)
	}

	f.UserAgent = "custom-agent/1.0"
	gotUA = ""
	resp, err = f.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	resp.Body.Close()
	if gotUA != "custom-agent/1.0" {
		t.Errorf("User-Agent = %q, want the configured override", gotUA)
	}
}

func TestValidate(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects non-https schemes", func(t *testing.T) {
		for _, raw := range []string{
			"file:///etc/passwd",
			"ftp://example.com/img.png",
			"http://example.com/img.png",
		} {
			if _, _, err := Validate(ctx, raw); err == nil {
				t.Errorf("expected rejection for %s", raw)
			}
		}
	})

	t.Run("rejects unresolvable host", func(t *testing.T) {
		if _, _, err := Validate(ctx, "https://no-such-host.invalid/img.png"); err == nil {
			t.Error("expected error for unresolvable host")
		}
	})

	t.Run("rejects loopback and private ranges", func(t *testing.T) {
		for _, raw := range []string{
			"https://127.0.0.1/img.png",
			"https://2130706433/img.png",
			"https://10.0.0.5/img.png",
			"https://192.168.1.10/img.png",
			"https://169.254.169.254/latest/meta-data/",
			"https://100.64.0.1/img.png",
			"https://[::1]/img.png",
			"https://[fc00::1]/img.png",
			"https://0.0.0.0/img.png",
		} {
			if _, _, err := Validate(ctx, raw); err == nil {
				t.Errorf("expected rejection for %s", raw)
			}
		}
	})

	t.Run("pins the resolved address", func(t *testing.T) {
		// IP literals skip DNS; the pinned URL must dial the exact
		// validated address on the https default port.
		pinned, host, err := Validate(ctx, "https://93.184.216.34/img.png")
		if err != nil {
			t.Fatalf("expected public IP literal to be allowed, got %v", err)
		}
		if host != "93.184.216.34" {
			t.Errorf("expected original host, got %s", host)
		}
		if pinned != "https://93.184.216.34:443/img.png" {
			t.Errorf("expected pinned URL, got %s", pinned)
		}
	})
}

func TestIsDisallowedIP(t *testing.T) {
	disallowed := []string{
		"127.0.0.1", "127.8.8.8",
		"10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "169.254.0.1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0",
		"::1", "fe80::1", "fc00::1", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	}
	for _, s := range disallowed {
		if !isDisallowedIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be disallowed", s)
		}
	}

	allowed := []string{
		"93.184.216.34", "8.8.8.8", "172.32.0.1", "100.128.0.1",
		"2606:4700:4700::1111", "2001:4860:4860::8888",
	}
	for _, s := range allowed {
		if isDisallowedIP(net.ParseIP(s)) {
			t.Errorf("expected %s to be allowed", s)
		}
	}
}
