package scrape

import (
	"strings"
	"testing"
)

func has(set *contactSet, typ, val string) bool {
	for _, c := range set.list {
		if c.Type == typ && strings.EqualFold(c.Value, val) {
			return true
		}
	}
	return false
}

func TestExtractContacts(t *testing.T) {
	set := newContactSet()
	text := "اطلب عبر واتساب https://wa.me/+201015335600 أو اتصل ٠١٠٢٣٤٥٦٧٨٩ \nmail: Sales@Shop.com, ig: https://www.instagram.com/shop.eg/ " +
		"https://api.whatsapp.com/send/?phone=966501234567&text=hi https://www.facebook.com/shop.page/ call +20 100 123 4567 price 199.99 EGP"
	extractFromText(text, "ad_text", "u", set)
	for _, w := range [][2]string{
		{"whatsapp", "+201015335600"}, {"whatsapp", "+966501234567"}, {"email", "sales@shop.com"},
		{"instagram", "shop.eg"}, {"facebook", "shop.page"}, {"phone", "+201001234567"},
	} {
		if !has(set, w[0], w[1]) {
			t.Errorf("missing %v in %+v", w, set.list)
		}
	}
	if !has(set, "phone", "01023456789") {
		t.Errorf("Arabic-digit local number not found: %+v", set.list)
	}
	for _, c := range set.list {
		if c.Type == "phone" && strings.Contains(c.Value, "199") {
			t.Errorf("price parsed as phone: %v", c)
		}
	}
}

func TestScanHTML(t *testing.T) {
	set := newContactSet()
	page := `<html><body><a href="mailto:Info@Brand.com?subject=x">mail</a><a href="tel:+20 2 2345 6789">call</a>
	<a href="/contact-us">Contact</a><a href="https://wa.me/201234567890">wa</a><a href="https://instagram.com/brand/">ig</a>
	<script type="application/ld+json">{"telephone":"+20 100 000 1111","email":"hi@brand.com"}</script></body></html>`
	links := scanHTML([]byte(page), "https://brand.com/", set)
	for _, w := range [][2]string{{"email", "info@brand.com"}, {"phone", "+20223456789"}, {"whatsapp", "+201234567890"},
		{"instagram", "brand"}, {"phone", "+201000001111"}, {"email", "hi@brand.com"}} {
		if !has(set, w[0], w[1]) {
			t.Errorf("missing %v in %+v", w, set.list)
		}
	}
	found := false
	for _, l := range links {
		if l == "https://brand.com/contact-us" {
			found = true
		}
	}
	if !found {
		t.Errorf("relative contact link not resolved: %v", links)
	}
}
