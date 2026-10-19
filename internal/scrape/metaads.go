package scrape

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ad is one ad from the Meta Ad Library.
type Ad struct {
	ID              string   `json:"id"`
	URL             string   `json:"url"`
	PageID          string   `json:"page_id"`
	PageName        string   `json:"page_name"`
	PageURL         string   `json:"page_url,omitempty"`
	PageLikes       int64    `json:"page_likes,omitempty"`
	PagePicture     string   `json:"page_picture,omitempty"`
	Active          bool     `json:"active"`
	StartDate       string   `json:"start_date,omitempty"`
	EndDate         string   `json:"end_date,omitempty"`
	Platforms       []string `json:"platforms,omitempty"`
	Format          string   `json:"format,omitempty"`
	Body            string   `json:"body,omitempty"`
	Title           string   `json:"title,omitempty"`
	LinkURL         string   `json:"link_url,omitempty"`
	LinkDescription string   `json:"link_description,omitempty"`
	CTA             string   `json:"cta,omitempty"`
	CTAType         string   `json:"cta_type,omitempty"`
	ExtraLinks      []string `json:"extra_links,omitempty"`
	Images          []string `json:"images,omitempty"`
	Videos          []string `json:"videos,omitempty"`
	Categories      []string `json:"categories,omitempty"`
	PageCategories  []string `json:"page_categories,omitempty"`
	Countries       []string `json:"countries,omitempty"`
	Variations      int      `json:"variations,omitempty"`
}

// MetaAdsResult is the output of the meta-ads mode.
type MetaAdsResult struct {
	SourceURL  string `json:"source_url"`
	TotalCount int    `json:"total_count"`
	Returned   int    `json:"returned"`
	HasNext    bool   `json:"has_next"`
	Cursor     string `json:"cursor,omitempty"`
	Ads        []Ad   `json:"ads"`
}

var challengeRe = regexp.MustCompile(`/__rd_verify_[^'"?]*\?challenge=\d+`)

// metaSession keeps cookies across requests so Facebook's JS challenge is
// only solved once per run.
type metaSession struct {
	client *http.Client
	ua     string
	reqs   int      // requests made so far
	ctx    *pageCtx // context of the most recently loaded page, needed for pagination
}

func newMetaSession(o Options) (*metaSession, error) {
	jar, _ := cookiejar.New(nil)
	client, err := newClient(o, jar)
	if err != nil {
		return nil, err
	}
	ua := o.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	return &metaSession{client: client, ua: ua}, nil
}

