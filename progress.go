package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mohamed-sameh/aintscrape/internal/scrape"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// progressUI draws a live panel on stderr: spinner, progress bars, timing and
// the most recently finished pages. It redraws in place.
type progressUI struct {
	mu       sync.Mutex
	title    string
	sub      string
	start    time.Time
	search   scrape.Event
	enrich   scrape.Event
	hasEnr   bool
	enrStart time.Time
	recent   []string
	tally    struct{ pages, withContact, wa, phones, emails, sites, socials, errs int }
	requests int
	lines    int
	frame    int
	stop     chan struct{}
	done     sync.WaitGroup
	sigs     chan os.Signal
}

// contactSummary renders what was found for a page, e.g. "WA +2010… · 2 tel · 1 mail · site shop.com · 4 social".
func contactSummary(p *scrape.PageProfile, social int) string {
	var parts []string
	if len(p.WhatsApp) > 0 {
		parts = append(parts, "\033[32mWA "+p.WhatsApp[0]+"\033[0m")
	} else if len(p.WhatsAppLinks) > 0 {
		parts = append(parts, "\033[32mWA link\033[0m")
	}
	if len(p.Phones) > 0 {
		parts = append(parts, fmt.Sprintf("%d tel", len(p.Phones)))
	}
	if len(p.Emails) > 0 {
		parts = append(parts, fmt.Sprintf("%d mail", len(p.Emails)))
	}
	if len(p.Websites) > 0 {
		parts = append(parts, "site "+strings.TrimPrefix(p.Websites[0], "https://"))
	}
	if social > 0 {
		parts = append(parts, fmt.Sprintf("%d social", social))
	}
	if len(parts) == 0 {
		return "\033[90mno contacts found\033[0m"
	}
	return strings.Join(parts, " · ")
}

func termWidth() int {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	if out, err := cmd.Output(); err == nil {
		f := strings.Fields(string(out))
		if len(f) == 2 {
			if w, err := strconv.Atoi(f[1]); err == nil && w > 20 {
				return w
			}
		}
	}
	return 80
}

func newProgressUI(title, sub string) *progressUI {
	u := &progressUI{title: title, sub: sub, start: time.Now(), stop: make(chan struct{}), sigs: make(chan os.Signal, 1)}
	fmt.Fprint(os.Stderr, "\033[?25l") // hide cursor
	signal.Notify(u.sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		if _, ok := <-u.sigs; ok {
			fmt.Fprint(os.Stderr, "\033[?25h\n")
			os.Exit(130)
		}
	}()
	u.done.Add(1)
	go func() {
		defer u.done.Done()
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-u.stop:
				return
			case <-t.C:
				u.mu.Lock()
				u.frame++
				u.draw()
				u.mu.Unlock()
			}
		}
	}()
	return u
}

func (u *progressUI) handle(e scrape.Event) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if e.Requests > 0 {
		u.requests = e.Requests
	}
	switch e.Stage {
	case "search":
		u.search = e
	case "enrich":
		if !u.hasEnr {
			u.hasEnr, u.enrStart = true, time.Now()
		}
		u.enrich = e
		if p := e.Page; p != nil {
			u.tally.pages++
			if p.Error != "" {
				u.tally.errs++
				u.recent = append(u.recent, fmt.Sprintf("\033[31m✗\033[0m %-24s %s", clip(p.PageName, 24), clip(p.Error, 60)))
			} else {
				social := len(p.Instagram) + len(p.Facebook) + len(p.Messenger) + len(p.Telegram) + len(p.TikTok) + len(p.YouTube) + len(p.Twitter) + len(p.LinkedIn) + len(p.OtherSocial)
				t := &u.tally
				t.wa += len(p.WhatsApp) + len(p.WhatsAppLinks)
				t.phones += len(p.Phones)
				t.emails += len(p.Emails)
				t.sites += len(p.Websites)
				t.socials += social
				if len(p.WhatsApp)+len(p.WhatsAppLinks)+len(p.Phones)+len(p.Emails) > 0 {
					t.withContact++
				}
				u.recent = append(u.recent, fmt.Sprintf("\033[32m✓\033[0m %-24s %s ads (%s active) · %s", clip(p.PageName, 24),
					thousands(p.TotalAds), thousands(p.ActiveAds), contactSummary(p, social)))
			}
			if len(u.recent) > 8 {
				u.recent = u.recent[len(u.recent)-8:]
			}
		}
	}
	u.draw()
}

