package scrape

import "testing"

const metaFixture = `<html><script type="application/json">{"x":{"search_results_connection":{"count":2,"page_info":{"end_cursor":"c1","has_next_page":true},"edges":[{"node":{"collated_results":[{"ad_archive_id":"111","is_active":true,"page_id":"9","page_name":"Shop","start_date":1783666800,"publisher_platform":["FACEBOOK"],"collation_count":3,"snapshot":{"display_format":"IMAGE","page_profile_uri":"https://facebook.com/shop","body":{"text":"Hello"},"cta_text":"Buy","images":[{"original_image_url":"https://img/1.jpg"}],"cards":[]}}]}}]}}}</script></html>`

func TestFindSearchConnection(t *testing.T) {
	c, err := findSearchConnection([]byte(metaFixture))
	if err != nil {
		t.Fatal(err)
	}
	if c.Count != 2 || !c.PageInfo.HasNextPage || c.PageInfo.EndCursor != "c1" {
		t.Fatalf("bad connection: %+v", c)
	}
	a := c.Edges[0].Node.CollatedResults[0].toAd()
	if a.ID != "111" || a.PageName != "Shop" || a.Body != "Hello" || a.StartDate != "2026-07-10" ||
		a.Variations != 3 || len(a.Images) != 1 || a.URL != "https://www.facebook.com/ads/library/?id=111" {
		t.Fatalf("bad ad: %+v", a)
	}
}

func TestDetectMode(t *testing.T) {
	if m, ok := DetectMode("https://www.facebook.com/ads/library/?q=x"); !ok || m.Name != "meta-pages" {
		t.Fatal("meta-pages not detected")
	}
	if _, ok := DetectMode("https://example.com/"); ok {
		t.Fatal("unexpected mode for example.com")
	}
}