func (m *metaSession) get(rawURL string) ([]byte, int, error) {
	m.reqs++
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	req.Header.Set("User-Agent", m.ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	return b, resp.StatusCode, err
}

// page loads an Ad Library URL (solving the JS challenge if asked) and
// returns the decoded ad connection.
func (m *metaSession) page(rawURL string) (*rawConnection, error) { return m.pageRetry(rawURL, 0) }

func (m *metaSession) pageRetry(rawURL string, retries int) (*rawConnection, error) {
	var body []byte
	var status int
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		body, status, err = m.get(rawURL)
		if err != nil {
			return nil, err
		}
		if p := challengeRe.Find(body); p != nil {
			req, _ := http.NewRequest(http.MethodPost, "https://www.facebook.com"+string(p), nil)
			req.Header.Set("User-Agent", m.ua)
			if resp, err := m.client.Do(req); err == nil {
				resp.Body.Close()
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		break
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Ad Library returned HTTP %d (blocked or rate limited; try another IP with `aintscrape rotate`)", status)
	}
	m.ctx = parsePageCtx(body)
	conn, err := findSearchConnection(body)
	if err == nil && conn.Count > 0 && countAds(conn) == 0 && retries < 2 {
		// Facebook sometimes answers with the result count but no ads when it
		// is throttling; wait a moment and ask again.
		time.Sleep(time.Duration(3+retries*4) * time.Second)
		return m.pageRetry(rawURL, retries+1)
	}
	return conn, err
}

func countAds(c *rawConnection) int {
	n := 0
	for _, e := range c.Edges {
		n += len(e.Node.CollatedResults)
	}
	return n
}

// ScrapeMetaAds loads an Ad Library page, passes Facebook's JS challenge
// without a browser, and extracts the ads. With o.Limit > 0 it keeps
// paginating until that many ads are collected (or results run out).
func ScrapeMetaAds(rawURL string, o Options) (*MetaAdsResult, error) {
	sess, err := newMetaSession(o)
	if err != nil {
		return nil, err
	}
	conn, err := sess.page(rawURL)
	if err != nil {
		return nil, err
	}
	res := conn.result(rawURL)
	if res.TotalCount > 0 && len(res.Ads) == 0 {
		return nil, fmt.Errorf("Facebook reported %d matches but returned no ads (throttled); wait a few minutes or rotate your IP and try again", res.TotalCount)
	}
	if o.Limit > 0 {
		emit := func(r *MetaAdsResult) {
			if o.OnEvent != nil {
				o.OnEvent(Event{Stage: "search", Done: min(len(r.Ads), o.Limit), Total: o.Limit, Msg: "Collecting ads", Requests: sess.reqs})
			}
		}
		emit(res)
		sess.paginate(res, o, func(r *MetaAdsResult) bool { return len(r.Ads) >= o.Limit }, emit)
		if len(res.Ads) > o.Limit {
			res.Ads = res.Ads[:o.Limit]
		}
		res.Returned = len(res.Ads)
	}
	return res, nil
}

func (c *rawConnection) result(src string) *MetaAdsResult {
	res := &MetaAdsResult{SourceURL: src, TotalCount: c.Count, HasNext: c.PageInfo.HasNextPage, Cursor: c.PageInfo.EndCursor}
	for _, e := range c.Edges {
		for _, cr := range e.Node.CollatedResults {
			res.Ads = append(res.Ads, cr.toAd())
		}
	}
	res.Returned = len(res.Ads)
	return res
}

type rawConnection struct {
	Count    int `json:"count"`
	PageInfo struct {
		EndCursor   string `json:"end_cursor"`
		HasNextPage bool   `json:"has_next_page"`
	} `json:"page_info"`
	Edges []struct {
		Node struct {
			CollatedResults []rawAd `json:"collated_results"`
		} `json:"node"`
	} `json:"edges"`
}

type rawCard struct {
	Body            string `json:"body"`
	Title           string `json:"title"`
	LinkURL         string `json:"link_url"`
	LinkDescription string `json:"link_description"`
	CTAText         string `json:"cta_text"`
	CTAType         string `json:"cta_type"`
	OriginalImage   string `json:"original_image_url"`
	ResizedImage    string `json:"resized_image_url"`
	VideoHD         string `json:"video_hd_url"`
	VideoSD         string `json:"video_sd_url"`
}

type rawAd struct {
	ID             string   `json:"ad_archive_id"`
	CollationCount *int     `json:"collation_count"`
	IsActive       bool     `json:"is_active"`
	PageID         string   `json:"page_id"`
	PageName       string   `json:"page_name"`
	StartDate      int64    `json:"start_date"`
	EndDate        int64    `json:"end_date"`
	Platforms      []string `json:"publisher_platform"`
	Categories     []string `json:"categories"`
	Countries      []string `json:"targeted_or_reached_countries"`
	Snapshot       struct {
		PageCategories  []string `json:"page_categories"`
		PageURI         string   `json:"page_profile_uri"`
		PagePicture     string   `json:"page_profile_picture_url"`
		PageLikes       int64    `json:"page_like_count"`
		Format          string   `json:"display_format"`
		Title           string   `json:"title"`
		LinkURL         string   `json:"link_url"`
		LinkDescription string   `json:"link_description"`
		CTAText         string   `json:"cta_text"`
		CTAType         string   `json:"cta_type"`
		ExtraLinks      []string `json:"extra_links"`
		Body            *struct {
			Text string `json:"text"`
		} `json:"body"`
		Cards  []rawCard `json:"cards"`
		Images []struct {
			Original string `json:"original_image_url"`
			Resized  string `json:"resized_image_url"`
		} `json:"images"`
		Videos []struct {
			HD string `json:"video_hd_url"`
			SD string `json:"video_sd_url"`
		} `json:"videos"`
	} `json:"snapshot"`
}

func date(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format("2006-01-02")
}

func (r rawAd) toAd() Ad {
	s := r.Snapshot
	a := Ad{
		ID: r.ID, URL: "https://www.facebook.com/ads/library/?id=" + r.ID,
		PageID: r.PageID, PageName: r.PageName, PageURL: s.PageURI, PageLikes: s.PageLikes, PagePicture: s.PagePicture,
		Active: r.IsActive, StartDate: date(r.StartDate), EndDate: date(r.EndDate),
		Platforms: r.Platforms, Format: s.Format, Title: s.Title, LinkURL: s.LinkURL,
		LinkDescription: s.LinkDescription, CTA: s.CTAText, CTAType: s.CTAType, ExtraLinks: s.ExtraLinks, Categories: r.Categories,
		PageCategories: s.PageCategories, Countries: r.Countries,
	}
	if r.CollationCount != nil {
		a.Variations = *r.CollationCount
	}
	if s.Body != nil {
		a.Body = s.Body.Text
	}
	add := func(dst *[]string, vals ...string) {
		for _, v := range vals {
			if v != "" {
				*dst = append(*dst, v)
				return
			}
		}
	}
	for _, im := range s.Images {
		add(&a.Images, im.Original, im.Resized)
	}
	for _, v := range s.Videos {
		add(&a.Videos, v.HD, v.SD)
	}
	for _, c := range s.Cards {
		add(&a.Images, c.OriginalImage, c.ResizedImage)
		add(&a.Videos, c.VideoHD, c.VideoSD)
		if a.Body == "" {
			a.Body = c.Body
		}
		if a.Title == "" {
			a.Title = c.Title
		}
		if a.LinkURL == "" {
			a.LinkURL = c.LinkURL
		}
		if a.LinkDescription == "" {
			a.LinkDescription = c.LinkDescription
		}
		if a.CTA == "" {
			a.CTA = c.CTAText
		}
		if a.CTAType == "" {
			a.CTAType = c.CTAType
		}
		if c.LinkURL != "" && c.LinkURL != a.LinkURL {
			a.ExtraLinks = append(a.ExtraLinks, c.LinkURL)
		}
	}
	return a
}

// findSearchConnection locates the <script> JSON blob that holds
// search_results_connection and decodes it.
func findSearchConnection(page []byte) (*rawConnection, error) {
	s := string(page)
	idx := strings.Index(s, `"search_results_connection"`)
	if idx < 0 {
		return nil, fmt.Errorf("no ads found in page (layout changed, or the query has no results)")
	}
	start := strings.LastIndex(s[:idx], "<script")
	end := strings.Index(s[idx:], "</script>")
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("could not isolate ad data in page")
	}
	tag := s[start : idx+end]
	tag = tag[strings.Index(tag, ">")+1:]

	var root any
	if err := json.Unmarshal([]byte(tag), &root); err != nil {
		return nil, fmt.Errorf("decode ad data: %w", err)
	}
	node := findKey(root, "search_results_connection")
	if node == nil {
		return nil, fmt.Errorf("search_results_connection missing in decoded data")
	}
	b, _ := json.Marshal(node)
	var conn rawConnection
	if err := json.Unmarshal(b, &conn); err != nil {
		return nil, fmt.Errorf("parse ads: %w", err)
	}
	return &conn, nil
}

func findKey(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		if x, ok := t[key]; ok && x != nil {
			return x
		}
		for _, c := range t {
			if r := findKey(c, key); r != nil {
				return r
			}
		}
	case []any:
		for _, c := range t {
			if r := findKey(c, key); r != nil {
				return r
			}
		}
	}
	return nil
}

