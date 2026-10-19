package scrape

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Contact is one piece of public contact information found for an advertiser.
type Contact struct {
	Type      string `json:"type"` // whatsapp, whatsapp_link, phone, email, instagram, facebook, messenger, telegram, tiktok, youtube, twitter, linkedin, snapchat, pinterest, threads, website
	Value     string `json:"value"`
	Source    string `json:"source"`               // ad_link, ad_text, website
	SourceURL string `json:"source_url,omitempty"` // ad or web page it came from
}

var (
	arabicDigits = strings.NewReplacer("٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9")

	emailRe    = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}`)
	urlRe      = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)
	intlPhone  = regexp.MustCompile(`(?:\+|\b00)\d{1,3}[\s\-.()]*\d(?:[\s\-.()]*\d){6,11}`)
	localPhone = regexp.MustCompile(`\b(?:01[0125]\d{8}|05\d{8}|07[0-9]\d{7,8})\b`)
	keyPhone   = regexp.MustCompile(`(?i)(?:whats\s?app|واتس\s?اب|واتساب|call|tel|phone|mobile|اتصل|اتصال|تليفون|موبايل|هاتف|جوال|للحجز|للتواصل|رقم)\D{0,15}?(\+?\d[\d\s\-]{7,14}\d)`)
	ldPhoneRe  = regexp.MustCompile(`"telephone"\s*:\s*"([^"]+)"`)
	ldEmailRe  = regexp.MustCompile(`"email"\s*:\s*"([^"]+)"`)
)

var junkEmailDomains = []string{"sentry.io", "wixpress.com", "example.com", "domain.com", "email.com", "yourdomain", "sentry-next.wixpress.com"}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normPhone returns +digits for international numbers, plain digits otherwise;
// "" if the length is implausible.
func normPhone(s string) string {
	s = strings.TrimSpace(s)
	d := digitsOnly(s)
	if len(d) < 8 || len(d) > 15 {
		return ""
	}
	if strings.HasPrefix(s, "+") {
		return "+" + d
	}
	if strings.HasPrefix(d, "00") {
		return "+" + strings.TrimPrefix(d, "00")
	}
	return d
}

type contactSet struct {
	seen map[string]bool
	list []Contact
}

func newContactSet() *contactSet { return &contactSet{seen: map[string]bool{}} }

func (c *contactSet) add(typ, val, source, srcURL string) {
	val = strings.TrimSpace(val)
	if val == "" {
		return
	}
	k := typ + "|" + strings.ToLower(val)
	if typ == "phone" || typ == "whatsapp" {
		k = typ + "|" + digitsOnly(val)
	}
	if c.seen[k] {
		return
	}
	c.seen[k] = true
	c.list = append(c.list, Contact{Type: typ, Value: val, Source: source, SourceURL: srcURL})
}

var socialHosts = map[string]string{
	"instagram.com": "instagram", "facebook.com": "facebook", "fb.com": "facebook", "m.facebook.com": "facebook",
	"tiktok.com": "tiktok", "youtube.com": "youtube", "youtu.be": "youtube", "twitter.com": "twitter", "x.com": "twitter",
	"linkedin.com": "linkedin", "t.me": "telegram", "telegram.me": "telegram", "snapchat.com": "snapchat",
	"pinterest.com": "pinterest", "threads.net": "threads", "m.me": "messenger", "messenger.com": "messenger", "ig.me": "instagram",
}

var nonProfileSeg = map[string]bool{"p": true, "reel": true, "reels": true, "explore": true, "share": true, "sharer": true, "sharer.php": true,
	"tr": true, "dialog": true, "plugins": true, "watch": true, "login": true, "ads": true, "policies": true, "privacy": true,
	"help": true, "intent": true, "home": true, "hashtag": true, "search": true, "story": true, "stories": true, "legal": true}

