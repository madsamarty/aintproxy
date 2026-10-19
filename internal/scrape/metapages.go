package scrape

import (
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var metaPagesMode = Mode{
	Name:        "meta-pages",
	Description: "Meta Ad Library search -> unique advertiser pages -> profile, ad stats and contacts (WhatsApp, phone, email, socials, website) per page (CSV/Excel export)",
	Match: func(u *url.URL) bool {
		h := strings.TrimPrefix(u.Host, "www.")
		return (h == "facebook.com" || h == "web.facebook.com") && strings.HasPrefix(u.Path, "/ads/library")
	},
	Run: func(rawURL string, o Options) (any, error) { return ScrapeMetaPages(rawURL, o) },
}

// PageProfile is everything the Ad Library exposes about one advertiser page.
type PageProfile struct {
	PageID         string   `json:"page_id"`
	PageName       string   `json:"page_name"`
	PageURL        string   `json:"page_url,omitempty"`
	AdLibraryURL   string   `json:"ad_library_url"`
	ProfilePicture string   `json:"profile_picture,omitempty"`
	PageLikes      int64    `json:"page_likes,omitempty"`
	PageCategories []string `json:"page_categories,omitempty"`
	TotalAds       int      `json:"total_ads"`
	ActiveAds      int      `json:"active_ads"`
	AdsInSearch    int      `json:"ads_in_search"`
	AdsSampled     int      `json:"ads_sampled"`
	FirstAdStart   string   `json:"first_ad_start,omitempty"`
	LatestAdStart  string   `json:"latest_ad_start,omitempty"`
	Platforms      []string `json:"platforms,omitempty"`
	Formats        []string `json:"formats,omitempty"`
	TopDomains     []string `json:"top_link_domains,omitempty"`
	TopCTAs        []string `json:"top_ctas,omitempty"`
	Countries      []string `json:"countries,omitempty"`
	SampleAdURL    string   `json:"sample_ad_url,omitempty"`
	Error          string   `json:"error,omitempty"`

	// Contact details found in the page's ads and (optionally) its website.
	WhatsApp      []string  `json:"whatsapp,omitempty"`
	WhatsAppLinks []string  `json:"whatsapp_links,omitempty"`
	Phones        []string  `json:"phones,omitempty"`
	Emails        []string  `json:"emails,omitempty"`
	Websites      []string  `json:"websites,omitempty"`
	Instagram     []string  `json:"instagram,omitempty"`
	Facebook      []string  `json:"facebook,omitempty"`
	Messenger     []string  `json:"messenger,omitempty"`
	Telegram      []string  `json:"telegram,omitempty"`
	TikTok        []string  `json:"tiktok,omitempty"`
	YouTube       []string  `json:"youtube,omitempty"`
	Twitter       []string  `json:"twitter,omitempty"`
	LinkedIn      []string  `json:"linkedin,omitempty"`
	OtherSocial   []string  `json:"other_social,omitempty"`
	Contacts      []Contact `json:"contacts,omitempty"`
}

func (p *PageProfile) setContacts(list []Contact) {
	p.Contacts = list
	p.WhatsApp, p.WhatsAppLinks = summarize(list, "whatsapp"), summarize(list, "whatsapp_link")
	p.Phones, p.Emails, p.Websites = summarize(list, "phone"), summarize(list, "email"), summarize(list, "website")
	p.Instagram, p.Facebook, p.Messenger = summarize(list, "instagram"), summarize(list, "facebook"), summarize(list, "messenger")
	p.Telegram, p.TikTok, p.YouTube = summarize(list, "telegram"), summarize(list, "tiktok"), summarize(list, "youtube")
	p.Twitter, p.LinkedIn = summarize(list, "twitter"), summarize(list, "linkedin")
	p.OtherSocial = append(append(summarize(list, "snapchat"), summarize(list, "pinterest")...), summarize(list, "threads")...)
	// Numbers already known as WhatsApp are not repeated as plain phones.
	var phones []string
	known := map[string]bool{}
	for _, w := range p.WhatsApp {
		known[digitsOnly(w)] = true
	}
	for _, ph := range p.Phones {
		if !known[digitsOnly(ph)] {
			phones = append(phones, ph)
		}
	}
	p.Phones = phones
}

// MetaPagesResult is the output of the meta-pages mode.
type MetaPagesResult struct {
	SourceURL    string        `json:"source_url"`
	SearchTotal  int           `json:"search_total_ads"`
	PagesFound   int           `json:"pages_found"`
	PagesFetched int           `json:"pages_fetched"`
	Pages        []PageProfile `json:"pages"`
	Ads          []Ad          `json:"ads"`
}

func pageAdsURL(pageID, status string) string {
	v := url.Values{}
	v.Set("active_status", status)
	v.Set("ad_type", "all")
	v.Set("country", "ALL")
	v.Set("view_all_page_id", pageID)
	v.Set("search_type", "page")
	v.Set("media_type", "all")
	return "https://www.facebook.com/ads/library/?" + v.Encode()
}

// topN returns the n most frequent non-empty keys in counts.
func topN(counts map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range counts {
		if k != "" {
			all = append(all, kv{k, v})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, all[i].k)
	}
	return out
}

func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// ScrapeMetaPages runs an Ad Library search, collects the unique advertiser
// page IDs from it, then loads each page's own Ad Library view to build a
// profile (totals, active ads, platforms, formats, domains, dates...).
func ScrapeMetaPages(searchURL string, o Options) (*MetaPagesResult, error) {
	sess, err := newMetaSession(o)
	if err != nil {
		return nil, err
	}
	say := func(f string, a ...any) {
		if o.Progress != nil {
			o.Progress(fmt.Sprintf(f, a...))
		}
	}
	delay := o.Delay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	maxPages := o.Limit
	if maxPages <= 0 {
		maxPages = 25
	}
	pause := func() { time.Sleep(delay + time.Duration(rand.Int63n(int64(delay)/2+1))) }

	say("Searching Ad Library...")
	conn, err := sess.page(searchURL)
	if err != nil {
		return nil, err
	}
	search := conn.result(searchURL)
	if search.TotalCount > 0 && len(search.Ads) == 0 {
		return nil, fmt.Errorf("Facebook reported %d matches but returned no ads (throttled); wait a few minutes or rotate your IP and try again", search.TotalCount)
	}
	countPages := func(r *MetaAdsResult) int {
		u := map[string]bool{}
		for _, a := range r.Ads {
			if a.PageID != "" {
				u[a.PageID] = true
			}
		}
		return len(u)
	}
	event := func(e Event) {
		e.Requests = sess.reqs
		if o.OnEvent != nil {
			o.OnEvent(e)
		}
	}
	emitSearch := func(r *MetaAdsResult) {
		event(Event{Stage: "search", Done: min(countPages(r), maxPages), Total: maxPages, Msg: fmt.Sprintf("%d ads scanned", len(r.Ads))})
	}
	emitSearch(search)
	sess.paginate(search, o, func(r *MetaAdsResult) bool { return countPages(r) >= maxPages }, emitSearch)
	res := &MetaPagesResult{SourceURL: searchURL, SearchTotal: search.TotalCount}

	// Unique pages, most ads in the search first.
	counts := map[string]int{}
	first := map[string]Ad{}
	var order []string
	seenAd := map[string]bool{}
	for _, a := range search.Ads {
		if a.PageID == "" {
			continue
		}
		if _, ok := counts[a.PageID]; !ok {
			order = append(order, a.PageID)
			first[a.PageID] = a
		}
		counts[a.PageID]++
		seenAd[a.ID] = true
		res.Ads = append(res.Ads, a)
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	res.PagesFound = len(order)
	say("Found %d ads from %d unique pages", len(search.Ads), len(order))
	if len(order) > maxPages {
		order = order[:maxPages]
	}

	consecutiveErrs := 0
	for i, id := range order {
		seed := first[id]
		say("[%d/%d] %s", i+1, len(order), seed.PageName)
		event(Event{Stage: "enrich", Done: i, Total: len(order), Msg: seed.PageName})
		p := PageProfile{
			PageID: id, PageName: seed.PageName, PageURL: seed.PageURL, ProfilePicture: seed.PagePicture,
			PageLikes: seed.PageLikes, PageCategories: seed.PageCategories, AdsInSearch: counts[id],
			AdLibraryURL: pageAdsURL(id, "all"), SampleAdURL: seed.URL,
		}
		event(Event{Stage: "enrich", Done: i, Total: len(order), Msg: seed.PageName + " · loading its ads"})
		pause()
		all, err := sess.page(pageAdsURL(id, "all"))
		if err != nil {
			p.Error = err.Error()
			res.Pages = append(res.Pages, p)
			event(Event{Stage: "enrich", Done: i + 1, Total: len(order), Msg: p.PageName, Page: &p})
			if consecutiveErrs++; consecutiveErrs >= 3 {
				say("Stopping: 3 pages in a row failed (%v)", err)
				break
			}
			continue
		}
		consecutiveErrs = 0
		ar := all.result("")
		p.TotalAds, p.AdsSampled = ar.TotalCount, ar.Returned

		platforms, formats, domains, ctas, countries := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
		for _, a := range ar.Ads {
			for _, x := range a.Platforms {
				platforms[x]++
			}
			formats[a.Format]++
			domains[domainOf(a.LinkURL)]++
			ctas[a.CTA]++
			for _, x := range a.Countries {
				countries[x]++
			}
			if a.StartDate != "" {
				if p.FirstAdStart == "" || a.StartDate < p.FirstAdStart {
					p.FirstAdStart = a.StartDate
				}
				if a.StartDate > p.LatestAdStart {
					p.LatestAdStart = a.StartDate
				}
			}
			if len(p.PageCategories) == 0 {
				p.PageCategories = a.PageCategories
			}
			if p.PageLikes == 0 {
				p.PageLikes = a.PageLikes
			}
			if p.PageURL == "" {
				p.PageURL = a.PageURL
			}
			if !seenAd[a.ID] {
				seenAd[a.ID] = true
				res.Ads = append(res.Ads, a)
			}
		}
		p.Platforms, p.Formats = topN(platforms, 10), topN(formats, 10)
		p.TopDomains, p.TopCTAs, p.Countries = topN(domains, 5), topN(ctas, 5), topN(countries, 30)

		// Contact details: this page's ads first, then its website.
		cset := newContactSet()
		var pageAds []Ad
		for _, a := range res.Ads {
			if a.PageID == id {
				pageAds = append(pageAds, a)
			}
		}
		contactsFromAds(pageAds, cset)
		event(Event{Stage: "enrich", Done: i, Total: len(order), Msg: seed.PageName + " · reading Facebook page"})
		pause()
		fbSite := crawlFacebookPage(sess, facebookPageURL(p.PageURL, id), cset)
		if o.Websites {
			sites := candidateSites(pageAds, 2)
			if h := domainOf(fbSite); h != "" {
				sites = append([]string{h}, sites...)
			}
			seenSite := map[string]bool{}
			for _, site := range sites {
				if seenSite[site] || len(seenSite) >= 3 {
					continue
				}
				seenSite[site] = true
				event(Event{Stage: "enrich", Done: i, Total: len(order), Msg: seed.PageName + " · reading " + site})
				crawlSite(site, o, cset)
			}
		}
		p.setContacts(cset.list)

		event(Event{Stage: "enrich", Done: i, Total: len(order), Msg: seed.PageName + " · counting active ads"})
		pause()
		if act, err := sess.page(pageAdsURL(id, "active")); err == nil {
			p.ActiveAds = act.Count
		}
		res.Pages = append(res.Pages, p)
		res.PagesFetched++
		event(Event{Stage: "enrich", Done: i + 1, Total: len(order), Msg: p.PageName, Page: &p})
	}
	event(Event{Stage: "done", Done: res.PagesFetched, Total: len(order)})
	return res, nil
}

// Tables implements Tabular for CSV/Excel export.
func (r *MetaPagesResult) Tables() []Table {
	j := func(v []string) string { return strings.Join(v, ", ") }
	itoa := func(n int) string { return strconv.Itoa(n) }
	pages := Table{Name: "Pages", Header: []string{
		"page_id", "page_name", "page_url", "ad_library_url", "profile_picture", "page_likes", "page_categories",
		"total_ads", "active_ads", "ads_in_search", "ads_sampled", "first_ad_start", "latest_ad_start",
		"platforms", "formats", "top_link_domains", "top_ctas", "countries", "sample_ad_url", "error",
		"whatsapp", "phones", "emails", "websites", "instagram", "facebook", "messenger", "telegram", "tiktok", "youtube", "twitter", "linkedin", "other_social", "whatsapp_links"}}
	for _, p := range r.Pages {
		pages.Rows = append(pages.Rows, []string{
			p.PageID, p.PageName, p.PageURL, p.AdLibraryURL, p.ProfilePicture, strconv.FormatInt(p.PageLikes, 10), j(p.PageCategories),
			itoa(p.TotalAds), itoa(p.ActiveAds), itoa(p.AdsInSearch), itoa(p.AdsSampled), p.FirstAdStart, p.LatestAdStart,
			j(p.Platforms), j(p.Formats), j(p.TopDomains), j(p.TopCTAs), j(p.Countries), p.SampleAdURL, p.Error,
			j(p.WhatsApp), j(p.Phones), j(p.Emails), j(p.Websites), j(p.Instagram), j(p.Facebook), j(p.Messenger), j(p.Telegram),
			j(p.TikTok), j(p.YouTube), j(p.Twitter), j(p.LinkedIn), j(p.OtherSocial), j(p.WhatsAppLinks)})
	}
	contacts := Table{Name: "Contacts", Header: []string{"page_id", "page_name", "type", "value", "source", "source_url"}}
	for _, p := range r.Pages {
		for _, c := range p.Contacts {
			contacts.Rows = append(contacts.Rows, []string{p.PageID, p.PageName, c.Type, c.Value, c.Source, c.SourceURL})
		}
	}
	return []Table{pages, contacts, adsTable(r.Ads)}
}

func adsTable(ads []Ad) Table {
	j := func(v []string) string { return strings.Join(v, ", ") }
	t := Table{Name: "Ads", Header: []string{
		"ad_id", "ad_url", "page_id", "page_name", "active", "start_date", "end_date", "platforms", "format",
		"title", "body", "link_url", "link_description", "cta", "images", "videos", "countries", "variations"}}
	for _, a := range ads {
		t.Rows = append(t.Rows, []string{
			a.ID, a.URL, a.PageID, a.PageName, strconv.FormatBool(a.Active), a.StartDate, a.EndDate, j(a.Platforms), a.Format,
			a.Title, a.Body, a.LinkURL, a.LinkDescription, a.CTA, strings.Join(a.Images, "\n"), strings.Join(a.Videos, "\n"),
			j(a.Countries), strconv.Itoa(a.Variations)})
	}
	return t
}

// Tables implements Tabular for the plain ads search.
func (r *MetaAdsResult) Tables() []Table { return []Table{adsTable(r.Ads)} }