// ---- pagination -------------------------------------------------------

// defaultPaginationDocID is Facebook's persisted-query id for
// AdLibrarySearchPaginationQuery; discoverPaginationDocID refreshes it from
// the site's JS bundles if it stops working.
const defaultPaginationDocID = "24922295957467452"

var (
	paginationDocID = defaultPaginationDocID
	lsdRe           = regexp.MustCompile(`"LSD",\[\],\{"token":"([^"]+)"`)
	spinRe          = regexp.MustCompile(`"__spin_r":(\d+)`)
	spinTRe         = regexp.MustCompile(`"__spin_t":(\d+)`)
	hasteRe         = regexp.MustCompile(`"haste_session":"([^"]+)"`)
	hsiRe           = regexp.MustCompile(`"hsi":"(\d+)"`)
	bundleRe        = regexp.MustCompile(`https:\\?/\\?/static\.xx\.fbcdn\.net\\?/rsrc\.php[^"\s\\]*(?:\\/[^"\s\\]*)*\.js`)
	docIDRe         = regexp.MustCompile(`AdLibrarySearchPaginationQuery_facebookRelayOperation",\[\],\(function\([^)]*\)\{[^}]*?exports="(\d+)"`)
)

// pageCtx holds what is needed to ask Facebook for the next page of results.
type pageCtx struct {
	lsd, rev, spinT, hs, hsi string
	vars                     map[string]any
	bundles                  []string
}

