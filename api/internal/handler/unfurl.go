package handler

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/model"
	"golang.org/x/net/html"
)

// netLookupIPAddr is injectable for testing.
var netLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// netLookupIP is injectable for testing.
var netLookupIP = net.LookupIP

// httpNewRequest is injectable for testing.
var httpNewRequest = http.NewRequestWithContext

// safeDialContext wraps the default dialer to reject connections to private/reserved IPs.
// This prevents SSRF attacks via DNS rebinding or redirect-to-private.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := netLookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, &net.AddrError{Err: "connection to private address denied", Addr: addr}
		}
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
}

var unfurlCheckRedirect = func(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return http.ErrUseLastResponse
	}
	// The safeDialContext hook validates resolved IPs on every redirect target,
	// so no additional hostname check is needed here.
	return nil
}

var unfurlClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DialContext: safeDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return unfurlCheckRedirect(req, via)
	},
}

const maxBodyBytes = 512_000 // 500 KB

// isPrivateHostCheck is a package-level var so tests can override it.
var isPrivateHostCheck = isPrivateHost

type unfurlRequest struct {
	URL string `json:"url" binding:"required"`
}

// POST /unfurl
func UnfurlURL(c *gin.Context) {
	var body unfurlRequest
	if !bindJSON(c, &body) {
		return
	}

	parsed, err := url.Parse(body.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only http/https URLs are supported"})
		return
	}

	if isPrivateHostCheck(parsed.Hostname()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL points to a private/reserved address"})
		return
	}

	req, err := httpNewRequest(c.Request.Context(), "GET", parsed.String(), nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL"})
		return
	}
	req.Header.Set("User-Agent", "Glyph-LinkUnfurl/1.0")
	req.Header.Set("Accept", "text/html, application/xhtml+xml")

	resp, err := unfurlClient.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, fallbackMeta(body.URL))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		c.JSON(http.StatusOK, fallbackMeta(body.URL))
		return
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "xhtml") {
		c.JSON(http.StatusOK, fallbackMeta(body.URL))
		return
	}

	limited := io.LimitReader(resp.Body, maxBodyBytes)
	htmlBytes, err := io.ReadAll(limited)
	if err != nil {
		c.JSON(http.StatusOK, fallbackMeta(body.URL))
		return
	}
	html := string(htmlBytes)

	meta := parseOpenGraph(html, parsed)
	c.JSON(http.StatusOK, meta)
}

func fallbackMeta(rawURL string) model.LinkMeta {
	var siteName *string
	if u, err := url.Parse(rawURL); err == nil {
		h := u.Hostname()
		siteName = &h
	}
	return model.LinkMeta{
		URL:      rawURL,
		SiteName: siteName,
	}
}

func parseOpenGraph(body string, u *url.URL) model.LinkMeta {
	metas := make(map[string]string) // property/name → content
	var htmlTitle *string
	var favicon *string

	tokenizer := html.NewTokenizer(strings.NewReader(body))
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)

			if tagName == "meta" && hasAttr {
				var propOrName, content string
				for {
					key, val, more := tokenizer.TagAttr()
					k := strings.ToLower(string(key))
					v := string(val)
					switch k {
					case "property", "name":
						propOrName = strings.ToLower(v)
					case "content":
						content = v
					}
					if !more {
						break
					}
				}
				if propOrName != "" && content != "" {
					metas[propOrName] = content
				}
			}

			if tagName == "title" {
				if tokenizer.Next() == html.TextToken {
					t := strings.TrimSpace(string(tokenizer.Text()))
					if t != "" {
						htmlTitle = &t
					}
				}
			}

			if tagName == "link" && hasAttr {
				var rel, href string
				for {
					key, val, more := tokenizer.TagAttr()
					k := strings.ToLower(string(key))
					v := string(val)
					switch k {
					case "rel":
						rel = strings.ToLower(v)
					case "href":
						href = v
					}
					if !more {
						break
					}
				}
				if href != "" && (rel == "icon" || rel == "shortcut icon" || rel == "apple-touch-icon") {
					if resolved, err := url.Parse(href); err == nil {
						f := u.ResolveReference(resolved).String()
						favicon = &f
					}
				}
			}
		}
	}

	if favicon == nil {
		f := u.Scheme + "://" + u.Host + "/favicon.ico"
		favicon = &f
	}

	getMetaPtr := func(key string) *string {
		if v, ok := metas[key]; ok && v != "" {
			return &v
		}
		return nil
	}

	title := coalesce(getMetaPtr("og:title"), getMetaPtr("twitter:title"), htmlTitle)
	desc := coalesce(getMetaPtr("og:description"), getMetaPtr("twitter:description"), getMetaPtr("description"))
	image := coalesce(getMetaPtr("og:image"), getMetaPtr("twitter:image"))
	if image != nil {
		if resolved, err := url.Parse(*image); err == nil {
			r := u.ResolveReference(resolved).String()
			image = &r
		}
	}

	hostname := u.Hostname()
	site := coalesce(getMetaPtr("og:site_name"), &hostname)

	return model.LinkMeta{
		URL:         u.String(),
		Title:       title,
		Description: desc,
		Image:       image,
		Favicon:     favicon,
		SiteName:    site,
	}
}

func coalesce(ptrs ...*string) *string {
	for _, p := range ptrs {
		if p != nil && *p != "" {
			return p
		}
	}
	return nil
}

func isPrivateHost(hostname string) bool {
	if hostname == "" {
		return true
	}
	lower := strings.ToLower(hostname)
	if lower == "localhost" || lower == "127.0.0.1" || lower == "::1" || lower == "0.0.0.0" {
		return true
	}
	if strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return true
	}

	// If it's an IP literal, check directly
	ip := net.ParseIP(hostname)
	if ip != nil {
		return isPrivateIP(ip)
	}

	// Resolve hostname and check all resulting IPs
	ips, err := netLookupIP(hostname)
	if err != nil {
		// If we can't resolve it, deny — fail closed
		return true
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}
	return false
}

// isPrivateIP returns true if the IP belongs to a private, loopback, or reserved range.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
