package tplink

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

var titleRe = regexp.MustCompile(`(?i)<title>(.*?)</title>`)

func init() {
	modem.Register(&modem.Driver{
		Name:        "tplink",
		Description: "TP-Link LTE gateways (Archer MR200, MR600, TD-W8968, and similar)",
		Status:      "supported",
		Detect:      detect,
		DetectModel: detectModel,
		New:         func(ip, user, pass string) modem.Modem { return New(ip, user, pass) },
	})
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 3 * time.Second}
}

// detect checks if a TP-Link modem is reachable.
func detect(ip, user, pass string) bool {
	_, model := fetchModel(ip)
	return model != ""
}

// fetchModel fetches the modem's main page and extracts the model from the title.
func fetchModel(ip string) (bool, string) {
	client := httpClient()
	// TP-Link modems typically use HTTP, not HTTPS
	resp, err := client.Get("http://" + ip + "/")
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return parseTitle(string(body))
}

// parseTitle extracts the model from the HTML <title> tag.
// TP-Link titles look like: "TP-LINK Archer MR600" or "TD-W8968 - TP-LINK".
func parseTitle(html string) (bool, string) {
	match := titleRe.FindStringSubmatch(html)
	if len(match) < 2 {
		return false, ""
	}
	title := strings.TrimSpace(match[1])
	upper := strings.ToUpper(title)
	if strings.Contains(upper, "TP-LINK") || strings.Contains(upper, "TP LINK") || strings.Contains(upper, "ARCHER") {
		return true, title
	}
	return false, ""
}

// detectModel probes the modem and returns whether it was detected and its model name.
func detectModel(ip string) (bool, string) {
	return fetchModel(ip)
}

type Modem struct {
	BaseURL  string
	Username string
	Password string
	Timeout  time.Duration
	client   *http.Client
}

func New(ip, user, pass string) *Modem {
	return &Modem{
		BaseURL:  "http://" + ip,
		Username: user,
		Password: pass,
		Timeout:  5 * time.Second,
		client:   httpClient(),
	}
}

func (m *Modem) Reboot() error {
	return fmt.Errorf("tplink: reboot not yet implemented — use manual reboot")
}

func (m *Modem) SetMobileData(enabled bool) error {
	return fmt.Errorf("tplink: mobile data toggle not yet implemented")
}
