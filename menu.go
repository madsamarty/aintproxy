package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mohamed-sameh/aintscrape/internal/config"
	"github.com/mohamed-sameh/aintscrape/internal/devices"
)

func isVirtual(d devices.Device) bool {
	n := d.Interface
	return n == "lo" || n == "docker0" || strings.HasPrefix(n, "br-") ||
		strings.HasPrefix(n, "veth") || d.Type == "tun" || d.Type == "bridge" || d.Type == "loopback"
}

// runSelect shows an interactive menu of network devices and saves the choice
// as modem.interface (and modem.ip from the device gateway) in the config.
func runSelect(path string) {
	ensureAccess(path)
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "Config not found at %s — run: sudo aintscrape config\n", path)
		os.Exit(2)
	}
	runCmd := func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	all, err := devices.ListAll(runCmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	devices.EnrichWithModemInfo(all, runCmd)

	current := ""
	if cfg, err := config.Load(path); err == nil {
		current = cfg.Modem.Interface
	}

	if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		selectWithArrows(path, all, current)
		return
	}

	in := bufio.NewReader(os.Stdin)
	showAll := false
	for {
		var list []devices.Device
		for _, d := range all {
			if showAll || !isVirtual(d) {
				list = append(list, d)
			}
		}
		fmt.Println("\nSelect the device aintscrape should use:")
		fmt.Printf("  %-3s %-16s %-10s %-12s %-14s %s\n", "#", "DEVICE", "TYPE", "STATE", "GATEWAY", "MODEM")
		for i, d := range list {
			mark := " "
			if d.Interface == current {
				mark = "*"
			}
			model := d.ModemModel
			if model == "" {
				model = "-"
			}
			gw := d.Gateway
			if gw == "" {
				gw = "-"
			}
			fmt.Printf("%s %-3d %-16s %-10s %-12s %-14s %s\n", mark, i+1, d.Interface, d.Type, d.State, gw, model)
		}
		fmt.Println("  (* = current default)   a = show/hide virtual devices   q = quit")
		fmt.Print("Choice: ")
		line, err := in.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "quit", "":
			return
		case "a":
			showAll = !showAll
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(list) {
			fmt.Println("Invalid choice.")
			continue
		}
		d := list[n-1]
		if err := config.SetModemDevice(path, d.Interface, d.Gateway); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n(try: sudo aintscrape select)\n", err)
			os.Exit(1)
		}
		fmt.Printf("Default device set to %s", d.Interface)
		if d.Gateway != "" {
			fmt.Printf(" (modem IP %s)", d.Gateway)
		}
		fmt.Printf(" in %s\n", path)
		return
	}
}

