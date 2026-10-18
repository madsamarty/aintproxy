package scrape

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const page = `<html><body><h1> Hello   <b>World</b></h1>
<a class="l" href="/a">A</a><a class="l" href="/b">B</a><script>x()</script></body></html>`

func TestSelect(t *testing.T) {
	got, err := Select([]byte(page), Extract{Selector: "h1"})
	if err != nil || !reflect.DeepEqual(got, []string{"Hello World"}) {
		t.Fatalf("text: %v %v", got, err)
	}
	got, _ = Select([]byte(page), Extract{Selector: "a.l", Attr: "href"})
	if !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Fatalf("attr: %v", got)
	}
	got, _ = Select([]byte(page), Extract{Selector: "h1", HTML: true})
	if len(got) != 1 || got[0] != " Hello   <b>World</b>" {
		t.Fatalf("html: %q", got)
	}
	if _, err := Select([]byte(page), Extract{Selector: "a["}); err == nil {
		t.Fatal("expected selector error")
	}
}

func TestFetchDirectAndProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(page))
	}))
	defer origin.Close()

	res, err := Fetch(origin.URL, Options{})
	if err != nil || res.Status != 200 {
		t.Fatalf("direct: %v %v", res, err)
	}

	hit := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Write([]byte("via proxy"))
	}))
	defer proxy.Close()
	res, err = Fetch("http://example.invalid/", Options{ProxyURL: proxy.URL})
	if err != nil || !hit || string(res.Body) != "via proxy" {
		t.Fatalf("proxy: %v %v hit=%v", res, err, hit)
	}
}

func TestFetchRejectsBadURL(t *testing.T) {
	if _, err := Fetch("ftp://x", Options{}); err == nil {
		t.Fatal("expected error")
	}
}