// classifyLink turns a URL into zero or more contacts.
func classifyLink(raw, source, srcURL string, set *contactSet) {
	raw = strings.TrimSpace(strings.TrimRight(raw, ".,;:!?)»”"))
	low := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(low, "mailto:"):
		e := strings.SplitN(strings.TrimPrefix(raw, raw[:7]), "?", 2)[0]
		if v := cleanEmail(e); v != "" {
			set.add("email", v, source, srcURL)
		}
		return
	case strings.HasPrefix(low, "tel:"):
		if p := normPhone(strings.TrimPrefix(raw, raw[:4])); p != "" {
			set.add("phone", p, source, srcURL)
		}
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	switch host {
	case "wa.me", "api.whatsapp.com", "web.whatsapp.com", "whatsapp.com", "wa.link", "chat.whatsapp.com":
		if d := digitsOnly(arabicDigits.Replace(u.Query().Get("phone"))); len(d) >= 8 && len(d) <= 15 {
			set.add("whatsapp", "+"+d, source, srcURL)
		} else if len(segs) > 0 && host == "wa.me" {
			d := arabicDigits.Replace(segs[0])
			if strings.Trim(d, "+0123456789") == "" {
				if p := normPhone("+" + digitsOnly(d)); p != "" {
					set.add("whatsapp", p, source, srcURL)
				}
			} else {
				set.add("whatsapp_link", "https://wa.me/"+strings.Join(segs, "/"), source, srcURL)
			}
		} else if host == "chat.whatsapp.com" || (host == "whatsapp.com" && len(segs) > 0 && segs[0] == "channel") || host == "wa.link" {
			set.add("whatsapp_link", "https://"+host+u.Path, source, srcURL)
		}
		return
	}
	typ, ok := socialHosts[host]
	if !ok {
		return
	}
	if len(segs) == 0 || nonProfileSeg[strings.ToLower(segs[0])] {
		return
	}
	name := strings.TrimPrefix(segs[0], "@")
	if typ == "facebook" {
		if name == "profile.php" {
			name = "profile.php?id=" + u.Query().Get("id")
		} else if name == "pages" && len(segs) > 1 {
			name = segs[1]
		}
	}
	if typ == "tiktok" && !strings.HasPrefix(segs[0], "@") {
		return
	}
	set.add(typ, name, source, srcURL)
}

func cleanEmail(e string) string {
	e = strings.ToLower(strings.Trim(e, ".,;:<>()[]\"' "))
	if !emailRe.MatchString(e) {
		return ""
	}
	for _, j := range junkEmailDomains {
		if strings.Contains(e, j) {
			return ""
		}
	}
	if strings.HasSuffix(e, ".png") || strings.HasSuffix(e, ".jpg") || strings.HasSuffix(e, ".webp") || strings.HasSuffix(e, ".svg") || strings.HasSuffix(e, ".gif") || strings.Contains(e, "@2x") {
		return ""
	}
	return e
}

// extractFromText finds links, emails and phone numbers in free text.
func extractFromText(text, source, srcURL string, set *contactSet) {
	text = arabicDigits.Replace(text)
	for _, u := range urlRe.FindAllString(text, -1) {
		classifyLink(u, source, srcURL, set)
	}
	for _, e := range emailRe.FindAllString(text, -1) {
		if v := cleanEmail(e); v != "" {
			set.add("email", v, source, srcURL)
		}
	}
	// Strip URLs so digits inside links are not read as phone numbers.
	plain := urlRe.ReplaceAllString(text, " ")
	for _, m := range intlPhone.FindAllString(plain, -1) {
		if p := normPhone(m); p != "" {
			set.add("phone", p, source, srcURL)
		}
	}
	for _, m := range localPhone.FindAllString(plain, -1) {
		set.add("phone", m, source, srcURL)
	}
	for _, m := range keyPhone.FindAllStringSubmatch(plain, -1) {
		if p := normPhone(m[1]); p != "" {
			set.add("phone", p, source, srcURL)
		}
	}
}

// contactsFromAds scans every ad's link and text fields.
func contactsFromAds(ads []Ad, set *contactSet) {
	for _, a := range ads {
		for _, l := range append([]string{a.LinkURL}, a.ExtraLinks...) {
			if l != "" {
				classifyLink(l, "ad_link", a.URL, set)
			}
		}
		extractFromText(strings.Join([]string{a.Body, a.Title, a.LinkDescription}, "\n"), "ad_text", a.URL, set)
	}
}

// ---- website crawl ------------------------------------------------------

