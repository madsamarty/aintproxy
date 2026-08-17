package zte

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

var titleRe = regexp.MustCompile(`(?i)<title>(.*?)</title>`)

func init() {
	modem.Register(&modem.Driver{
		Name:        "zte",
		Description: "ZTE ZXHN gateways (H188A, H1000N, MF289D, and similar)",
		Status:      "supported",
		Detect:      detect,
		DetectModel: detectModel,
		New:         func(ip, user, pass string) modem.Modem { return New(ip, user, pass) },
	})
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

// detect checks if a ZTE modem is reachable by fetching its admin page via HTTPS.
func detect(ip, user, pass string) bool {
	_, model := fetchModel(ip)
	return model != ""
}

// fetchModel fetches the modem's main page and extracts the model from the <title> tag.
func fetchModel(ip string) (bool, string) {
	client := httpClient()
	resp, err := client.Get("https://" + ip + "/")
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return parseTitle(string(body))
}

// parseTitle extracts and decodes the model from the HTML <title> tag.
// ZTE modems encode the model as HTML entities, e.g. "&#90;&#88;&#72;&#78; ZXHN H188A".
func parseTitle(html string) (bool, string) {
	match := titleRe.FindStringSubmatch(html)
	if len(match) < 2 {
		return false, ""
	}
	title := decodeHTMLEntities(strings.TrimSpace(match[1]))
	if !strings.Contains(strings.ToUpper(title), "ZXHN") {
		return false, ""
	}
	return true, title
}

// detectModel probes the modem and returns whether it was detected and its model name.
func detectModel(ip string) (bool, string) {
	return fetchModel(ip)
}

func decodeHTMLEntities(s string) string {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '&' {
			// Try to find ;
			j := strings.IndexByte(s[i:], ';')
			if j == -1 {
				result.WriteByte(s[i])
				i++
				continue
			}
			entity := s[i+1 : i+j]
			if strings.HasPrefix(entity, "#") {
				r := decodeNumericEntity(entity[1:])
				if r != 0 {
					result.WriteRune(r)
					i += j + 1
					continue
				}
			}
			result.WriteByte(s[i])
			i++
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

func decodeNumericEntity(s string) rune {
	if len(s) == 0 {
		return 0
	}
	var n rune
	if s[0] == 'x' || s[0] == 'X' {
		for _, c := range s[1:] {
			if c >= '0' && c <= '9' {
				n = n*16 + rune(c-'0')
			} else if c >= 'a' && c <= 'f' {
				n = n*16 + rune(c-'a'+10)
			} else if c >= 'A' && c <= 'F' {
				n = n*16 + rune(c-'A'+10)
			} else {
				return 0
			}
		}
	} else {
		for _, c := range s {
			if c >= '0' && c <= '9' {
				n = n*10 + rune(c-'0')
			} else {
				return 0
			}
		}
	}
	if unicode.IsPrint(n) {
		return n
	}
	return 0
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
		BaseURL:  "https://" + ip,
		Username: user,
		Password: pass,
		Timeout:  5 * time.Second,
		client:   httpClient(),
	}
}

func (m *Modem) Reboot() error {
	return fmt.Errorf("zte: reboot not yet implemented — use manual reboot")
}

func (m *Modem) SetMobileData(enabled bool) error {
	return fmt.Errorf("zte: mobile data toggle not yet implemented")
}
