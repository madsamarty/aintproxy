package scrape

import "testing"

func TestScanFBProfile(t *testing.T) {
	page := `<meta property="og:description" content="Shop. Call us 01012345678 or WhatsApp +20 100 123 4567" />
	{"external_url":"https:\/\/l.facebook.com\/l.php?u=http%3A%2F%2Fshop.com%2F&h=AU","email":"Hi@Shop.com","phone":"+20 2 2345 6789"}
	"cta":"https:\/\/wa.me\/201111111111" "ig":"https:\/\/www.instagram.com\/shop.eg\/"`
	set := newContactSet()
	site := scanFBProfile([]byte(page), "https://www.facebook.com/shop.page", set)
	if site != "http://shop.com/" {
		t.Errorf("website = %q", site)
	}
	for _, w := range [][2]string{{"facebook", "shop.page"}, {"website", "https://shop.com"}, {"email", "hi@shop.com"},
		{"phone", "+20223456789"}, {"phone", "01012345678"}, {"whatsapp", "+201111111111"}, {"instagram", "shop.eg"}} {
		if !has(set, w[0], w[1]) {
			t.Errorf("missing %v in %+v", w, set.list)
		}
	}
}