var paginationVars = []string{"activeStatus", "adType", "bylines", "collationToken", "contentLanguages", "countries",
	"isTargetedCountry", "location", "mediaType", "multiCountryFilterMode", "pageIDs", "potentialReachInput",
	"publisherPlatforms", "queryString", "regions", "searchType", "sessionID", "sortData", "source", "startDate", "v", "viewAllPageID"}

func parsePageCtx(page []byte) *pageCtx {
	h := string(page)
	c := &pageCtx{}
	if m := lsdRe.FindStringSubmatch(h); m != nil {
		c.lsd = m[1]
	}
	if m := spinRe.FindStringSubmatch(h); m != nil {
		c.rev = m[1]
	}
	if m := spinTRe.FindStringSubmatch(h); m != nil {
		c.spinT = m[1]
	}
	if m := hasteRe.FindStringSubmatch(h); m != nil {
		c.hs = m[1]
	}
	if m := hsiRe.FindStringSubmatch(h); m != nil {
		c.hsi = m[1]
	}
	if i := strings.Index(h, `"preloaders":[`); i >= 0 {
		var arr []struct {
			Variables map[string]any `json:"variables"`
		}
		dec := json.NewDecoder(strings.NewReader(h[i+len(`"preloaders":`):]))
		if dec.Decode(&arr) == nil && len(arr) > 0 {
			c.vars = map[string]any{}
			for _, k := range paginationVars {
				c.vars[k] = arr[0].Variables[k]
			}
		}
	}
	for _, u := range bundleRe.FindAllString(strings.ReplaceAll(h, `\/`, "/"), -1) {
		c.bundles = append(c.bundles, u)
	}
	return c
}

