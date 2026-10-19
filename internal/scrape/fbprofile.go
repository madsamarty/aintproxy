package scrape

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	fbExternalRe = regexp.MustCompile(`"external_url"\s*:\s*"([^"]+)"`)
	fbEmailRe    = regexp.MustCompile(`"(?:email|public_email)"\s*:\s*"([^"]+)"`)
	fbPhoneRe    = regexp.MustCompile(`"(?:phone|phone_number|single_line_phone|display_phone)"\s*:\s*"([^"]+)"`)
	fbMetaDescRe = regexp.MustCompile(`<meta\s+(?:property="og:description"|name="description")\s+content="([^"]*)"`)
	fbWebsiteRe  = regexp.MustCompile(`"(?:website|websites)"\s*:\s*\[?\s*"([^"]+)"`)
)

// jsonUnescape undoes the JSON string escaping Facebook uses inside its page
// payload (\/ and \uXXXX) so URLs and numbers can be read.
func jsonUnescape(s string) string {
	s = strings.ReplaceAll(s, `\/`, "/")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+5 < len(s) && s[i+1] == 'u' {
			var r rune
			ok := true
			for _, c := range s[i+2 : i+6] {
				switch {
				case c >= '0' && c <= '9':
					r = r<<4 | (c - '0')
				case c >= 'a' && c <= 'f':
					r = r<<4 | (c - 'a' + 10)
				case c >= 'A' && c <= 'F':
					r = r<<4 | (c - 'A' + 10)
				default:
					ok = false
				}
			}
			if ok {
				b.WriteRune(r)
				i += 5
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unwrapFBRedirect turns https://l.facebook.com/l.php?u=<real> into <real>.
func unwrapFBRedirect(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.HasSuffix(u.Hostname(), "facebook.com") && strings.HasPrefix(u.Path, "/l.php") {
		if t := u.Query().Get("u"); t != "" {
			return t
		}
	}
	return raw
}

// facebookPageURL picks the page's profile URL, falling back to its ID.
func facebookPageURL(pageURL, pageID string) string {
	if pageURL != "" {
		return pageURL
	}
	return "https://www.facebook.com/" + pageID
}

// scanFBProfile records the contact details a logged-out Facebook page
// profile exposes: its username, linked website, email, phone / WhatsApp and
// anything in the short description. It returns the linked website, if any.
func scanFBProfile(body []byte, pageURL string, set *contactSet) (website string) {
	src := string(body)
	add := func(typ, val string) { set.add(typ, val, "facebook_page", pageURL) }
	classify := func(raw string) {
		classifyLink(raw, "facebook_page", pageURL, set)
	}

	// Username: facebook.com/<username> (numeric profile.php ids are kept too).
	classify(pageURL)

	if m := fbExternalRe.FindStringSubmatch(src); m != nil {
		website = unwrapFBRedirect(jsonUnescape(m[1]))
	} else if m := fbWebsiteRe.FindStringSubmatch(src); m != nil {
		website = unwrapFBRedirect(jsonUnescape(m[1]))
	}
	if website != "" {
		if !strings.Contains(website, "://") {
			website = "https://" + website
		}
		classify(website)
		if h := domainOf(website); h != "" && !skipSiteHosts[h] {
			add("website", "https://"+h)
		} else {
			website = ""
		}
	}
	for _, m := range fbEmailRe.FindAllStringSubmatch(src, -1) {
		if v := cleanEmail(jsonUnescape(m[1])); v != "" {
			add("email", v)
		}
	}
	for _, m := range fbPhoneRe.FindAllStringSubmatch(src, -1) {
		if p := normPhone(arabicDigits.Replace(jsonUnescape(m[1]))); p != "" {
			add("phone", p)
		}
	}
	// WhatsApp / social links embedded in the payload.
	for _, m := range urlRe.FindAllString(jsonUnescape(src), -1) {
		h := domainOf(m)
		switch h {
		case "wa.me", "api.whatsapp.com", "wa.link", "chat.whatsapp.com", "instagram.com", "t.me", "tiktok.com", "youtube.com", "linkedin.com", "twitter.com", "x.com":
			classify(m)
		}
	}
	// Short description ("Call us: ...", emails, links).
	if m := fbMetaDescRe.FindStringSubmatch(src); m != nil {
		extractFromText(html.UnescapeString(m[1]), "facebook_page", pageURL, set)
	}
	return website
}

// crawlFacebookPage fetches the advertiser's Facebook page through the
// session and records its public contact details. Failures are ignored.
func crawlFacebookPage(sess *metaSession, pageURL string, set *contactSet) (website string) {
	body, status, err := sess.get(pageURL)
	if err != nil {
		return ""
	}
	if p := challengeRe.Find(body); p != nil {
		req, _ := http.NewRequest(http.MethodPost, "https://www.facebook.com"+string(p), nil)
		req.Header.Set("User-Agent", sess.ua)
		if resp, err := sess.client.Do(req); err == nil {
			resp.Body.Close()
		}
		body, status, err = sess.get(pageURL)
		if err != nil {
			return ""
		}
	}
	if status != http.StatusOK {
		return ""
	}
	return scanFBProfile(body, pageURL, set)
}