// ensureAccess re-runs this command under sudo when the config exists but the
// current user cannot read/write it (it is root-only because it holds the
// modem password).
func ensureAccess(path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return
	}
	if !os.IsPermission(err) || os.Geteuid() == 0 {
		return
	}
	self, e := os.Executable()
	if e != nil {
		return
	}
	fmt.Println("Config is root-only — re-running with sudo...")
	cmd := exec.Command("sudo", append([]string{self}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
	os.Exit(0)
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

type menuItem struct {
	key, label string
	args       []string
}

func stty(args ...string) {
	c := exec.Command("stty", args...)
	c.Stdin = os.Stdin
	c.Run()
}

// readKey returns "up", "down", "enter", "quit" or the typed character.
func readKey(r *bufio.Reader) string {
	b, err := r.ReadByte()
	if err != nil {
		return "quit"
	}
	switch b {
	case '\r', '\n':
		return "enter"
	case 3, 4: // Ctrl+C, Ctrl+D
		return "quit"
	case 27:
		if r.Buffered() == 0 {
			return "quit" // bare Esc
		}
		if n, _ := r.ReadByte(); n == '[' || n == 'O' {
			c, _ := r.ReadByte()
			switch c {
			case 'A':
				return "up"
			case 'B':
				return "down"
			}
		}
		return ""
	}
	return strings.ToLower(string(rune(b)))
}

type dash struct {
	Model, Gateway, State string
	LocalIP, PublicIP     string
	ModemState            string
	Rx, Tx                uint64
	Updated               time.Time
	Loading               bool
}

func humanBytes(n uint64) string {
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1024 {
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return fmt.Sprintf("%.1f PB", f)
}

func readStat(iface, name string) uint64 {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/statistics/" + name)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return n
}

var ipv4Re = regexp.MustCompile(`inet (\d+\.\d+\.\d+\.\d+)`)

// gatherDash collects status quickly (local data) then slowly (modem probe,
// public IP). onUpdate is called after each stage.
func gatherDash(self, configPath, iface string, d *dash, mu *sync.Mutex, onUpdate func()) {
	set := func(f func()) { mu.Lock(); f(); mu.Unlock(); onUpdate() }
	set(func() {
		d.Loading = true
		d.Rx, d.Tx = readStat(iface, "rx_bytes"), readStat(iface, "tx_bytes")
		if out, err := exec.Command("ip", "-4", "addr", "show", "dev", iface).Output(); err == nil {
			if m := ipv4Re.FindStringSubmatch(string(out)); m != nil {
				d.LocalIP = m[1]
			} else {
				d.LocalIP = ""
			}
		}
	})
	run := func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	if devs, err := devices.ListAll(run); err == nil {
		devices.EnrichWithModemInfo(devs, run)
		for _, dv := range devs {
			if dv.Interface == iface {
				set(func() { d.Model, d.Gateway, d.State = dv.ModemModel, dv.Gateway, dv.State })
			}
		}
	}
	out, err := exec.Command(self, "--config", configPath, "--json", "info").Output()
	var info struct {
		PublicIP   string `json:"public_ip"`
		ModemState string `json:"modem_state"`
	}
	if err == nil {
		json.Unmarshal(out, &info)
	}
	set(func() {
		d.PublicIP, d.ModemState = info.PublicIP, info.ModemState
		d.Loading, d.Updated = false, time.Now()
	})
}

// runMainMenu is the interactive mode shown when aintscrape runs with no command
// on a terminal. Each action re-executes this binary with the matching command.
func runMainMenu(configPath string) {
	ensureAccess(configPath)
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	items := []menuItem{
		{"1", "Status", []string{"info"}},
		{"2", "Change IP", []string{"rotate"}},
		{"3", "Choose device", []string{"select"}},
		{"4", "Scrape", nil},
		{"5", "Output folder", nil},
		{"q", "Quit", nil},
	}
	descs := map[string]string{
		"5": "where scrape results are saved",
		"1": "full details",
		"2": "toggle mobile data, get a new public IP",
		"3": "pick which modem aintscrape uses",
		"4": "Meta Ad Library, any web page, more soon",
	}
	in := bufio.NewReader(os.Stdin)
	line := func(prompt string) string {
		fmt.Print(prompt)
		s, err := in.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	defer stty("sane")

	var (
		mu      sync.Mutex
		d       dash
		gen     int
		redraw  = make(chan struct{}, 16)
		keys    = make(chan string, 1)
		pending bool
	)
	refresh := func(iface string) {
		mu.Lock()
		gen++
		my := gen
		d = dash{Loading: true}
		mu.Unlock()
		go gatherDash(self, configPath, iface, &d, &mu, func() {
			mu.Lock()
			stale := my != gen
			mu.Unlock()
			if !stale {
				select {
				case redraw <- struct{}{}:
				default:
				}
			}
		})
	}

	cur := 0
	for {
		m, merr := config.PeekModem(configPath)
		if merr == nil {
			refresh(m.Interface)
		}
		var sel *menuItem
		stty("raw", "-echo")
		for sel == nil {
			mu.Lock()
			dd := d
			mu.Unlock()
			var b strings.Builder
			b.WriteString("\033[2J\033[H")
			b.WriteString("\033[1maintscrape\033[0m " + version + "\r\n\r\n")
			if merr != nil {
				b.WriteString("  not configured (run: sudo aintscrape config)\r\n")
				fmt.Fprintf(&b, "  Output     %s\r\n", config.PeekOutputDir(configPath))
			} else {
				pub := dd.PublicIP
				if pub == "" {
					pub = "unknown"
					if dd.Loading {
						pub = "checking…"
					}
				}
				orDash := func(v string) string {
					if v == "" {
						return "-"
					}
					return v
				}
				fmt.Fprintf(&b, "  Device     %s  %s\r\n", m.Interface, orDash(dd.State))
				fmt.Fprintf(&b, "  Modem      %s  %s\r\n", orDash(dd.Model), orDash(dd.ModemState))
				fmt.Fprintf(&b, "  Gateway    %s\r\n", orDash(orDefault(dd.Gateway, m.IP)))
				fmt.Fprintf(&b, "  Local IP   %s\r\n", orDash(dd.LocalIP))
				fmt.Fprintf(&b, "  Public IP  \033[1m%s\033[0m\r\n", pub)
				fmt.Fprintf(&b, "  Traffic    ↓ %s   ↑ %s\r\n", humanBytes(dd.Rx), humanBytes(dd.Tx))
				fmt.Fprintf(&b, "  Output     %s\r\n", config.PeekOutputDir(configPath))
				if !dd.Updated.IsZero() {
					fmt.Fprintf(&b, "  Updated    %s\r\n", dd.Updated.Format("15:04:05"))
				}
			}
			b.WriteString("\r\n")
			for i, it := range items {
				if i == cur {
					fmt.Fprintf(&b, "  \033[7m> %-15s\033[0m  %s\r\n", it.label, descs[it.key])
				} else {
					fmt.Fprintf(&b, "    %-15s  \033[2m%s\033[0m\r\n", it.label, descs[it.key])
				}
			}
			b.WriteString("\r\n  ↑/↓ move   Enter select   r refresh   q quit\r\n")
			fmt.Print(b.String())

			if !pending {
				pending = true
				go func() { keys <- readKey(in) }()
			}
			select {
			case <-redraw:
				continue
			case k := <-keys:
				pending = false
				switch k {
				case "up", "k":
					cur = (cur + len(items) - 1) % len(items)
				case "down", "j":
					cur = (cur + 1) % len(items)
				case "r":
					if merr == nil {
						refresh(m.Interface)
					}
				case "enter":
					sel = &items[cur]
				case "quit":
					sel = &items[len(items)-1]
				default:
					for i := range items {
						if items[i].key == k {
							cur, sel = i, &items[i]
						}
					}
				}
			}
		}
		mu.Lock()
		gen++ // drop in-flight updates
		mu.Unlock()
		stty("sane")
		fmt.Print("\033[2J\033[H")
		if sel.key == "q" {
			return
		}
		args := sel.args
		switch sel.key {
		case "4":
			args = scrapeSubmenu(in, configPath)
			if args == nil {
				continue
			}
		case "5":
			cur := config.PeekOutputDir(configPath)
			fmt.Printf("Output folder (current: %s)\n", cur)
			dir := line("New folder (Enter = keep): ")
			if dir != "" {
				dir = config.ResolveOutputDir(dir)
				if err := config.SetOutputDir(configPath, dir); err != nil {
					fmt.Printf("Could not save: %v\n", err)
				} else {
					fmt.Printf("Output folder set to %s\n", dir)
				}
				line("\nPress Enter to go back...")
			}
			continue
		}
		cmd := exec.Command(self, append([]string{"--config", configPath}, args...)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("(exited: %v)\n", err)
		}
		line("\nPress Enter to go back...")
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// selectWithArrows is the arrow-key device picker used on a terminal.
func selectWithArrows(path string, all []devices.Device, current string) {
	in := bufio.NewReader(os.Stdin)
	defer stty("sane")
	showAll := false
	cur := 0
	first := true
	for {
		var list []devices.Device
		for _, d := range all {
			if showAll || !isVirtual(d) {
				list = append(list, d)
			}
		}
		if first {
			for i, d := range list {
				if d.Interface == current {
					cur = i
				}
			}
			first = false
		}
		if cur >= len(list) {
			cur = len(list) - 1
		}
		if cur < 0 {
			cur = 0
		}
		var b strings.Builder
		b.WriteString("\033[2J\033[H")
		b.WriteString("\033[1mChoose device\033[0m\r\n\r\n")
		fmt.Fprintf(&b, "    %-16s %-10s %-12s %-14s %s\r\n", "DEVICE", "TYPE", "STATE", "GATEWAY", "MODEM")
		dash := func(v string) string {
			if v == "" {
				return "-"
			}
			return v
		}
		for i, d := range list {
			mark := " "
			if d.Interface == current {
				mark = "*"
			}
			row := fmt.Sprintf("%-16s %-10s %-12s %-14s %s", d.Interface, d.Type, d.State, dash(d.Gateway), dash(d.ModemModel))
			if i == cur {
				fmt.Fprintf(&b, "  \033[7m%s> %s\033[0m\r\n", mark, row)
			} else {
				fmt.Fprintf(&b, "  %s  %s\r\n", mark, row)
			}
		}
		toggle := "show"
		if showAll {
			toggle = "hide"
		}
		fmt.Fprintf(&b, "\r\n  * current default\r\n  ↑/↓ move   Enter select   a %s virtual   q/Esc back\r\n", toggle)
		stty("raw", "-echo")
		fmt.Print(b.String())

		switch k := readKey(in); k {
		case "up", "k":
			if len(list) > 0 {
				cur = (cur + len(list) - 1) % len(list)
			}
		case "down", "j":
			if len(list) > 0 {
				cur = (cur + 1) % len(list)
			}
		case "a":
			showAll = !showAll
		case "quit", "q":
			stty("sane")
			return
		case "enter":
			if len(list) == 0 {
				continue
			}
			stty("sane")
			fmt.Print("\033[2J\033[H")
			d := list[cur]
			if err := config.SetModemDevice(path, d.Interface, d.Gateway); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Default device set to %s", d.Interface)
			if d.Gateway != "" {
				fmt.Printf(" (modem IP %s)", d.Gateway)
			}
			fmt.Printf(" in %s\n", path)
			return
		}
	}
}

// pickOne shows a titled arrow-key list and returns the chosen index, or -1
// when the user goes back (q, Esc or Ctrl+C).
func pickOne(in *bufio.Reader, title string, labels, descs []string) int {
	defer stty("sane")
	cur := 0
	for {
		var b strings.Builder
		b.WriteString("\033[2J\033[H")
		b.WriteString("\033[1m" + title + "\033[0m\r\n\r\n")
		for i, l := range labels {
			d := ""
			if i < len(descs) {
				d = descs[i]
			}
			if i == cur {
				fmt.Fprintf(&b, "  \033[7m> %-24s\033[0m  %s\r\n", l, d)
			} else {
				fmt.Fprintf(&b, "    %-24s  \033[2m%s\033[0m\r\n", l, d)
			}
		}
		b.WriteString("\r\n  ↑/↓ move   Enter select   q/Esc back\r\n")
		stty("raw", "-echo")
		fmt.Print(b.String())
		switch k := readKey(in); k {
		case "up", "k":
			cur = (cur + len(labels) - 1) % len(labels)
		case "down", "j":
			cur = (cur + 1) % len(labels)
		case "enter":
			return cur
		case "quit", "q":
			return -1
		default:
			if n, err := strconv.Atoi(k); err == nil && n >= 1 && n <= len(labels) {
				return n - 1
			}
		}
	}
}

// scrapeSubmenu lets the user choose a scraper mode, asks the few questions
// it needs, and returns the aintscrape arguments to run (nil = go back).
// Files are saved into the configured output folder.
func scrapeSubmenu(in *bufio.Reader, configPath string) []string {
	ask := func(prompt string) string {
		fmt.Print(prompt)
		s, err := in.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	labels := []string{"Meta advertiser pages", "Any web page", "Back"}
	descs := []string{"search -> page IDs -> profile, stats, WhatsApp/phone/email/socials/website", "fetch a URL, optionally extract with CSS", ""}
	for {
		choice := pickOne(in, "Scrape", labels, descs)
		if choice < 0 || choice == 2 {
			return nil
		}
		if choice == 1 {
			fmt.Print("\033[2J\033[H")
			fmt.Println("Any web page")
			target := ask("\nURL: ")
			if target == "" {
				continue
			}
			args := []string{"scrape", "--mode", "raw", target}
			if css := ask("Extract (CSS selector, Enter = whole page): "); css != "" {
				args = append(args, "--select", css)
			}
			return args
		}

		mode, title := "meta-pages", "Meta advertiser pages"
		fmt.Print("\033[2J\033[H")
		fmt.Println(title)
		fmt.Println("Paste an Ad Library URL, or type a keyword to search.")
		query := ask("\nURL or keyword: ")
		if query == "" {
			continue
		}
		target, slug := query, "search"
		if !strings.HasPrefix(query, "http") {
			country := strings.ToUpper(ask("Country code [ALL]: "))
			if country == "" {
				country = "ALL"
			}
			status := strings.ToLower(ask("Status active/inactive/all [active]: "))
			if status != "inactive" && status != "all" {
				status = "active"
			}
			v := url.Values{}
			v.Set("active_status", status)
			v.Set("ad_type", "all")
			v.Set("country", country)
			v.Set("q", query)
			v.Set("search_type", "keyword_unordered")
			target = "https://www.facebook.com/ads/library/?" + v.Encode()
			slug = query
		}
		args := []string{"scrape", "--mode", mode}
		n := ask("How many advertiser pages to fetch [25]: ")
		if v, err := strconv.Atoi(n); err == nil && v > 0 {
			args = append(args, "--limit", n)
		}

		fl := []string{"Excel (.xlsx)", "CSV (.csv)", "JSON (.json)", "Just show on screen"}
		fd := []string{"pages, contacts + ads in one workbook", "one file per table", "everything, nested", ""}
		pick := pickOne(in, "Save results as", fl, fd)
		if pick < 0 {
			continue
		}
		fmt.Print("\033[2J\033[H")
		if pick < 3 {
			ext := []string{".xlsx", ".csv", ".json"}[pick]
			dir := config.PeekOutputDir(configPath)
			name := fmt.Sprintf("%s-%s-%s%s", mode, safeSlug(slug), time.Now().Format("20060102-150405"), ext)
			args = append(args, "--out", filepath.Join(dir, name))
		}
		return append(args, target)
	}
}

func safeSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r > 127:
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if rs := []rune(out); len(rs) > 30 {
		out = string(rs[:30])
	}
	if out == "" {
		return "search"
	}
	return out
}