func (u *progressUI) finish() {
	close(u.stop)
	u.done.Wait()
	signal.Stop(u.sigs)
	close(u.sigs)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.erase()
	fmt.Fprint(os.Stderr, "\033[?25h")
}

func (u *progressUI) elapsed() time.Duration { return time.Since(u.start).Round(time.Second) }

func (u *progressUI) erase() {
	if u.lines > 0 {
		fmt.Fprintf(os.Stderr, "\033[%dA", u.lines)
		for i := 0; i < u.lines; i++ {
			fmt.Fprint(os.Stderr, "\033[2K\n")
		}
		fmt.Fprintf(os.Stderr, "\033[%dA", u.lines)
	}
	u.lines = 0
}

func bar(done, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	if done > total {
		done = total
	}
	n := done * width / total
	return "\033[36m" + strings.Repeat("█", n) + "\033[90m" + strings.Repeat("░", width-n) + "\033[0m"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func (u *progressUI) stageLine(label string, e scrape.Event, active bool, start time.Time) string {
	spin := "\033[32m✓\033[0m"
	if active {
		spin = "\033[36m" + spinnerFrames[u.frame%len(spinnerFrames)] + "\033[0m"
	}
	pct := ""
	if e.Total > 0 {
		pct = fmt.Sprintf("%3d%%", e.Done*100/e.Total)
	}
	return fmt.Sprintf("  %s %-24s %s %4d/%-4d %s", spin, label, bar(e.Done, e.Total, 24), e.Done, e.Total, pct)
}

func (u *progressUI) draw() {
	w := termWidth()
	var lines []string
	lines = append(lines, "", "  \033[1m"+u.title+"\033[0m", "  \033[90m"+clip(u.sub, w-4)+"\033[0m", "")

	searchActive := !u.hasEnr
	if u.search.Stage != "" || !u.hasEnr {
		lines = append(lines, u.stageLine("Collecting", u.search, searchActive, u.start))
		if u.search.Msg != "" && searchActive {
			lines = append(lines, "    \033[90m"+u.search.Msg+"\033[0m")
		}
	}
	if u.hasEnr {
		lines = append(lines, u.stageLine("Enriching pages", u.enrich, u.enrich.Done < u.enrich.Total, u.enrStart))
		if u.enrich.Done < u.enrich.Total && u.enrich.Msg != "" {
			lines = append(lines, "    \033[90m"+clip("now: "+u.enrich.Msg, w-8)+"\033[0m")
		}
	}

	eta := ""
	if u.hasEnr && u.enrich.Done > 0 && u.enrich.Total > u.enrich.Done {
		per := time.Since(u.enrStart) / time.Duration(u.enrich.Done)
		eta = " · ETA ~" + fmtDur(per*time.Duration(u.enrich.Total-u.enrich.Done))
	} else if !u.hasEnr && u.search.Done > 0 && u.search.Total > u.search.Done {
		per := time.Since(u.start) / time.Duration(u.search.Done)
		eta = " · ETA ~" + fmtDur(per*time.Duration(u.search.Total-u.search.Done))
	}
	if t := u.tally; t.pages > 0 {
		lines = append(lines, "", fmt.Sprintf("  \033[1mFound so far\033[0m  %d pages · \033[32m%d with contact\033[0m · %d WhatsApp · %d phones · %d emails · %d websites · %d socials%s",
			t.pages, t.withContact, t.wa, t.phones, t.emails, t.sites, t.socials, map[bool]string{true: fmt.Sprintf(" · \033[31m%d failed\033[0m", t.errs)}[t.errs > 0]))
	}
	lines = append(lines, "", fmt.Sprintf("  \033[90mElapsed %s%s · %d requests · Ctrl+C to cancel\033[0m", fmtDur(u.elapsed()), eta, u.requests))
	if len(u.recent) > 0 {
		lines = append(lines, "")
		for _, r := range u.recent {
			lines = append(lines, "  "+r)
		}
	}
	lines = append(lines, "")

	u.erase()
	for _, l := range lines {
		fmt.Fprintln(os.Stderr, l)
	}
	u.lines = len(lines)
}
