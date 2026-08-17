package huawei

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mohamed-sameh/aintproxy/internal/modem"
)

var (
	sesRe = regexp.MustCompile(`<SesInfo>(.*?)</SesInfo>`)
	tokRe = regexp.MustCompile(`<TokInfo>(.*?)</TokInfo>`)
)

type ModemError struct {
	Op  string
	Msg string
}

func (e *ModemError) Error() string {
	return fmt.Sprintf("huawei: %s: %s", e.Op, e.Msg)
}

func init() {
	modem.Register(&modem.Driver{
		Name:        "huawei-hilink",
		Description: "Huawei HiLink gateways (B525, B535, E5172, and similar)",
		Status:      "supported",
		Detect:      detect,
		DetectModel: detectModel,
		New:         func(ip, user, pass string) modem.Modem { return New(ip, user, pass) },
	})
}

// detect probes the modem for the HiLink SesTokInfo endpoint.
func detect(ip, user, pass string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + ip + "/api/webserver/SesTokInfo")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return sesRe.Match(body) && tokRe.Match(body)
}

// detectModel probes the modem for device information and returns the model name.
func detectModel(ip string) (bool, string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + ip + "/api/device/basic_information")
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "<response>") {
		return false, ""
	}
	model := extractXMLTag(bodyStr, "devicename")
	if model == "" {
		model = extractXMLTag(bodyStr, "spreadname_en")
	}
	return true, model
}

func extractXMLTag(xmlStr, tag string) string {
	re := regexp.MustCompile(`<` + tag + `>(.*?)</` + tag + `>`)
	if match := re.FindStringSubmatch(xmlStr); len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

type Modem struct {
	BaseURL  string
	Username string
	Password string
	Timeout  time.Duration
	cookie   string
	token    string
	client   *http.Client
}

func New(ip, user, pass string) *Modem {
	return &Modem{
		BaseURL:  "http://" + ip,
		Username: user,
		Password: pass,
		Timeout:  5 * time.Second,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (m *Modem) Login() error {
	cookie, token, err := m.sessionTokens()
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	hashed := m.passwordHash(m.Username, m.Password, token)
	payload := fmt.Sprintf(
		`<request><Username>%s</Username><Password>%s</Password><password_type>4</password_type></request>`,
		m.Username, hashed,
	)

	req, err := http.NewRequest("POST", m.BaseURL+"/api/user/login", strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("__RequestVerificationToken", token)
	req.Header.Set("Content-Type", "application/xml")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !isOK(string(body)) {
		return &ModemError{Op: "login", Msg: string(body)}
	}

	m.cookie = headerCookie(resp, cookie)
	m.token = headerToken(resp, token)

	if m.token == "" {
		tokResp, err := http.NewRequest("GET", m.BaseURL+"/api/webserver/TokInfo", nil)
		if err == nil {
			tokResp.Header.Set("Cookie", m.cookie)
			r, err := m.client.Do(tokResp)
			if err == nil {
				defer r.Body.Close()
				tokBody, _ := io.ReadAll(r.Body)
				if match := tokRe.FindStringSubmatch(string(tokBody)); len(match) > 1 {
					m.token = match[1]
				}
			}
		}
	}

	return nil
}

func (m *Modem) Reboot() error {
	if err := m.Login(); err != nil {
		return err
	}
	resp, err := m.post("/api/device/control", "<request><Control>1</Control></request>")
	if err != nil {
		return &ModemError{Op: "reboot", Msg: err.Error()}
	}
	if !isOK(resp) {
		return &ModemError{Op: "reboot", Msg: resp}
	}
	return nil
}

func (m *Modem) SetMobileData(enabled bool) error {
	if err := m.Login(); err != nil {
		return err
	}
	val := "0"
	if enabled {
		val = "1"
	}
	payload := fmt.Sprintf("<request><dataswitch>%s</dataswitch></request>", val)
	resp, err := m.post("/api/dialup/mobile-dataswitch", payload)
	if err != nil {
		action := "disable"
		if enabled {
			action = "enable"
		}
		return &ModemError{Op: "set_mobile_data " + action, Msg: err.Error()}
	}
	if !isOK(resp) {
		action := "disable"
		if enabled {
			action = "enable"
		}
		return &ModemError{Op: "set_mobile_data " + action, Msg: resp}
	}
	return nil
}

func (m *Modem) sessionTokens() (string, string, error) {
	req, err := http.NewRequest("GET", m.BaseURL+"/api/webserver/SesTokInfo", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	sesMatch := sesRe.FindStringSubmatch(string(body))
	tokMatch := tokRe.FindStringSubmatch(string(body))
	if len(sesMatch) < 2 || len(tokMatch) < 2 {
		return "", "", fmt.Errorf("could not parse session tokens from: %s", string(body))
	}
	return sesMatch[1], tokMatch[1], nil
}

func (m *Modem) post(path, payload string) (string, error) {
	if m.cookie == "" || m.token == "" {
		if err := m.Login(); err != nil {
			return "", err
		}
	}

	req, err := http.NewRequest("POST", m.BaseURL+path, strings.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", m.cookie)
	req.Header.Set("__RequestVerificationToken", m.token)
	req.Header.Set("Content-Type", "application/xml")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if t := headerToken(resp, ""); t != "" {
		m.token = t
	}
	if c := headerCookie(resp, ""); c != "" {
		m.cookie = c
	}

	return string(body), nil
}

func passwordHash(username, password, token string) string {
	h1 := sha256.Sum256([]byte(password))
	h1hex := fmt.Sprintf("%x", h1)
	b1 := base64.StdEncoding.EncodeToString([]byte(h1hex))

	combined := username + b1 + token
	h2 := sha256.Sum256([]byte(combined))
	h2hex := fmt.Sprintf("%x", h2)
	return base64.StdEncoding.EncodeToString([]byte(h2hex))
}

func (m *Modem) passwordHash(username, password, token string) string {
	return passwordHash(username, password, token)
}

func isOK(body string) bool {
	var resp struct {
		XMLName xml.Name `xml:"response"`
		Text    string   `xml:",chardata"`
	}
	if err := xml.Unmarshal([]byte(body), &resp); err == nil {
		return resp.Text == "OK"
	}
	return strings.Contains(body, "OK")
}

func headerCookie(resp *http.Response, def string) string {
	raw := resp.Header.Get("Set-Cookie")
	if raw == "" {
		return def
	}
	if i := strings.IndexByte(raw, ';'); i > 0 {
		return raw[:i]
	}
	return raw
}

func headerToken(resp *http.Response, def string) string {
	if t := resp.Header.Get("__requestverificationtokenone"); t != "" {
		return t
	}
	if t := resp.Header.Get("__requestverificationtoken"); t != "" {
		return t
	}
	return def
}
