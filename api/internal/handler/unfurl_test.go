package handler

import (
	"net"
	"net/url"
	"testing"
)

// ─── isPrivateIP ─────────────────────────────────────────────────────────────

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		// Loopback
		{"127.0.0.1", true},
		{"127.255.255.255", true},
		{"::1", true},
		// Private ranges
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		// Link-local
		{"169.254.1.1", true},
		{"fe80::1", true},
		// Unspecified
		{"0.0.0.0", true},
		{"::", true},
		// Public
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"104.16.0.0", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("test setup: net.ParseIP(%q) returned nil", tt.ip)
		}
		if got := isPrivateIP(ip); got != tt.want {
			t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

// ─── isPrivateHost ───────────────────────────────────────────────────────────

func TestIsPrivateHost_KnownPrivateHostnames(t *testing.T) {
	private := []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"app.local",
		"service.internal",
	}
	for _, h := range private {
		if !isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = false, want true", h)
		}
	}
}

func TestIsPrivateHost_EmptyIsPrivate(t *testing.T) {
	if !isPrivateHost("") {
		t.Error("isPrivateHost(\"\") should return true (fail closed)")
	}
}

func TestIsPrivateHost_PrivateIPLiteral(t *testing.T) {
	for _, h := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1"} {
		if !isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = false, want true", h)
		}
	}
}

func TestIsPrivateHost_PublicIPLiteral_ReturnsFalse(t *testing.T) {
	// 8.8.8.8 is a public IP — should not be considered private
	if isPrivateHost("8.8.8.8") {
		t.Error("isPrivateHost(\"8.8.8.8\") = true, want false")
	}
}

func TestIsPrivateHost_UnresolvableHostname_ReturnsTrue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS test in short mode")
	}
	// An unresolvable hostname should fail closed (return true = private)
	if !isPrivateHost("nonexistent.thisdoesnotexist.invalid") {
		t.Error("unresolvable hostname should return true (fail closed)")
	}
}

// ─── coalesce ────────────────────────────────────────────────────────────────

func TestCoalesce_ReturnsFirstNonNilNonEmpty(t *testing.T) {
	a := "first"
	b := "second"
	result := coalesce(&a, &b)
	if result == nil || *result != "first" {
		t.Errorf("coalesce returned %v, want 'first'", result)
	}
}

func TestCoalesce_SkipsEmptyString(t *testing.T) {
	empty := ""
	b := "fallback"
	result := coalesce(&empty, &b)
	if result == nil || *result != "fallback" {
		t.Errorf("coalesce skipped empty string: got %v, want 'fallback'", result)
	}
}

func TestCoalesce_ReturnsNilWhenAllNil(t *testing.T) {
	if coalesce(nil, nil, nil) != nil {
		t.Error("coalesce should return nil when all inputs are nil")
	}
}

func TestCoalesce_ReturnsNilWhenAllEmpty(t *testing.T) {
	e1, e2 := "", ""
	if coalesce(&e1, &e2) != nil {
		t.Error("coalesce should return nil when all strings are empty")
	}
}

// ─── fallbackMeta ────────────────────────────────────────────────────────────

func TestFallbackMeta_URLPreserved(t *testing.T) {
	rawURL := "https://example.com/page"
	meta := fallbackMeta(rawURL)
	if meta.URL != rawURL {
		t.Errorf("URL = %q, want %q", meta.URL, rawURL)
	}
}

func TestFallbackMeta_SiteNameFromHostname(t *testing.T) {
	meta := fallbackMeta("https://example.com/some/path?q=1")
	if meta.SiteName == nil || *meta.SiteName != "example.com" {
		t.Errorf("SiteName = %v, want 'example.com'", meta.SiteName)
	}
}

func TestFallbackMeta_OtherFieldsNil(t *testing.T) {
	meta := fallbackMeta("https://example.com")
	if meta.Title != nil || meta.Description != nil || meta.Image != nil || meta.Favicon != nil {
		t.Error("Title/Description/Image/Favicon should be nil in fallback meta")
	}
}

