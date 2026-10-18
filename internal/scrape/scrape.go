// Package scrape fetches web pages, optionally through the local proxy,
// and extracts data from them with CSS selectors.
package scrape

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

const (
	DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	maxBodyBytes     = 32 << 20
)

// Options controls a fetch.
type Options struct {
	ProxyURL  string        // e.g. http://127.0.0.1:3128; empty means direct
	UserAgent string        // defaults to DefaultUserAgent
	Timeout   time.Duration // defaults to 30s
	Retries   int           // extra attempts on network error, 429 or 5xx
	Headers   map[string]string
}

// Result is the outcome of a fetch.
type Result struct {
	URL    string
	Status int
	Body   []byte
}

// Fetch GETs rawURL and returns the response body.
func Fetch(rawURL string, o Options) (*Result, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q (must be http or https)", rawURL)
	}

	transport := &http.Transport{}
	if o.ProxyURL != "" {
		p, err := url.Parse(o.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(p)
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	ua := o.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}

	var lastErr error
	for attempt := 0; attempt <= o.Retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
		for k, v := range o.Headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		res := &Result{URL: resp.Request.URL.String(), Status: resp.StatusCode, Body: body}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if attempt == o.Retries {
				return res, nil
			}
			continue
		}
		return res, nil
	}
	return nil, lastErr
}

// Extract describes what to pull out of each matched element.
type Extract struct {
	Selector string
	Attr     string // if set, return this attribute instead of text
	HTML     bool   // if true, return inner HTML instead of text
}

// Select parses body and returns one string per element matching the selector.
func Select(body []byte, e Extract) ([]string, error) {
	sel, err := cascadia.Compile(e.Selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector %q: %w", e.Selector, err)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	var out []string
	for _, n := range cascadia.QueryAll(doc, sel) {
		switch {
		case e.Attr != "":
			for _, a := range n.Attr {
				if a.Key == e.Attr {
					out = append(out, a.Val)
					break
				}
			}
		case e.HTML:
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				html.Render(&sb, c)
			}
			out = append(out, sb.String())
		default:
			out = append(out, strings.Join(strings.Fields(text(n)), " "))
		}
	}
	return out, nil
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
		return ""
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(text(c))
		sb.WriteByte(' ')
	}
	return sb.String()
}