var skipSiteHosts = map[string]bool{
	"play.google.com": true, "itunes.apple.com": true, "apps.apple.com": true, "facebook.com": true, "fb.com": true, "fb.me": true,
	"m.me": true, "instagram.com": true, "wa.me": true, "whatsapp.com": true, "api.whatsapp.com": true, "t.me": true, "tiktok.com": true,
	"youtube.com": true, "youtu.be": true, "twitter.com": true, "x.com": true, "linkedin.com": true, "bit.ly": true, "l.facebook.com": true,
	"lnkd.in": true, "maps.google.com": true, "google.com": true, "maps.app.goo.gl": true, "g.page": true, "forms.gle": true, "docs.google.com": true, "goo.gl": true, "tinyurl.com": true, "wa.link": true, "ig.me": true,
}

// candidateSites returns the most frequently advertised non-social domains.
func candidateSites(ads []Ad, n int) []string {
	counts := map[string]int{}
	for _, a := range ads {
		u, err := url.Parse(a.LinkURL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		if skipSiteHosts[h] || !strings.Contains(h, ".") {
			continue
		}
		counts[h]++
	}
	return topN(counts, n)
}

var contactPathRe = regexp.MustCompile(`(?i)contact|about|اتصل|تواصل|من نحن|support|help`)

// crawlSite visits a site's homepage plus up to two contact/about pages and
// records contacts. It uses plain HTTP, a short timeout and a small delay.
func crawlSite(host string, o Options, set *contactSet) {
	client := &http.Client{Timeout: 10 * time.Second}
	ua := o.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	base := "https://" + host
	visited := map[string]bool{}
	queue := []string{base + "/"}
	for len(queue) > 0 && len(visited) < 3 {
		pageURL := queue[0]
		queue = queue[1:]
		if visited[pageURL] {
			continue
		}
		visited[pageURL] = true
		req, _ := http.NewRequest(http.MethodGet, pageURL, nil)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9,ar;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			if len(visited) == 1 {
				return // site unreachable
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		if final := resp.Request.URL.Hostname(); registrable(final) != registrable(host) {
			continue // short link / redirector that left the advertiser's domain
		}
		set.add("website", base, "website", pageURL)
		links := scanHTML(body, pageURL, set)
		if len(visited) == 1 {
			for _, l := range links {
				if len(queue) >= 2 {
					break
				}
				if u, err := url.Parse(l); err == nil && strings.TrimPrefix(u.Hostname(), "www.") == host && contactPathRe.MatchString(u.Path+" "+l) && !visited[l] {
					queue = append(queue, l)
				}
			}
			if len(queue) == 0 {
				queue = append(queue, base+"/contact", base+"/contact-us")
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// scanHTML records contacts found in an HTML page and returns its links.
func scanHTML(body []byte, pageURL string, set *contactSet) []string {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(pageURL)
	var links []string
	var text strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script":
				for _, a := range n.Attr {
					if a.Key == "type" && strings.Contains(a.Val, "ld+json") && n.FirstChild != nil {
						for _, m := range ldPhoneRe.FindAllStringSubmatch(n.FirstChild.Data, -1) {
							if p := normPhone(m[1]); p != "" {
								set.add("phone", p, "website", pageURL)
							}
						}
						for _, m := range ldEmailRe.FindAllStringSubmatch(n.FirstChild.Data, -1) {
							if v := cleanEmail(strings.TrimPrefix(m[1], "mailto:")); v != "" {
								set.add("email", v, "website", pageURL)
							}
						}
					}
				}
				return
			case "style", "noscript":
				return
			case "a":
				for _, a := range n.Attr {
					if a.Key != "href" {
						continue
					}
					href := strings.TrimSpace(a.Val)
					if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:") {
						continue
					}
					if base != nil && !strings.Contains(href, ":") {
						if r, err := base.Parse(href); err == nil {
							href = r.String()
						}
					}
					links = append(links, href)
					classifyLink(href, "website", pageURL, set)
				}
			}
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	extractFromText(text.String(), "website", pageURL, set)
	sort.Strings(links)
	return links
}

// summarize groups a contact list by type for the Pages table.
func summarize(list []Contact, typ string) []string {
	var out []string
	for _, c := range list {
		if c.Type == typ {
			out = append(out, c.Value)
		}
	}
	return out
}

// registrable returns the last two labels of a hostname (good enough to tell
// "shop.com" from "bit.ly" without a public-suffix list).
func registrable(h string) string {
	p := strings.Split(strings.TrimPrefix(strings.ToLower(h), "www."), ".")
	if len(p) > 2 {
		p = p[len(p)-2:]
	}
	return strings.Join(p, ".")
}