// next fetches the batch of results after cursor.
func (m *metaSession) next(cursor string, first int) (*rawConnection, error) {
	m.reqs++
	c := m.ctx
	if c == nil || c.lsd == "" || c.vars == nil {
		return nil, fmt.Errorf("page did not include pagination data")
	}
	vars := map[string]any{}
	for k, v := range c.vars {
		vars[k] = v
	}
	vars["cursor"], vars["first"], vars["excludedIDs"] = cursor, first, []string{}
	vb, _ := json.Marshal(vars)

	do := func(doc string) ([]byte, error) {
		f := url.Values{}
		for k, v := range map[string]string{
			"av": "0", "__aaid": "0", "__user": "0", "__a": "1", "__req": "1", "dpr": "1", "__ccg": "EXCELLENT",
			"__comet_req": "15", "__s": "::", "__hs": c.hs, "__hsi": c.hsi, "__rev": c.rev, "__spin_r": c.rev,
			"__spin_b": "trunk", "__spin_t": c.spinT, "lsd": c.lsd, "jazoest": jazoest(c.lsd),
			"fb_api_caller_class": "RelayModern", "fb_api_req_friendly_name": "AdLibrarySearchPaginationQuery",
			"server_timestamps": "true", "variables": string(vb), "doc_id": doc,
		} {
			f.Set(k, v)
		}
		req, _ := http.NewRequest(http.MethodPost, "https://www.facebook.com/api/graphql/", strings.NewReader(f.Encode()))
		req.Header.Set("User-Agent", m.ua)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Origin", "https://www.facebook.com")
		req.Header.Set("Referer", "https://www.facebook.com/ads/library/")
		req.Header.Set("X-FB-LSD", c.lsd)
		req.Header.Set("X-FB-Friendly-Name", "AdLibrarySearchPaginationQuery")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, err := m.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("pagination HTTP %d", resp.StatusCode)
		}
		return b, nil
	}

	parse := func(b []byte) (*rawConnection, error) {
		b = []byte(strings.TrimPrefix(strings.TrimSpace(string(b)), "for (;;);"))
		var out struct {
			Data struct {
				Main struct {
					Conn *rawConnection `json:"search_results_connection"`
				} `json:"ad_library_main"`
			} `json:"data"`
			Error int `json:"error"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			// Several JSON documents can be concatenated; take the first.
			if err2 := json.NewDecoder(strings.NewReader(string(b))).Decode(&out); err2 != nil {
				return nil, fmt.Errorf("decode pagination response: %w", err)
			}
		}
		if out.Data.Main.Conn == nil {
			return nil, fmt.Errorf("pagination returned no results (error %d)", out.Error)
		}
		return out.Data.Main.Conn, nil
	}

	b, err := do(paginationDocID)
	if err == nil {
		if conn, perr := parse(b); perr == nil {
			return conn, nil
		} else {
			err = perr
		}
	}
	// The persisted-query id may have rotated: look it up in the page's bundles.
	if id := m.discoverDocID(); id != "" && id != paginationDocID {
		paginationDocID = id
		if b, err2 := do(id); err2 == nil {
			return parse(b)
		}
	}
	return nil, err
}

func jazoest(lsd string) string {
	n := 0
	for _, r := range lsd {
		n += int(r)
	}
	return "2" + strconv.Itoa(n)
}

// discoverDocID scans the page's JS bundles for the pagination query id.
func (m *metaSession) discoverDocID() string {
	if m.ctx == nil {
		return ""
	}
	seen := map[string]bool{}
	for _, u := range m.ctx.bundles {
		if seen[u] {
			continue
		}
		seen[u] = true
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set("User-Agent", m.ua)
		resp, err := m.client.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if i := strings.Index(string(b), "AdLibrarySearchPaginationQuery_facebookRelayOperation"); i >= 0 {
			seg := string(b[i:min(i+300, len(b))])
			if q := regexp.MustCompile(`exports="(\d+)"`).FindStringSubmatch(seg); q != nil {
				return q[1]
			}
		}
	}
	return ""
}

// paginate appends further batches to res until done(res) is true, the
// results run out, or a request fails (res keeps what was collected so far).
func (m *metaSession) paginate(res *MetaAdsResult, o Options, done func(*MetaAdsResult) bool, report func(*MetaAdsResult)) {
	delay := o.Delay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	seen := map[string]bool{}
	for _, a := range res.Ads {
		seen[a.ID] = true
	}
	empty := 0
	for req := 0; req < 300 && !done(res) && res.HasNext && res.Cursor != ""; req++ {
		time.Sleep(delay)
		conn, err := m.next(res.Cursor, 30)
		if err == nil && countAds(conn) == 0 {
			if empty++; empty >= 3 {
				err = fmt.Errorf("Facebook returned 3 empty batches in a row (rate limited?)")
			}
		} else if err == nil {
			empty = 0
		}
		if err != nil {
			if o.Progress != nil {
				o.Progress("Pagination stopped: " + err.Error())
			}
			return
		}
		next := conn.result("")
		for _, a := range next.Ads {
			if !seen[a.ID] {
				seen[a.ID] = true
				res.Ads = append(res.Ads, a)
			}
		}
		res.HasNext, res.Cursor, res.Returned = next.HasNext, next.Cursor, len(res.Ads)
		if o.Progress != nil {
			o.Progress(fmt.Sprintf("Collected %d ads...", len(res.Ads)))
		}
		if report != nil {
			report(res)
		}
	}
}