// ─── parseOpenGraph ──────────────────────────────────────────────────────────

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func TestParseOpenGraph_OGTitleAndDescription(t *testing.T) {
	body := `<html><head>
		<meta property="og:title" content="Test Title">
		<meta property="og:description" content="Test Desc">
	</head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://example.com"))
	if meta.Title == nil || *meta.Title != "Test Title" {
		t.Errorf("Title = %v, want 'Test Title'", meta.Title)
	}
	if meta.Description == nil || *meta.Description != "Test Desc" {
		t.Errorf("Description = %v, want 'Test Desc'", meta.Description)
	}
}

func TestParseOpenGraph_FallsBackToHTMLTitle(t *testing.T) {
	body := `<html><head><title>HTML Title</title></head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://example.com"))
	if meta.Title == nil || *meta.Title != "HTML Title" {
		t.Errorf("Title = %v, want 'HTML Title'", meta.Title)
	}
}

func TestParseOpenGraph_OGTitleTakesPrecedenceOverHTMLTitle(t *testing.T) {
	body := `<html><head>
		<title>HTML Title</title>
		<meta property="og:title" content="OG Title">
	</head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://example.com"))
	if meta.Title == nil || *meta.Title != "OG Title" {
		t.Errorf("Title = %v, want 'OG Title'", meta.Title)
	}
}

func TestParseOpenGraph_FaviconFromLink(t *testing.T) {
	body := `<html><head>
		<link rel="icon" href="/favicon.png">
	</head></html>`
	u := mustParseURL("https://example.com/page")
	meta := parseOpenGraph(body, u)
	if meta.Favicon == nil || *meta.Favicon != "https://example.com/favicon.png" {
		t.Errorf("Favicon = %v, want 'https://example.com/favicon.png'", meta.Favicon)
	}
}

func TestParseOpenGraph_DefaultFaviconFallback(t *testing.T) {
	body := `<html><head><title>Page</title></head></html>`
	u := mustParseURL("https://example.com/page")
	meta := parseOpenGraph(body, u)
	if meta.Favicon == nil || *meta.Favicon != "https://example.com/favicon.ico" {
		t.Errorf("Favicon = %v, want default /favicon.ico", meta.Favicon)
	}
}

func TestParseOpenGraph_ImageURLResolved(t *testing.T) {
	body := `<html><head>
		<meta property="og:image" content="/img/hero.jpg">
	</head></html>`
	u := mustParseURL("https://example.com/page")
	meta := parseOpenGraph(body, u)
	if meta.Image == nil || *meta.Image != "https://example.com/img/hero.jpg" {
		t.Errorf("Image = %v, want resolved absolute URL", meta.Image)
	}
}

func TestParseOpenGraph_SiteNameFromOG(t *testing.T) {
	body := `<html><head>
		<meta property="og:site_name" content="Acme Inc">
	</head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://acme.com"))
	if meta.SiteName == nil || *meta.SiteName != "Acme Inc" {
		t.Errorf("SiteName = %v, want 'Acme Inc'", meta.SiteName)
	}
}

func TestParseOpenGraph_SiteNameFallsBackToHostname(t *testing.T) {
	body := `<html><head></head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://example.com/path"))
	if meta.SiteName == nil || *meta.SiteName != "example.com" {
		t.Errorf("SiteName = %v, want 'example.com'", meta.SiteName)
	}
}

func TestParseOpenGraph_URLPreserved(t *testing.T) {
	body := `<html><head></head></html>`
	rawURL := "https://example.com/article?id=1"
	u := mustParseURL(rawURL)
	meta := parseOpenGraph(body, u)
	if meta.URL != rawURL {
		t.Errorf("URL = %q, want %q", meta.URL, rawURL)
	}
}

func TestParseOpenGraph_TwitterFallback(t *testing.T) {
	body := `<html><head>
		<meta name="twitter:title" content="Tweet Title">
		<meta name="twitter:description" content="Tweet Desc">
	</head></html>`
	meta := parseOpenGraph(body, mustParseURL("https://example.com"))
	if meta.Title == nil || *meta.Title != "Tweet Title" {
		t.Errorf("Title = %v, want 'Tweet Title'", meta.Title)
	}
	if meta.Description == nil || *meta.Description != "Tweet Desc" {
		t.Errorf("Description = %v, want 'Tweet Desc'", meta.Description)
	}
}
