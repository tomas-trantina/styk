package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const appVersion = "3.0.0"

const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Italic    = "\033[3m"
	Underline = "\033[4m"

	// Modern 256-color palette
	Green    = "\033[38;5;84m" // Vibrant Green
	GreenAlt = "\033[38;5;120m"
	Yellow   = "\033[38;5;227m" // Soft Yellow
	Red      = "\033[38;5;203m" // Soft Red
	Cyan     = "\033[38;5;81m"  // Sky Blue
	CyanAlt  = "\033[38;5;123m"
	Magenta  = "\033[38;5;213m"
	Blue     = "\033[38;5;39m" // Deep Sky Blue
	BlueAlt  = "\033[38;5;153m"
	Gray     = "\033[38;5;248m"
	DarkGray = "\033[38;5;240m"
	White    = "\033[38;5;255m"
	Orange   = "\033[38;5;208m"
	Purple   = "\033[38;5;141m"

	Clear     = "\033[2J\033[H"
	LineClear = "\033[K"
	CursorUp  = "\033[A"
)

var (
	globalQuiet   bool
	globalJSON    bool
	globalVerbose bool
	globalDryRun  bool
	globalProfile string
	globalK       bool
	globalLevel   zstd.EncoderLevel = zstd.SpeedDefault
)

func logStep(msg string) {
	if globalQuiet {
		return
	}
	fmt.Printf("%s[%s]%s %s❯%s %s\n", DarkGray, time.Now().Format("15:04:05"), Reset, Cyan, Reset, msg)
}
func logSuccess(msg string) {
	if globalQuiet {
		return
	}
	fmt.Printf("%s[%s]%s %s✔%s %s\n", DarkGray, time.Now().Format("15:04:05"), Reset, Green, Reset, msg)
}
func logWarning(msg string) {
	if globalQuiet {
		return
	}
	fmt.Printf("%s[%s]%s %s⚠%s %s\n", DarkGray, time.Now().Format("15:04:05"), Reset, Yellow, Reset, msg)
}
func logAction(msg string) {
	if globalQuiet {
		return
	}
	fmt.Printf("\n%s🚀 %s%s%s\n", Bold+Cyan, White, msg, Reset)
}
func logInfo(k, v string) {
	if globalQuiet {
		return
	}
	fmt.Printf("  %s%-12s%s %s\n", Gray, k, Reset, v)
}
func logDetail(k, v string) {
	if globalQuiet {
		return
	}
	fmt.Printf("  %s%s:%s %s\n", Gray, k, Reset, v)
}
func logError(msg string) {
	// logError should probably always show unless we are REALLY quiet
	fmt.Fprintf(os.Stderr, "\n%s[%s]%s %s✘ %s%s%s\n\n", DarkGray, time.Now().Format("15:04:05"), Reset, Red, Bold+Red, msg, Reset)
}
func logDebug(msg string) {
	if globalVerbose {
		fmt.Printf("  %s[%s] [D]%s %s\n", DarkGray, time.Now().Format("15:04:05"), Reset, msg)
	}
}

func logLive(msg string) {
	if globalQuiet {
		return
	}
	// Truncate msg if too long for a single line
	if len(msg) > 60 {
		msg = msg[:57] + "..."
	}
	fmt.Printf("\r%s  %s %s", LineClear, Gray+"•"+Reset, msg)
}

func logLiveDone() {
	if globalQuiet {
		return
	}
	fmt.Printf("\r%s", LineClear)
}

type Spinner struct {
	stop chan struct{}
	done chan struct{}
	msg  string
}

func newSpinner(msg string) *Spinner {
	return &Spinner{
		stop: make(chan struct{}),
		done: make(chan struct{}),
		msg:  msg,
	}
}

func (s *Spinner) Start() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Printf("\r%s  %s\n", LineClear, s.msg)
				close(s.done)
				return
			default:
				fmt.Printf("\r%s %s %s", LineClear, Cyan+frames[i%len(frames)]+Reset, s.msg)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop(success bool) {
	close(s.stop)
	<-s.done
	if success {
		logSuccess(s.msg)
	} else {
		logError(s.msg + " - selhalo")
	}
}

func printBanner() {
	fmt.Printf("\n%s🚀 %sSTYK VCS %s· %s%s%s\n", Orange, Bold+White, DarkGray, CyanAlt, "v"+appVersion, Reset)
	fmt.Printf("%s%s%s\n\n", DarkGray, strings.Repeat("─", 50), Reset)
}
func drawBar(val, maxVal int, width int, color string) string {
	if maxVal <= 0 {
		return strings.Repeat(" ", width)
	}
	filled := (val * width) / maxVal
	if filled > width {
		filled = width
	}
	return color + strings.Repeat("━", filled) + DarkGray + strings.Repeat("━", width-filled) + Reset
}

func printHelp() {
	printBanner()
	type cmd struct{ name, desc, alias string }
	cmds := []cmd{
		{"new <název>", "Iniciuje nový projekt na serveru", ""},
		{"init <název>", "Inicializuje aktuální adresář", ""},
		{"add <zpráva>", "Uloží verzi (diff + zstd)", ""},
		{"snapshot <zpráva>", "Uloží plný snapshot", ""},
		{"clone <název>", "Klonuje projekt ze serveru", ""},
		{"checkout <v>", "Přepne na verzi", "co"},
		{"back", "Vrátí poslední verzi", ""},
		{"diff [v1] [v2]", "Zobrazí rozdíly", ""},
		{"log", "Historie verzí", ""},
		{"tui", "Interaktivní prohlížeč", ""},
		{"status", "Zobrazí změny", "st"},
		{"info", "Detaily projektu", ""},
		{"infoall", "Přehled všech projektů", ""},
		{"stats", "Statistiky projektu", ""},
		{"list", "Vypíše projekty", "ls"},
		{"tag <název>", "Přidá tag", ""},
		{"tags", "Vypíše tagy", ""},
		{"search <text>", "Hledá v historii", ""},
		{"export [cesta]", "Exportuje projekt", ""},
		{"import <c> <n>", "Importuje projekt", ""},
		{"mirror", "Lokální zrcadlo historie", ""},
		{"verify", "Ověří integritu", ""},
		{"doctor", "Diagnostika", ""},
		{"clean", "Smaže lokální cache", ""},
		{"size", "Velikost na serveru", ""},
		{"recent", "Nedávné aktivity", ""},
		{"rename <nový>", "Přejmenuje projekt", "mv"},
		{"copy <nový>", "Zkopíruje projekt", "cp"},
		{"lock", "Zamkne projekt", ""},
		{"unlock", "Odemkne projekt", ""},
		{"ignore <cmd>", "Správa .stykignore", ""},
		{"config", "Nastavení (interaktivní)", ""},
		{"del <název>", "Smaže projekt", "rm"},
	}

	fmt.Printf("%sPoužití:%s\n  styk %s<příkaz>%s [parametry]\n\n", Bold+White, Reset, Cyan, Reset)

	fmt.Printf("%sVlajky:%s\n", Bold+White, Reset)
	fmt.Printf("  %s--json%s, %s-q%s, %s-V%s, %s--dry-run%s, %s-k%s (vlastní komprese)\n\n", CyanAlt, Reset, CyanAlt, Reset, CyanAlt, Reset, CyanAlt, Reset, CyanAlt, Reset)

	fmt.Printf("%sPříkazy:%s\n", Bold+White, Reset)
	mid := (len(cmds) + 1) / 2
	for i := 0; i < mid; i++ {
		// Left column
		c1 := cmds[i]
		alias1 := ""
		if c1.alias != "" {
			alias1 = fmt.Sprintf(" %s(%s)%s", Gray, c1.alias, Reset)
		}
		left := fmt.Sprintf("  %s%-18s%s%s", Bold+Cyan, c1.name, Reset, alias1)

		// Right column
		right := ""
		if i+mid < len(cmds) {
			c2 := cmds[i+mid]
			alias2 := ""
			if c2.alias != "" {
				alias2 = fmt.Sprintf(" %s(%s)%s", Gray, c2.alias, Reset)
			}
			right = fmt.Sprintf("  %s%-18s%s%s", Bold+Cyan, c2.name, Reset, alias2)
		}

		// Adjust padding for descriptions
		fmt.Printf("%-50s %s\n", left, right)
		desc1 := fmt.Sprintf("    %s%s%s", Gray, c1.desc, Reset)
		desc2 := ""
		if i+mid < len(cmds) {
			desc2 = fmt.Sprintf("    %s%s%s", Gray, cmds[i+mid].desc, Reset)
		}
		fmt.Printf("%-50s %s\n", desc1, desc2)
	}
	fmt.Println()
}

type Profile struct {
	ServerIP   string `json:"server_ip"`
	Username   string `json:"username"`
	KeyPath    string `json:"key_path"`
	RemotePath string `json:"remote_path"`
	UseAgent   bool   `json:"use_agent"`
	Port       int    `json:"port"`
	KnownHosts string `json:"known_hosts"`
}

func (p Profile) Validate() error {
	if p.ServerIP == "" {
		return fmt.Errorf("chybí IP/hostname serveru")
	}
	if p.Username == "" {
		return fmt.Errorf("chybí uživatelské jméno")
	}
	if !p.UseAgent && p.KeyPath == "" {
		return fmt.Errorf("chybí cesta k SSH klíči (nebo zapni UseAgent)")
	}
	if p.RemotePath == "" {
		return fmt.Errorf("chybí vzdálená cesta")
	}
	if p.Port == 0 {
		p.Port = 22
	}
	return nil
}

type Config struct {
	ActiveProfile string             `json:"active_profile"`
	Profiles      map[string]Profile `json:"profiles"`
	DefaultAuthor string             `json:"default_author,omitempty"`
	DefaultEmail  string             `json:"default_email,omitempty"`
}

type Commit struct {
	Version  int    `json:"version"`
	Type     string `json:"type"`
	Message  string `json:"message"`
	Time     string `json:"time"`
	Size     int    `json:"size"`
	RawSize  int    `json:"raw_size,omitempty"`
	Author   string `json:"author,omitempty"`
	Email    string `json:"email,omitempty"`
	Checksum string `json:"checksum,omitempty"`
	Branch   string `json:"branch,omitempty"`
}

type Tag struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Time    string `json:"time"`
}

type ProjectMeta struct {
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Owner       string `json:"owner,omitempty"`
	Description string `json:"description,omitempty"`
}

type ProjectLock struct {
	Locked   bool   `json:"locked"`
	LockedBy string `json:"locked_by,omitempty"`
	Time     string `json:"time,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".styk", "config.json")
}

func cachePath(proj string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".styk", "cache", proj)
}

func mirrorPath(proj string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".styk", "mirror", proj)
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func formatSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatSizeInt(b int) string { return formatSize(int64(b)) }

func compressionRatio(rawSize, compressedSize int) string {
	if rawSize <= 0 {
		return ""
	}
	pct := float64(rawSize-compressedSize) / float64(rawSize) * 100
	return fmt.Sprintf("%.0f%% úspora", pct)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func getProjectName() string {
	data, err := os.ReadFile(".styk_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func setProjectName(name string) error {
	return os.WriteFile(".styk_id", []byte(name), 0644)
}

func remoteProjPath(cfg Config, proj string) string {
	p := getActiveProfile(cfg)
	return fmt.Sprintf("%s/%s", p.RemotePath, proj)
}

func getActiveProfile(cfg Config) Profile {
	if p, ok := cfg.Profiles[cfg.ActiveProfile]; ok {
		if p.Port == 0 {
			p.Port = 22
		}
		return p
	}
	if len(cfg.Profiles) > 0 {
		for _, p := range cfg.Profiles {
			if p.Port == 0 {
				p.Port = 22
			}
			return p
		}
	}
	return Profile{Port: 22}
}

func loadConfig() (Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return Config{}, fmt.Errorf("konfigurace nenalezena – spusť 'styk config'")
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("poškozená konfigurace: %w", err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	if cfg.ActiveProfile == "" && len(cfg.Profiles) > 0 {
		for k := range cfg.Profiles {
			cfg.ActiveProfile = k
			break
		}
	}
	return cfg, nil
}

func mustLoadConfig() Config {
	cfg, err := loadConfig()
	if err != nil {
		logError(err.Error())
		os.Exit(1)
	}
	return cfg
}

func saveConfig(cfg Config) error {
	if err := ensureDir(filepath.Dir(configPath())); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), data, 0600)
}

func getAuthor(cfg Config) string {
	if cfg.DefaultAuthor != "" {
		return cfg.DefaultAuthor
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("LOGNAME"); u != "" {
		return u
	}
	return "unknown"
}

func getEmail(cfg Config) string {
	if cfg.DefaultEmail != "" {
		return cfg.DefaultEmail
	}
	if e := os.Getenv("EMAIL"); e != "" {
		return e
	}
	return ""
}

var builtinIgnore = []string{
	".git/", "node_modules/", "__pycache__/", "target/",
	"dist/", "build/", ".DS_Store", "*.pyc", "*.o",
	"*.a", "*.so", "*.tmp", "*.log", "*.swp", ".styk/",
	".styk_id", ".stykignore", ".styk_cache/", ".styk_mirror/",
}

func getIgnoreRules() []string {
	data, err := os.ReadFile(".stykignore")
	if err != nil {
		return builtinIgnore
	}
	rules := strings.Split(strings.TrimSpace(string(data)), "\n")
	return append(rules, builtinIgnore...)
}

func initIgnoreFile() {
	if _, err := os.Stat(".stykignore"); err == nil {
		return
	}
	content := "# Styk – soubory k ignorování\n" + strings.Join(builtinIgnore, "\n") + "\n"
	os.WriteFile(".stykignore", []byte(content), 0644)
	logStep("Vytvořen .stykignore s výchozími pravidly")
}

func isIgnored(path string, rules []string) bool {
	if path == ".styk_id" || path == ".stykignore" ||
		strings.HasPrefix(path, ".git") || strings.HasPrefix(path, ".styk") {
		return true
	}
	path = filepath.ToSlash(path)
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}
		if strings.HasSuffix(rule, "/") {
			dir := strings.TrimSuffix(rule, "/")
			if strings.HasPrefix(path, dir+"/") || path == dir {
				return true
			}
		}
		if match, _ := filepath.Match(rule, path); match {
			return true
		}
		if match, _ := filepath.Match(rule, filepath.Base(path)); match {
			return true
		}
	}
	return false
}

func connectSSH(cfg Config) (*ssh.Client, error) {
	p := getActiveProfile(cfg)
	if p.Port == 0 {
		p.Port = 22
	}

	var authMethods []ssh.AuthMethod

	if p.UseAgent {
		if runtime.GOOS == "windows" {
		} else {
			if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
				conn, err := net.Dial("unix", sock)
				if err == nil {
					ag := agent.NewClient(conn)
					authMethods = append(authMethods, ssh.PublicKeysCallback(ag.Signers))
				}
			}
		}
	}

	if p.KeyPath != "" && !p.UseAgent {
		keyData, err := os.ReadFile(expandPath(p.KeyPath))
		if err != nil {
			return nil, fmt.Errorf("nelze číst SSH klíč '%s': %w", p.KeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("neplatný nebo šifrovaný SSH klíč: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("žádná autentizační metoda dostupná")
	}

	sshCfg := &ssh.ClientConfig{
		User:            p.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", p.ServerIP, p.Port)
	var client *ssh.Client
	var err error

	sp := newSpinner("Připojování k serveru " + addr)
	if !globalQuiet {
		sp.Start()
	}

	for attempt := 1; attempt <= 3; attempt++ {
		client, err = ssh.Dial("tcp", addr, sshCfg)
		if err == nil {
			break
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	if !globalQuiet {
		sp.Stop(err == nil)
	}

	if err != nil {
		return nil, fmt.Errorf("SSH připojení selhalo: %w", err)
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return
			}
		}
	}()

	return client, nil
}

func runCmd(client *ssh.Client, cmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run(cmd)
}

func runCmdOutput(client *ssh.Client, cmd string) ([]byte, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.Output(cmd)
}

func runCmdCombined(client *ssh.Client, cmd string) ([]byte, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.CombinedOutput(cmd)
}

func newProgressBar(size int, label string) *progressbar.ProgressBar {
	options := []progressbar.Option{
		progressbar.OptionSetDescription(fmt.Sprintf("  %s%-16s%s", Cyan, label, Reset)),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        Cyan + "━" + Reset,
			SaucerHead:    Cyan + "━" + Reset,
			SaucerPadding: DarkGray + "━" + Reset,
			BarStart:      " ",
			BarEnd:        " ",
		}),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(25),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetPredictTime(true),
	}

	if globalQuiet {
		options = append(options, progressbar.OptionSetWriter(io.Discard))
	}

	return progressbar.NewOptions(size, options...)
}

func uploadAtomic(client *ssh.Client, data []byte, remotePath, label string) error {
	if globalDryRun {
		logStep(fmt.Sprintf("[DRY-RUN] Upload %s → %s (%s)", label, remotePath, formatSizeInt(len(data))))
		return nil
	}
	tmp := remotePath + ".tmp"
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("nelze otevřít SSH session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}

	bar := newProgressBar(len(data), label)

	uploadErr := make(chan error, 1)
	go func() {
		defer stdin.Close()
		_, err := io.Copy(io.MultiWriter(stdin, bar), bytes.NewReader(data))
		uploadErr <- err
	}()

	if err := sess.Run("cat > " + tmp); err != nil {
		return fmt.Errorf("upload selhal: %w", err)
	}
	if err := <-uploadErr; err != nil {
		return err
	}
	fmt.Print("\r" + LineClear)

	return runCmd(client, fmt.Sprintf("mv '%s' '%s'", tmp, remotePath))
}

func downloadData(client *ssh.Client, remotePath string) ([]byte, error) {
	if globalDryRun {
		logStep("[DRY-RUN] Download " + remotePath)
		return nil, nil
	}
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	out, err := sess.Output("cat '" + remotePath + "'")
	if err != nil {
		return nil, fmt.Errorf("nelze stáhnout '%s': %w", remotePath, err)
	}
	return out, nil
}

func downloadDataWithProgress(client *ssh.Client, remotePath, label string) ([]byte, error) {
	if globalDryRun {
		logStep("[DRY-RUN] Download " + remotePath)
		return nil, nil
	}
	sizeOut, _ := runCmdOutput(client, "stat -c%s '"+remotePath+"' 2>/dev/null || echo 0")
	sizeStr := strings.TrimSpace(string(sizeOut))
	size, _ := strconv.Atoi(sizeStr)

	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	var bar *progressbar.ProgressBar
	if size > 0 {
		bar = newProgressBar(size, label)
	}

	if err := sess.Start("cat '" + remotePath + "'"); err != nil {
		return nil, err
	}

	if bar != nil {
		_, err = io.Copy(io.MultiWriter(&buf, bar), stdout)
		fmt.Print("\r" + LineClear)
	} else {
		_, err = io.Copy(&buf, stdout)
	}
	if err != nil {
		return nil, err
	}

	if err := sess.Wait(); err != nil {
		return nil, fmt.Errorf("nelze stáhnout '%s': %w", remotePath, err)
	}
	return buf.Bytes(), nil
}

func compressZstd(data []byte, level zstd.EncoderLevel) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(level))
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func getAdaptiveLevel(size int) zstd.EncoderLevel {
	// < 10MB: Best compression
	if size < 10*1024*1024 {
		return zstd.SpeedBestCompression
	}
	// < 100MB: Default/Balanced
	if size < 100*1024*1024 {
		return zstd.SpeedDefault
	}
	// > 100MB: Fast
	return zstd.SpeedFastest
}

func selectCompressionMenu() zstd.EncoderLevel {
	fmt.Printf("\n%s🛠  Výběr úrovně komprese:%s\n", Bold+White, Reset)
	fmt.Printf("  1. %sNejsilnější%s (nejpomalejší, nejmenší soubor)\n", Green, Reset)
	fmt.Printf("  2. %sVyvážená%s    (standardní)\n", Yellow, Reset)
	fmt.Printf("  3. %sNejrychlejší%s  (největší soubor)\n", Cyan, Reset)
	fmt.Printf("  4. %sŽádná%s        (pouze store)\n", Gray, Reset)
	fmt.Printf("\n  Vyber [1-4, výchozí 2]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	switch input {
	case "1":
		return zstd.SpeedBestCompression
	case "3":
		return zstd.SpeedFastest
	case "4":
		return zstd.SpeedFastest // zstd v Go obvykle nemá "null" kompresi přes toto API, SpeedFastest je nejblíže
	default:
		return zstd.SpeedDefault
	}
}

func decompressZstd(data []byte) ([]byte, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func createArchive(rules []string, basePath string) (int64, []byte, string, error) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	var totalSize int64

	var fileList []string
	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == basePath {
			return nil
		}
		rel, _ := filepath.Rel(basePath, path)
		if isIgnored(rel, rules) {
			return nil
		}
		fileList = append(fileList, path)
		return nil
	})

	bar := newProgressBar(len(fileList), "Archivace")

	for _, path := range fileList {
		bar.Add(1)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(basePath, path)
		logLive("Zpracovávám: " + rel)

		header, err := tar.FileInfoHeader(info, info.Name())

		if err != nil {
			return 0, nil, "", err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return 0, nil, "", err
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return 0, nil, "", err
			}
			n, err := io.Copy(tw, f)
			f.Close()
			totalSize += n
			if err != nil {
				return 0, nil, "", err
			}
		}
	}
	logLiveDone()
	if err := tw.Close(); err != nil {
		return 0, nil, "", err
	}

	fmt.Print("\r" + LineClear)

	logStep("Komprimuji (zstd)...")
	level := globalLevel
	if !globalK {
		level = getAdaptiveLevel(raw.Len())
	}
	compressed, err := compressZstd(raw.Bytes(), level)
	if err != nil {
		return 0, nil, "", err
	}
	checksum := sha256sum(compressed)
	return totalSize, compressed, checksum, nil
}

func extractArchive(data []byte, dst string) error {
	raw, err := decompressZstd(data)
	if err != nil {
		return fmt.Errorf("dekomprese selhala: %w", err)
	}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dst, header.Name)
		logLive("Extrahuji: " + header.Name)
		if header.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	logLiveDone()
	return nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return exec.Command("cp", "-a", src+"/.", dst).Run()
}

func acquireLock(client *ssh.Client, cfg Config, proj string) error {
	if globalDryRun {
		return nil
	}
	lockPath := remoteProjPath(cfg, proj) + "/lock.json"
	lockData := ProjectLock{
		Locked:   true,
		LockedBy: getAuthor(cfg),
		Time:     time.Now().Format("15:04 02.01.2006"),
		PID:      os.Getpid(),
	}
	data, _ := json.Marshal(lockData)
	out, err := runCmdCombined(client, fmt.Sprintf(
		"if [ -f '%s' ]; then echo 'LOCKED'; else echo '%s' > '%s'; fi",
		lockPath, string(data), lockPath,
	))
	if err != nil {
		return fmt.Errorf("nelze zamknout projekt: %w", err)
	}
	if strings.Contains(string(out), "LOCKED") {
		return fmt.Errorf("projekt je zamčený jiným procesem")
	}
	return nil
}

func releaseLock(client *ssh.Client, cfg Config, proj string) error {
	if globalDryRun {
		return nil
	}
	lockPath := remoteProjPath(cfg, proj) + "/lock.json"
	return runCmd(client, "rm -f '"+lockPath+"'")
}

func getHistory(client *ssh.Client, cfg Config, proj string) ([]Commit, error) {
	data, err := downloadData(client, remoteProjPath(cfg, proj)+"/history.json")
	if err != nil {
		return nil, err
	}
	var history []Commit
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("poškozená history.json: %w", err)
	}
	return history, nil
}

func saveHistory(client *ssh.Client, cfg Config, proj string, history []Commit) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return uploadAtomic(client, data, remoteProjPath(cfg, proj)+"/history.json", "Metadata")
}

func getMeta(client *ssh.Client, cfg Config, proj string) (ProjectMeta, error) {
	data, err := downloadData(client, remoteProjPath(cfg, proj)+"/meta.json")
	if err != nil {
		return ProjectMeta{Name: proj}, nil
	}
	var meta ProjectMeta
	json.Unmarshal(data, &meta)
	return meta, nil
}

func saveMeta(client *ssh.Client, cfg Config, proj string, meta ProjectMeta) error {
	data, _ := json.MarshalIndent(meta, "", "  ")
	return uploadAtomic(client, data, remoteProjPath(cfg, proj)+"/meta.json", "Meta")
}

func getTags(client *ssh.Client, cfg Config, proj string) ([]Tag, error) {
	data, err := downloadData(client, remoteProjPath(cfg, proj)+"/tags.json")
	if err != nil {
		return nil, nil
	}
	var tags []Tag
	json.Unmarshal(data, &tags)
	return tags, nil
}

func saveTags(client *ssh.Client, cfg Config, proj string, tags []Tag) error {
	data, _ := json.MarshalIndent(tags, "", "  ")
	return uploadAtomic(client, data, remoteProjPath(cfg, proj)+"/tags.json", "Tags")
}

func getLock(client *ssh.Client, cfg Config, proj string) (ProjectLock, error) {
	data, err := downloadData(client, remoteProjPath(cfg, proj)+"/lock.json")
	if err != nil {
		return ProjectLock{}, nil
	}
	var lock ProjectLock
	json.Unmarshal(data, &lock)
	return lock, nil
}

func remoteExists(client *ssh.Client, cfg Config, proj string) bool {
	err := runCmd(client, fmt.Sprintf("test -d '%s'", remoteProjPath(cfg, proj)))
	return err == nil
}

func remoteFileSize(client *ssh.Client, path string) (int64, error) {
	out, err := runCmdOutput(client, "stat -c%s '"+path+"' 2>/dev/null || echo -1")
	if err != nil {
		return -1, err
	}
	s, _ := strconv.ParseInt(strings.Fields(string(out))[0], 10, 64)
	return s, nil
}

func reconstructVersion(client *ssh.Client, cfg Config, proj string, ver int, dest string) error {
	history, err := getHistory(client, cfg, proj)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return fmt.Errorf("prázdná historie")
	}
	if ver < 1 || ver > len(history) {
		return fmt.Errorf("verze %d neexistuje (1-%d)", ver, len(history))
	}

	baseData, err := downloadDataWithProgress(client,
		fmt.Sprintf("%s/versions/v1_base.tar.zst", remoteProjPath(cfg, proj)),
		"Base v1")
	if err != nil {
		return err
	}
	if err := extractArchive(baseData, dest); err != nil {
		return fmt.Errorf("extrakce base selhala: %w", err)
	}

	for i := 1; i < ver; i++ {
		c := history[i]
		patchZstd, err := downloadDataWithProgress(client,
			fmt.Sprintf("%s/versions/v%d.patch.zst", remoteProjPath(cfg, proj), c.Version),
			fmt.Sprintf("Patch v%d", c.Version))
		if err != nil {
			return err
		}
		patchData, err := decompressZstd(patchZstd)
		if err != nil {
			return fmt.Errorf("dekomprese patche v%d: %w", c.Version, err)
		}
		patchFile := filepath.Join(os.TempDir(), fmt.Sprintf("styk_recon_%d.patch", c.Version))
		os.WriteFile(patchFile, patchData, 0644)
		defer os.Remove(patchFile)

		out, err := exec.Command("patch", "-p1", "-i", patchFile).CombinedOutput()
		if err != nil {
			return fmt.Errorf("patch v%d selhal:\n%s", c.Version, string(out))
		}
		os.Remove(patchFile)
	}
	return nil
}

func getDiffStats(diffText string) (added, deleted, modified int) {
	lines := strings.Split(diffText, "\n")
	inHunk := false
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			deleted++
		}
	}
	modified = minInt(added, deleted)
	added -= modified
	deleted -= modified
	return
}

func cmdConfigWizard() {
	reader := bufio.NewReader(os.Stdin)
	printBanner()
	fmt.Printf("%s⚙  Nastavení připojení k serveru%s\n\n", Cyan, Reset)

	ask := func(prompt, defaultVal string) string {
		if defaultVal != "" {
			fmt.Printf("  %s%s%s [%s%s%s]: ", Bold, prompt, Reset, Gray, defaultVal, Reset)
		} else {
			fmt.Printf("  %s%s%s: ", Bold, prompt, Reset)
		}
		val, _ := reader.ReadString('\n')
		val = strings.TrimSpace(val)
		if val == "" {
			return defaultVal
		}
		return val
	}

	profileName := ask("Název profilu", "default")
	cfg, _ := loadConfig()
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	existing := cfg.Profiles[profileName]
	p := Profile{
		ServerIP:   ask("IP/hostname serveru", existing.ServerIP),
		Username:   ask("SSH uživatel", def(existing.Username, "root")),
		KeyPath:    ask("SSH klíč (nebo prázdné pro agent)", def(existing.KeyPath, "~/.ssh/id_rsa")),
		RemotePath: ask("Vzdálená cesta (uložiště)", def(existing.RemotePath, "/home/styk")),
		UseAgent:   ask("Použít SSH agent? [y/N]", boolStr(existing.UseAgent)) == "y",
		Port:       atoi(ask("Port", strconv.Itoa(defInt(existing.Port, 22)))),
	}
	if p.KeyPath == "" {
		p.UseAgent = true
	}

	if err := p.Validate(); err != nil {
		logError("Neplatná konfigurace: " + err.Error())
		return
	}

	cfg.Profiles[profileName] = p
	if cfg.ActiveProfile == "" {
		cfg.ActiveProfile = profileName
	}
	if err := saveConfig(cfg); err != nil {
		logError("Nelze zapsat konfiguraci: " + err.Error())
		return
	}

	logStep("Testuji SSH připojení...")
	client, err := connectSSH(cfg)
	if err != nil {
		logWarning("Konfigurace uložena, ale připojení selhalo: " + err.Error())
		return
	}
	client.Close()
	logSuccess("Připojení OK – konfigurace uložena")
	fmt.Println()
}

func cmdConfigShow(cfg Config) {
	if globalJSON {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(data))
		return
	}
	printBanner()
	fmt.Printf("%s⚙ %sProfil: %s%s%s\n", Cyan, Bold+White, cfg.ActiveProfile, Reset, "")
	fmt.Printf("%s%s%s\n\n", DarkGray, strings.Repeat("─", 50), Reset)

	for name, p := range cfg.Profiles {
		active := ""
		if name == cfg.ActiveProfile {
			active = Green + " (aktivní)" + Reset
		}
		fmt.Printf("%s%s%s %s\n", Bold, name, Reset, active)
		fmt.Printf("  %s%s@%s:%d%s\n", Gray, p.Username, p.ServerIP, p.Port, Reset)
		fmt.Printf("  %s%s%s\n\n", Gray, p.RemotePath, Reset)
	}
	if cfg.DefaultAuthor != "" {
		logInfo("Autor", cfg.DefaultAuthor)
	}
	if cfg.DefaultEmail != "" {
		logInfo("Email", cfg.DefaultEmail)
	}
}

func cmdConfigEdit(cfg Config) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}
	cmd := exec.Command(editor, configPath())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logError("Editor selhal: " + err.Error())
	}
}

func def(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
func defInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
func boolStr(b bool) string {
	if b {
		return "y"
	}
	return "n"
}
func atoi(s string) int { i, _ := strconv.Atoi(s); return i }

func cmdNew(cfg Config, name string) {
	if strings.ContainsAny(name, " /\\") {
		logError("Název projektu nesmí obsahovat mezery ani lomítka.")
		return
	}
	logAction("Zakládám projekt: " + Bold + name + Reset)
	initIgnoreFile()

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if remoteExists(client, cfg, name) {
		logError(fmt.Sprintf("Projekt '%s' už na serveru existuje. Použij 'styk clone %s'.", name, name))
		return
	}

	if err := runCmd(client, fmt.Sprintf("mkdir -p '%s/%s/versions'", getActiveProfile(cfg).RemotePath, name)); err != nil {
		logError("Nelze vytvořit adresář na serveru: " + err.Error())
		return
	}

	rules := getIgnoreRules()
	logStep("Archivuji a kompresuji zdrojový strom...")
	rawSize, archiveData, checksum, err := createArchive(rules, ".")
	if err != nil {
		logError("Archivace selhala: " + err.Error())
		return
	}

	logStep(fmt.Sprintf("Komprese: %s → %s (%s) [sha256: %s...]",
		formatSize(rawSize), formatSizeInt(len(archiveData)), compressionRatio(int(rawSize), len(archiveData)), checksum[:16]))

	basePath := fmt.Sprintf("%s/%s/versions/v1_base.tar.zst", getActiveProfile(cfg).RemotePath, name)
	if err := uploadAtomic(client, archiveData, basePath, "Base"); err != nil {
		logError("Upload selhal: " + err.Error())
		return
	}

	now := time.Now().Format("15:04 02.01.2006")
	history := []Commit{{
		Version: 1, Type: "base", Message: "Inicializace projektu",
		Time: now, Size: len(archiveData), RawSize: int(rawSize),
		Author: getAuthor(cfg), Email: getEmail(cfg), Checksum: checksum,
		Branch: "main",
	}}
	if err := saveHistory(client, cfg, name, history); err != nil {
		logError("Nelze uložit historii: " + err.Error())
		return
	}

	meta := ProjectMeta{Name: name, CreatedAt: now, UpdatedAt: now, Owner: getAuthor(cfg)}
	_ = saveMeta(client, cfg, name, meta)

	os.RemoveAll(cachePath(name))
	_ = copyDir(".", cachePath(name))
	_ = setProjectName(name)

	fmt.Println()
	logSuccess(fmt.Sprintf("Projekt '%s' vytvořen.", name))
	logInfo("Verze:", "v1 (base)")
	logInfo("Velikost:", formatSizeInt(len(archiveData)))
	logInfo("SHA256:", checksum[:24]+"...")
	fmt.Println()
}

func cmdInit(cfg Config, name string) {
	if getProjectName() != "" {
		logError("V tomto adresáři už je inicializován projekt: " + getProjectName())
		return
	}
	cmdNew(cfg, name)
}

func cmdAdd(cfg Config, msg string, snapshot bool) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v žádném projektu. Spusť nejdříve 'styk new <název>'.")
		return
	}
	if snapshot {
		logAction("Ukládám snapshot (plná kopie)")
	} else {
		logAction("Ukládám novou verzi")
	}

	if globalK {
		globalLevel = selectCompressionMenu()
	}

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if err := acquireLock(client, cfg, proj); err != nil {
		logError(err.Error())
		return
	}
	defer releaseLock(client, cfg, proj)

	rules := getIgnoreRules()
	tempDir, _ := os.MkdirTemp("", "styk_add_*")
	defer os.RemoveAll(tempDir)

	if snapshot {
		rawSize, archiveData, checksum, err := createArchive(rules, ".")
		if err != nil {
			logError("Archivace selhala: " + err.Error())
			return
		}

		history, _ := getHistory(client, cfg, proj)
		newVer := len(history) + 1
		snapPath := fmt.Sprintf("%s/%s/versions/v%d_snapshot.tar.zst", getActiveProfile(cfg).RemotePath, proj, newVer)
		if err := uploadAtomic(client, archiveData, snapPath, fmt.Sprintf("Snapshot v%d", newVer)); err != nil {
			logError("Upload selhal: " + err.Error())
			return
		}

		history = append(history, Commit{
			Version: newVer, Type: "snapshot", Message: msg,
			Time: time.Now().Format("15:04 02.01.2006"),
			Size: len(archiveData), RawSize: int(rawSize),
			Author: getAuthor(cfg), Checksum: checksum,
		})
		if err := saveHistory(client, cfg, proj, history); err != nil {
			logError("Nelze uložit historii: " + err.Error())
			return
		}

		os.RemoveAll(cachePath(proj))
		_ = copyDir(".", cachePath(proj))
		logSuccess(fmt.Sprintf("Snapshot v%d odeslán.", newVer))
		logInfo("Velikost:", formatSizeInt(len(archiveData)))
		return
	}

	oldDir := filepath.Join(tempDir, "old")
	newDir := filepath.Join(tempDir, "new")

	spPrep := newSpinner("Připravuji diff snapshot...")
	spPrep.Start()
	if err := copyDir(cachePath(proj), oldDir); err != nil {
		spPrep.Stop(false)
		logError("Lokální cache poškozena nebo neexistuje. Spusť 'styk clone " + proj + "'.")
		return
	}
	if err := copyDir(".", newDir); err != nil {
		spPrep.Stop(false)
		logError(err.Error())
		return
	}
	_ = filepath.Walk(newDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(newDir, path)
		logLive("Kontrola: " + rel)
		if isIgnored(rel, rules) {
			return os.RemoveAll(path)
		}
		return nil
	})
	logLiveDone()

	spPrep.Stop(true)

	spDiff := newSpinner("Generuji diff...")
	spDiff.Start()
	diffCmd := exec.Command("diff", "-urN", "--exclude=.styk_id", "--exclude=.stykignore", "old", "new")
	diffCmd.Dir = tempDir
	var diffBuf bytes.Buffer
	diffCmd.Stdout = &diffBuf
	diffCmd.Run()
	spDiff.Stop(true)

	if diffBuf.Len() == 0 {
		logWarning("Žádné změny k uložení.")
		return
	}

	rawSize := diffBuf.Len()
	logStep(fmt.Sprintf("Diff velikost: %s", formatSizeInt(rawSize)))

	level := globalLevel
	if !globalK {
		level = getAdaptiveLevel(rawSize)
	}
	compressed, err := compressZstd(diffBuf.Bytes(), level)
	if err != nil {
		logError("Komprese selhala: " + err.Error())
		return
	}
	checksum := sha256sum(compressed)
	logStep(fmt.Sprintf("Po kompresi: %s (%s) [sha256: %s...]",
		formatSizeInt(len(compressed)), compressionRatio(rawSize, len(compressed)), checksum[:16]))

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}
	newVer := len(history) + 1

	patchPath := fmt.Sprintf("%s/%s/versions/v%d.patch.zst", getActiveProfile(cfg).RemotePath, proj, newVer)
	if err := uploadAtomic(client, compressed, patchPath, fmt.Sprintf("v%d patch", newVer)); err != nil {
		logError("Upload selhal: " + err.Error())
		return
	}

	history = append(history, Commit{
		Version: newVer, Type: "patch", Message: msg,
		Time: time.Now().Format("15:04 02.01.2006"),
		Size: len(compressed), RawSize: rawSize,
		Author: getAuthor(cfg), Email: getEmail(cfg), Checksum: checksum,
	})
	if err := saveHistory(client, cfg, proj, history); err != nil {
		logError("Nelze uložit historii: " + err.Error())
		return
	}

	meta, _ := getMeta(client, cfg, proj)
	meta.UpdatedAt = time.Now().Format("15:04 02.01.2006")
	_ = saveMeta(client, cfg, proj, meta)

	os.RemoveAll(cachePath(proj))
	_ = copyDir(".", cachePath(proj))

	fmt.Println()
	logSuccess(fmt.Sprintf("Verze v%d odeslána.", newVer))
	logInfo("Zpráva:", msg)
	logInfo("Patch:", formatSizeInt(len(compressed)))
	logInfo("SHA256:", checksum[:24]+"...")
	fmt.Println()
}

func cmdClone(cfg Config, proj string) {
	logAction("Klonuji projekt: " + Bold + proj + Reset)
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError("Projekt neexistuje nebo je nedostupný: " + err.Error())
		return
	}
	if len(history) == 0 {
		logError("Projekt je prázdný.")
		return
	}

	lock, _ := getLock(client, cfg, proj)
	if lock.Locked {
		logWarning(fmt.Sprintf("Projekt je zamčen uživatelem %s (%s)", lock.LockedBy, lock.Time))
	}

	logStep(fmt.Sprintf("Stahuji base verzi (%s)...", formatSizeInt(history[0].Size)))
	baseData, err := downloadDataWithProgress(client,
		fmt.Sprintf("%s/%s/versions/v1_base.tar.zst", getActiveProfile(cfg).RemotePath, proj),
		"Base v1")
	if err != nil {
		logError(err.Error())
		return
	}
	if err := extractArchive(baseData, "."); err != nil {
		logError("Extrakce base selhala: " + err.Error())
		return
	}

	for i := 1; i < len(history); i++ {
		c := history[i]
		label := fmt.Sprintf("Patch v%d", c.Version)
		if c.Type == "snapshot" {
			label = fmt.Sprintf("Snapshot v%d", c.Version)
		}
		logStep(fmt.Sprintf("Aplikuji %s: %s (%s)...", label, c.Message, formatSizeInt(c.Size)))

		var patchZstd []byte
		var err error
		if c.Type == "snapshot" {
			patchZstd, err = downloadDataWithProgress(client,
				fmt.Sprintf("%s/%s/versions/v%d_snapshot.tar.zst", getActiveProfile(cfg).RemotePath, proj, c.Version),
				label)
			if err != nil {
				logError(err.Error())
				return
			}
			if err := extractArchive(patchZstd, "."); err != nil {
				logError(fmt.Sprintf("Extrakce snapshotu v%d selhala: %s", c.Version, err))
				return
			}
			continue
		}

		patchZstd, err = downloadDataWithProgress(client,
			fmt.Sprintf("%s/%s/versions/v%d.patch.zst", getActiveProfile(cfg).RemotePath, proj, c.Version),
			label)
		if err != nil {
			logError(err.Error())
			return
		}
		patchData, err := decompressZstd(patchZstd)
		if err != nil {
			logError(fmt.Sprintf("Dekomprese patche v%d selhala: %s", c.Version, err))
			return
		}
		patchFile := filepath.Join(os.TempDir(), fmt.Sprintf("styk_clone_%d.patch", c.Version))
		os.WriteFile(patchFile, patchData, 0644)
		defer os.Remove(patchFile)

		out, err := exec.Command("patch", "-p1", "-i", patchFile).CombinedOutput()
		if err != nil {
			logError(fmt.Sprintf("Patch v%d selhal:\n%s", c.Version, string(out)))
			return
		}
		os.Remove(patchFile)
	}

	_ = setProjectName(proj)
	os.RemoveAll(cachePath(proj))
	_ = copyDir(".", cachePath(proj))

	last := history[len(history)-1]
	fmt.Println()
	logSuccess(fmt.Sprintf("Projekt '%s' naklonován (%d verzí).", proj, len(history)))
	logInfo("Aktuální:", fmt.Sprintf("v%d – %s", last.Version, last.Message))
	if last.Checksum != "" {
		logInfo("Checksum:", last.Checksum[:24]+"...")
	}
	fmt.Println()
}

func cmdAddSilent(cfg Config, msg string, snapshot bool) {
	globalQuiet = true
	defer func() { globalQuiet = false }()
	cmdAdd(cfg, msg, snapshot)
}

func cmdCloneSilent(cfg Config, proj string) {
	globalQuiet = true
	defer func() { globalQuiet = false }()
	cmdClone(cfg, proj)
}

func cmdCheckout(cfg Config, verStr string, force bool) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	ver, err := strconv.Atoi(verStr)
	if err != nil || ver < 1 {
		logError("Neplatné číslo verze.")
		return
	}

	logAction(fmt.Sprintf("Checkout verze v%d", ver))
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}
	if ver > len(history) {
		logError("Verze neexistuje.")
		return
	}

	if !force {
		status := getWorkingTreeStatus(proj)
		if status != "" {
			logWarning("Máš neuložené změny. Použij --force nebo nejdřív 'styk add'.")
			return
		}
	}

	logStep("Čistím pracovní strom...")
	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || path == "." {
			return nil
		}
		if strings.HasPrefix(path, ".styk") || path == ".styk_id" || path == ".stykignore" {
			return nil
		}
		return os.RemoveAll(path)
	})

	logStep("Rekonstrukce verze...")
	if err := reconstructVersion(client, cfg, proj, ver, "."); err != nil {
		logError(err.Error())
		return
	}

	os.RemoveAll(cachePath(proj))
	_ = copyDir(".", cachePath(proj))

	c := history[ver-1]
	logSuccess(fmt.Sprintf("Přepnuto na verzi v%d: %s", ver, c.Message))
}

func getWorkingTreeStatus(proj string) string {
	rules := getIgnoreRules()
	tempDir, _ := os.MkdirTemp("", "styk_status_*")
	defer os.RemoveAll(tempDir)
	oldDir := filepath.Join(tempDir, "old")
	newDir := filepath.Join(tempDir, "new")
	_ = copyDir(cachePath(proj), oldDir)
	_ = copyDir(".", newDir)
	_ = filepath.Walk(newDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(newDir, path)
		logLive("Kontrola: " + rel)
		if isIgnored(rel, rules) {
			return os.RemoveAll(path)
		}
		return nil
	})
	logLiveDone()

	diffCmd := exec.Command("diff", "-urN", "--exclude=.styk_id", "--exclude=.stykignore", "old", "new")
	diffCmd.Dir = tempDir
	var buf bytes.Buffer
	diffCmd.Stdout = &buf
	diffCmd.Run()
	return buf.String()
}

func cmdBack(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	logAction("Vracím poslední verzi")

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if err := acquireLock(client, cfg, proj); err != nil {
		logError(err.Error())
		return
	}
	defer releaseLock(client, cfg, proj)

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}
	if len(history) < 2 {
		logError("Nelze se vrátit přes základní verzi (v1).")
		return
	}

	last := history[len(history)-1]
	logStep(fmt.Sprintf("Anuluji verzi v%d: %s...", last.Version, last.Message))

	if last.Type == "snapshot" {
		logStep("Verze je snapshot, rekonstruuji předchozí stav...")
		_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if err != nil || path == "." {
				return nil
			}
			if strings.HasPrefix(path, ".styk") || path == ".styk_id" || path == ".stykignore" {
				return nil
			}
			return os.RemoveAll(path)
		})
		if err := reconstructVersion(client, cfg, proj, len(history)-1, "."); err != nil {
			logError(err.Error())
			return
		}
	} else {
		patchZstd, err := downloadData(client,
			fmt.Sprintf("%s/%s/versions/v%d.patch.zst", getActiveProfile(cfg).RemotePath, proj, last.Version))
		if err != nil {
			logError(err.Error())
			return
		}
		patchData, err := decompressZstd(patchZstd)
		if err != nil {
			logError("Dekomprese selhala: " + err.Error())
			return
		}
		patchFile := filepath.Join(os.TempDir(), "styk_revert.patch")
		_ = os.WriteFile(patchFile, patchData, 0644)
		defer os.Remove(patchFile)
		out, err := exec.Command("patch", "-R", "-p1", "-i", patchFile).CombinedOutput()
		if err != nil {
			logError(fmt.Sprintf("Revert selhal:\n%s", string(out)))
			return
		}
	}

	_ = runCmd(client, fmt.Sprintf("rm -f '%s/%s/versions/v%d.*'",
		getActiveProfile(cfg).RemotePath, proj, last.Version))

	history = history[:len(history)-1]
	if err := saveHistory(client, cfg, proj, history); err != nil {
		logError("Nelze uložit historii: " + err.Error())
		return
	}

	os.RemoveAll(cachePath(proj))
	_ = copyDir(".", cachePath(proj))

	prev := history[len(history)-1]
	fmt.Println()
	logSuccess(fmt.Sprintf("Vráceno do verze v%d: %s", prev.Version, prev.Message))
	fmt.Println()
}

func cmdDiff(cfg Config, args []string) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}

	tempDir, _ := os.MkdirTemp("", "styk_diff_*")
	defer os.RemoveAll(tempDir)

	var diffText string
	var label string

	switch len(args) {
	case 0:
		diffText = getWorkingTreeStatus(proj)
		label = fmt.Sprintf("Working tree vs v%d", len(history))
	case 1:
		ver, err := strconv.Atoi(args[0])
		if err != nil || ver < 1 {
			logError("Neplatná verze.")
			return
		}
		oldDir := filepath.Join(tempDir, "old")
		os.MkdirAll(oldDir, 0755)
		if err := reconstructVersion(client, cfg, proj, ver, oldDir); err != nil {
			logError(err.Error())
			return
		}
		newDir := filepath.Join(tempDir, "new")
		_ = copyDir(".", newDir)
		diffCmd := exec.Command("diff", "-urN", "--exclude=.styk_id", "--exclude=.stykignore", oldDir, newDir)
		var buf bytes.Buffer
		diffCmd.Stdout = &buf
		diffCmd.Run()
		diffText = buf.String()
		label = fmt.Sprintf("Working tree vs v%d", ver)
	case 2:
		v1, _ := strconv.Atoi(args[0])
		v2, _ := strconv.Atoi(args[1])
		if v1 < 1 || v2 < 1 || v1 > len(history) || v2 > len(history) {
			logError("Neplatná verze.")
			return
		}
		d1 := filepath.Join(tempDir, "v1")
		d2 := filepath.Join(tempDir, "v2")
		os.MkdirAll(d1, 0755)
		os.MkdirAll(d2, 0755)
		logStep(fmt.Sprintf("Rekonstrukce v%d...", v1))
		if err := reconstructVersion(client, cfg, proj, v1, d1); err != nil {
			logError(err.Error())
			return
		}
		logStep(fmt.Sprintf("Rekonstrukce v%d...", v2))
		if err := reconstructVersion(client, cfg, proj, v2, d2); err != nil {
			logError(err.Error())
			return
		}
		diffCmd := exec.Command("diff", "-urN", d1, d2)
		var buf bytes.Buffer
		diffCmd.Stdout = &buf
		diffCmd.Run()
		diffText = buf.String()
		label = fmt.Sprintf("v%d vs v%d", v1, v2)
	default:
		logError("Použití: styk diff [verze1] [verze2]")
		return
	}

	fmt.Printf("\n%s⚖ %sDiff: %s%s%s\n", Cyan, Bold+White, label, Reset, "")
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	if diffText == "" {
		logSuccess("Žádné rozdíly.")
		return
	}

	added, deleted, modified := getDiffStats(diffText)
	fmt.Printf("  %s+%d  %s-%d  %s~%d\n\n", Green, added, Red, deleted, Yellow, modified)

	for _, line := range strings.Split(diffText, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			fmt.Printf("%s%s%s\n", Green, line, Reset)
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			fmt.Printf("%s%s%s\n", Red, line, Reset)
		case strings.HasPrefix(line, "@@"):
			fmt.Printf("%s%s%s\n", Cyan, line, Reset)
		case strings.HasPrefix(line, "diff "):
			fmt.Printf("%s%s%s\n", Magenta, line, Reset)
		default:
			fmt.Println(line)
		}
	}
	fmt.Println()
}

func cmdLog(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}

	fmt.Printf("  %s· Načítám historii...%s\r", Gray, Reset)
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}

	var history []Commit
	var tags []Tag
	var hErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { history, hErr = getHistory(client, cfg, proj); wg.Done() }()
	go func() { tags, _ = getTags(client, cfg, proj); wg.Done() }()
	wg.Wait()
	client.Close()
	if hErr != nil {
		logError(hErr.Error())
		return
	}

	totalSize := 0
	for _, c := range history {
		totalSize += c.Size
	}

	fmt.Printf("\033[K\n%s📜 %sHistorie: %s%s %s(%d verzí, %s)%s\n", Cyan, Bold+White, proj, Reset, Gray, len(history), formatSizeInt(totalSize), Reset)
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	tagMap := make(map[int][]string)
	for _, t := range tags {
		tagMap[t.Version] = append(tagMap[t.Version], t.Name)
	}

	for i := len(history) - 1; i >= 0; i-- {
		c := history[i]
		typeIcon := "○"
		typeColor := Gray
		if c.Type == "base" {
			typeIcon = "◈"
			typeColor = Green
		}
		if c.Type == "snapshot" {
			typeIcon = "◉"
			typeColor = Blue
		}

		ratio := ""
		if c.RawSize > 0 && c.Type != "base" {
			ratio = fmt.Sprintf(" %s(%s)%s", Gray, compressionRatio(c.RawSize, c.Size), Reset)
		}

		tagStr := ""
		if tgs, ok := tagMap[c.Version]; ok {
			tagStr = fmt.Sprintf(" %s🏷 %s%s", Yellow, strings.Join(tgs, ", "), Reset)
		}

		author := ""
		if c.Author != "" {
			author = fmt.Sprintf(" %s• %s%s", Gray, c.Author, Reset)
		}

		fmt.Printf("%s%s%s %sv%-3d%s %s%s%s%s%s\n",
			typeColor, typeIcon, Reset, typeColor, c.Version, Reset, Bold+White, c.Message, Reset, ratio, tagStr)
		fmt.Printf("  %s%s %s· %s%s%s\n\n", Gray, c.Time, formatSizeInt(c.Size), author, Reset, "")
	}
}

func cmdStatus(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}

	diffText := getWorkingTreeStatus(proj)
	fmt.Printf("\n%s🔍 %sStatus: %s%s%s\n", Cyan, Bold+White, proj, Reset, "")
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	if diffText == "" {
		fmt.Printf("%s✔ %sPracovní strom je čistý.%s\n\n", Green, Gray, Reset)
		return
	}

	added, deleted, modified := getDiffStats(diffText)
	fmt.Printf("%sNezaznamenané změny:%s\n", Bold+White, Reset)
	if added > 0 {
		fmt.Printf("  %s+%s Přidané:  %d\n", Green, Reset, added)
	}
	if deleted > 0 {
		fmt.Printf("  %s-%s Smazané:  %d\n", Red, Reset, deleted)
	}
	if modified > 0 {
		fmt.Printf("  %s~%s Upravené: %d\n", Yellow, Reset, modified)
	}

	files := make(map[string]bool)
	for _, line := range strings.Split(diffText, "\n") {
		if strings.HasPrefix(line, "--- a/") || strings.HasPrefix(line, "+++ b/") {
			parts := strings.SplitN(line, "/", 2)
			if len(parts) == 2 {
				files[parts[1]] = true
			}
		}
	}
	if len(files) > 0 {
		fmt.Printf("\n%sZměněné soubory:%s\n", Bold+White, Reset)
		for f := range files {
			fmt.Printf("  %s%s%s\n", Gray, f, Reset)
		}
	}
	fmt.Printf("\n%s(styk add <zpráva> pro uložení)%s\n\n", DarkGray, Reset)
}

func getRemoteDiskInfo(client *ssh.Client, path string) string {
	// df -h <path> | tail -n1
	out, err := runCmdOutput(client, fmt.Sprintf("df -h '%s' | tail -n1", path))
	if err != nil {
		return "nedostupné"
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 {
		return "neznámé"
	}
	// fields: Filesystem Size Used Avail Use% MountedOn
	size := fields[1]
	used := fields[2]
	avail := fields[3]
	usage := fields[4]
	return fmt.Sprintf("%s volno z %s (%s využito, %s)", avail, size, used, usage)
}

func cmdInfo(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}
	meta, _ := getMeta(client, cfg, proj)
	tags, _ := getTags(client, cfg, proj)
	lock, _ := getLock(client, cfg, proj)

	if globalJSON {
		out := map[string]interface{}{
			"project": proj, "history": history, "meta": meta,
			"tags": tags, "lock": lock, "versions": len(history),
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return
	}

	totalSize := int64(0)
	maxVerSize := 0
	for _, c := range history {
		totalSize += int64(c.Size)
		if c.Size > maxVerSize {
			maxVerSize = c.Size
		}
	}

	fmt.Printf("\n%sℹ %sDetaily: %s%s%s\n", Cyan, Bold+White, proj, Reset, "")
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	logInfo("Vytvořen", meta.CreatedAt)
	logInfo("Upraven", meta.UpdatedAt)
	if meta.Owner != "" {
		logInfo("Vlastník", meta.Owner)
	}

	fmt.Println()
	logInfo("Verzí", strconv.Itoa(len(history)))
	logInfo("Velikost", formatSize(totalSize))
	logInfo("Disk", getRemoteDiskInfo(client, getActiveProfile(cfg).RemotePath))
	logInfo("Tagů", strconv.Itoa(len(tags)))

	statusColor := Green
	statusText := "Odemčeno"
	if lock.Locked {
		statusColor = Red
		statusText = fmt.Sprintf("ZAMČENO (%s)", lock.LockedBy)
	}
	fmt.Printf("  %s%-10s%s %s%s%s\n", Gray, "Status", Reset, statusColor, statusText, Reset)

	if len(history) > 0 {
		fmt.Printf("\n%sTrend velikostí (posledních 8):%s\n", Bold+White, Reset)
		start := maxInt(0, len(history)-8)
		for i := start; i < len(history); i++ {
			c := history[i]
			bar := drawBar(c.Size, maxVerSize, 30, Cyan)
			fmt.Printf("  v%-3d %s %s\n", c.Version, bar, formatSizeInt(c.Size))
		}

		last := history[len(history)-1]
		fmt.Printf("\n%sPoslední verze:%s\n", Bold+White, Reset)
		fmt.Printf("  %s%s%s v%d\n", Cyan, last.Message, Reset, last.Version)
		fmt.Printf("  %s%s • %s%s\n", Gray, last.Time, last.Author, Reset)
	}
	fmt.Println()
}

func cmdInfoAll(cfg Config) {
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	out, err := runCmdOutput(client, fmt.Sprintf("ls -1 '%s' 2>/dev/null", getActiveProfile(cfg).RemotePath))
	if err != nil || strings.TrimSpace(string(out)) == "" {
		logWarning("Na serveru nejsou žádné projekty.")
		return
	}

	projects := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(projects)

	fmt.Printf("\n%s🌐 %sProjekty na serveru%s\n", Cyan, Bold+White, Reset)
	logInfo("Disk", getRemoteDiskInfo(client, getActiveProfile(cfg).RemotePath))
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	current := getProjectName()
	for _, p := range projects {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		history, _ := getHistory(client, cfg, p)
		meta, _ := getMeta(client, cfg, p)
		totalSize := int64(0)
		for _, c := range history {
			totalSize += int64(c.Size)
		}

		marker := "  "
		color := White
		if p == current {
			marker = Green + "▶ " + Reset
			color = Green
		}

		fmt.Printf("%s%s%s\n", marker, Bold+color, p, Reset)
		fmt.Printf("    %s%d verzí • %s%s\n", Gray, len(history), formatSize(totalSize), Reset)
		fmt.Printf("    %sUpraveno: %s%s\n\n", DarkGray, meta.UpdatedAt, Reset)
	}
}

func cmdStats(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}

	var files, dirs, lines int64
	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || isIgnored(path, getIgnoreRules()) {
			return nil
		}
		if info.IsDir() {
			dirs++
			return nil
		}
		files++
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lines++
		}
		return nil
	})

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()
	history, _ := getHistory(client, cfg, proj)

	fmt.Printf("\n%s📊 %sStatistiky: %s%s%s\n", Cyan, Bold+White, proj, Reset, "")
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	logInfo("Souborů", strconv.FormatInt(files, 10))
	logInfo("Adresářů", strconv.FormatInt(dirs, 10))
	logInfo("Řádků", strconv.FormatInt(lines, 10))
	logInfo("Verzí", strconv.Itoa(len(history)))

	if len(history) > 0 {
		logInfo("Hustota", fmt.Sprintf("%.1f řádků/verze", float64(lines)/float64(len(history))))
	}

	fmt.Printf("\n%sSložení projektu:%s\n", Bold+White, Reset)
	total := files + dirs
	if total > 0 {
		fBar := drawBar(int(files), int(total), 40, Blue)
		dBar := drawBar(int(dirs), int(total), 40, Yellow)
		fmt.Printf("  Soubory   %s %d%%\n", fBar, (files*100)/total)
		fmt.Printf("  Složky    %s %d%%\n", dBar, (dirs*100)/total)
	}
	fmt.Println()
}

func cmdList(cfg Config) {
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	out, err := runCmdOutput(client, fmt.Sprintf("ls -1 '%s' 2>/dev/null", getActiveProfile(cfg).RemotePath))
	if err != nil || strings.TrimSpace(string(out)) == "" {
		logWarning("Na serveru nejsou žádné projekty.")
		return
	}

	current := getProjectName()
	projects := strings.Split(strings.TrimSpace(string(out)), "\n")
	fmt.Printf("\n%s▸ Projekty na serveru %s(%s)%s\n\n", Cyan, Gray, getActiveProfile(cfg).ServerIP, Reset)
	for _, p := range projects {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == current {
			fmt.Printf("  %s► %-20s%s %s(aktuální)%s\n", Green, p, Reset, Gray, Reset)
		} else {
			fmt.Printf("  %s  %s%s\n", Gray, p, Reset)
		}
	}
	fmt.Println()
}

func cmdTag(cfg Config, name string) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}
	if len(history) == 0 {
		logError("Prázdná historie.")
		return
	}

	tags, _ := getTags(client, cfg, proj)
	lastVer := history[len(history)-1].Version

	for _, t := range tags {
		if t.Name == name {
			logError("Tag s tímto názvem už existuje.")
			return
		}
	}

	tags = append(tags, Tag{Name: name, Version: lastVer, Time: time.Now().Format("15:04 02.01.2006")})
	if err := saveTags(client, cfg, proj, tags); err != nil {
		logError("Nelze uložit tagy: " + err.Error())
		return
	}
	logSuccess(fmt.Sprintf("Tag '%s' přidán k verzi v%d.", name, lastVer))
}

func cmdTags(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	tags, err := getTags(client, cfg, proj)
	if err != nil || len(tags) == 0 {
		logWarning("Žádné tagy.")
		return
	}

	fmt.Printf("\n%s▸ Tagy projektu %s%s%s\n\n", Cyan, Bold, proj, Reset)
	for _, t := range tags {
		fmt.Printf("  %s🏷 %s%s → v%d (%s)\n", Yellow, t.Name, Reset, t.Version, t.Time)
	}
	fmt.Println()
}

func cmdSearch(cfg Config, query string) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}

	query = strings.ToLower(query)
	found := 0
	fmt.Printf("\n%s▸ Výsledky hledání '%s%s%s':\n\n", Cyan, Bold, query, Reset)
	for _, c := range history {
		if strings.Contains(strings.ToLower(c.Message), query) ||
			strings.Contains(strings.ToLower(c.Author), query) {
			found++
			fmt.Printf("  %sv%-3d%s %s%s%s %s%s%s\n",
				Cyan, c.Version, Reset, Gray, c.Time, Reset, Bold, c.Message, Reset)
			if c.Author != "" {
				fmt.Printf("       %sAutor: %s%s\n", Gray, c.Author, Reset)
			}
		}
	}
	if found == 0 {
		logWarning("Žádné výsledky.")
	} else {
		fmt.Printf("\n  Nalezeno %d výsledků.\n", found)
	}
	fmt.Println()
}

func cmdExport(cfg Config, outPath string) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	if outPath == "" {
		outPath = proj + "_export.tar.zst"
	}

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}

	logAction("Exportuji projekt: " + proj)
	logStep("Stahuji historii a verze...")

	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	tw := tar.NewWriter(zw)

	histData, _ := json.MarshalIndent(history, "", "  ")
	_ = tw.WriteHeader(&tar.Header{Name: "history.json", Size: int64(len(histData)), Mode: 0644, ModTime: time.Now()})
	_, _ = tw.Write(histData)

	meta, _ := getMeta(client, cfg, proj)
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	_ = tw.WriteHeader(&tar.Header{Name: "meta.json", Size: int64(len(metaData)), Mode: 0644, ModTime: time.Now()})
	_, _ = tw.Write(metaData)

	tags, _ := getTags(client, cfg, proj)
	if len(tags) > 0 {
		tagsData, _ := json.MarshalIndent(tags, "", "  ")
		_ = tw.WriteHeader(&tar.Header{Name: "tags.json", Size: int64(len(tagsData)), Mode: 0644, ModTime: time.Now()})
		_, _ = tw.Write(tagsData)
	}

	for _, c := range history {
		var remoteFile string
		var localName string
		if c.Version == 1 {
			remoteFile = fmt.Sprintf("%s/versions/v1_base.tar.zst", remoteProjPath(cfg, proj))
			localName = "versions/v1_base.tar.zst"
		} else if c.Type == "snapshot" {
			remoteFile = fmt.Sprintf("%s/versions/v%d_snapshot.tar.zst", remoteProjPath(cfg, proj), c.Version)
			localName = fmt.Sprintf("versions/v%d_snapshot.tar.zst", c.Version)
		} else {
			remoteFile = fmt.Sprintf("%s/versions/v%d.patch.zst", remoteProjPath(cfg, proj), c.Version)
			localName = fmt.Sprintf("versions/v%d.patch.zst", c.Version)
		}
		data, err := downloadDataWithProgress(client, remoteFile, fmt.Sprintf("v%d", c.Version))
		if err != nil {
			logError(err.Error())
			return
		}
		_ = tw.WriteHeader(&tar.Header{Name: localName, Size: int64(len(data)), Mode: 0644, ModTime: time.Now()})
		_, _ = tw.Write(data)
	}

	_ = tw.Close()
	_ = zw.Close()
	os.WriteFile(outPath, buf.Bytes(), 0644)
	logSuccess(fmt.Sprintf("Export uložen: %s (%s)", outPath, formatSizeInt(buf.Len())))
}

func cmdImport(cfg Config, srcPath, name string) {
	if strings.ContainsAny(name, " /\\") {
		logError("Název projektu nesmí obsahovat mezery ani lomítka.")
		return
	}
	logAction("Importuji: " + srcPath + " jako " + name)

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if remoteExists(client, cfg, name) {
		logError("Projekt s tímto názvem už existuje.")
		return
	}

	if err := runCmd(client, fmt.Sprintf("mkdir -p '%s/%s/versions'", getActiveProfile(cfg).RemotePath, name)); err != nil {
		logError("Nelze vytvořit adresář: " + err.Error())
		return
	}

	rules := getIgnoreRules()
	rawSize, archiveData, checksum, err := createArchive(rules, srcPath)
	if err != nil {
		logError("Archivace selhala: " + err.Error())
		return
	}

	basePath := fmt.Sprintf("%s/%s/versions/v1_base.tar.zst", getActiveProfile(cfg).RemotePath, name)
	if err := uploadAtomic(client, archiveData, basePath, "Base"); err != nil {
		logError("Upload selhal: " + err.Error())
		return
	}

	now := time.Now().Format("15:04 02.01.2006")
	history := []Commit{{
		Version: 1, Type: "base", Message: "Import z " + srcPath,
		Time: now, Size: len(archiveData), RawSize: int(rawSize),
		Author: getAuthor(cfg), Checksum: checksum, Branch: "main",
	}}
	_ = saveHistory(client, cfg, name, history)
	_ = saveMeta(client, cfg, name, ProjectMeta{Name: name, CreatedAt: now, UpdatedAt: now, Owner: getAuthor(cfg)})
	logSuccess(fmt.Sprintf("Projekt '%s' importován.", name))
}

func cmdMirror(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	logAction("Zrcadlení projektu: " + proj)

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}

	mirrorDir := mirrorPath(proj)
	os.RemoveAll(mirrorDir)
	os.MkdirAll(mirrorDir+"/versions", 0755)

	histData, _ := json.MarshalIndent(history, "", "  ")
	os.WriteFile(mirrorDir+"/history.json", histData, 0644)
	meta, _ := getMeta(client, cfg, proj)
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(mirrorDir+"/meta.json", metaData, 0644)
	tags, _ := getTags(client, cfg, proj)
	if len(tags) > 0 {
		tagsData, _ := json.MarshalIndent(tags, "", "  ")
		os.WriteFile(mirrorDir+"/tags.json", tagsData, 0644)
	}

	for _, c := range history {
		var remoteFile, localFile string
		if c.Version == 1 {
			remoteFile = fmt.Sprintf("%s/versions/v1_base.tar.zst", remoteProjPath(cfg, proj))
			localFile = mirrorDir + "/versions/v1_base.tar.zst"
		} else if c.Type == "snapshot" {
			remoteFile = fmt.Sprintf("%s/versions/v%d_snapshot.tar.zst", remoteProjPath(cfg, proj), c.Version)
			localFile = fmt.Sprintf("%s/versions/v%d_snapshot.tar.zst", mirrorDir, c.Version)
		} else {
			remoteFile = fmt.Sprintf("%s/versions/v%d.patch.zst", remoteProjPath(cfg, proj), c.Version)
			localFile = fmt.Sprintf("%s/versions/v%d.patch.zst", mirrorDir, c.Version)
		}
		data, err := downloadDataWithProgress(client, remoteFile, fmt.Sprintf("v%d", c.Version))
		if err != nil {
			logError(err.Error())
			return
		}
		os.WriteFile(localFile, data, 0644)
	}
	logSuccess("Zrcadlení dokončeno v: " + mirrorDir)
}

func cmdVerify(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	logAction("Ověřuji integritu: " + proj)

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	history, err := getHistory(client, cfg, proj)
	if err != nil {
		logError(err.Error())
		return
	}

	failed := 0
	for _, c := range history {
		var remoteFile string
		if c.Version == 1 {
			remoteFile = fmt.Sprintf("%s/versions/v1_base.tar.zst", remoteProjPath(cfg, proj))
		} else if c.Type == "snapshot" {
			remoteFile = fmt.Sprintf("%s/versions/v%d_snapshot.tar.zst", remoteProjPath(cfg, proj), c.Version)
		} else {
			remoteFile = fmt.Sprintf("%s/versions/v%d.patch.zst", remoteProjPath(cfg, proj), c.Version)
		}
		data, err := downloadData(client, remoteFile)
		if err != nil {
			logError(fmt.Sprintf("v%d: nelze stáhnout: %v", c.Version, err))
			failed++
			continue
		}
		checksum := sha256sum(data)
		if c.Checksum != "" && c.Checksum != checksum {
			logError(fmt.Sprintf("v%d: checksum nesedí! očekáváno %s, získáno %s", c.Version, c.Checksum, checksum))
			failed++
			continue
		}
		logSuccess(fmt.Sprintf("v%d: OK (%s)", c.Version, checksum[:16]))
	}
	if failed == 0 {
		logSuccess("Všechny verze jsou v pořádku.")
	} else {
		logWarning(fmt.Sprintf("%d verzí selhalo.", failed))
	}
}

func cmdDoctor(cfg Config) {
	printBanner()
	logAction("Diagnostika prostředí")

	checks := []struct {
		name string
		ok   bool
		desc string
	}{
		{"SSH klient", commandExists("ssh"), "ssh nenalezen – nainstaluj OpenSSH"},
		{"diff", commandExists("diff"), "diff nenalezen – nainstaluj diffutils"},
		{"patch", commandExists("patch"), "patch nenalezen – nainstaluj patch"},
		{"Konfigurace", false, ""},
		{"SSH připojení", false, ""},
		{"Server path", false, ""},
	}

	cfgLoaded := false
	if _, err := loadConfig(); err == nil {
		checks[3].ok = true
		cfgLoaded = true
	} else {
		checks[3].desc = err.Error()
	}

	if cfgLoaded {
		cfg, _ := loadConfig()
		client, err := connectSSH(cfg)
		if err == nil {
			checks[4].ok = true
			p := getActiveProfile(cfg)
			if err := runCmd(client, fmt.Sprintf("test -d '%s' && test -w '%s'", p.RemotePath, p.RemotePath)); err == nil {
				checks[5].ok = true
			} else {
				checks[5].desc = "Vzdálená cesta neexistuje nebo není zapisovatelná"
			}
			client.Close()
		} else {
			checks[4].desc = err.Error()
		}
	} else {
		checks[4].desc = "Nelze testovat bez konfigurace"
		checks[5].desc = "Nelze testovat bez konfigurace"
	}

	for _, c := range checks {
		if c.ok {
			fmt.Printf("  %s✔%s %-20s %sOK%s\n", Green, Reset, c.name, Green, Reset)
		} else {
			fmt.Printf("  %s✖%s %-20s %s%s%s\n", Red, Reset, c.name, Red, c.desc, Reset)
		}
	}
	fmt.Println()
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func cmdClean(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logAction("Čištění všech cache")
		os.RemoveAll(filepath.Join(os.TempDir(), "styk_*"))
		home, _ := os.UserHomeDir()
		os.RemoveAll(filepath.Join(home, ".styk", "cache"))
		logSuccess("Cache vyčištěna.")
		return
	}
	logAction("Čištění cache: " + proj)
	os.RemoveAll(cachePath(proj))
	logSuccess("Cache projektu vyčištěna.")
}

func cmdSize(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	out, err := runCmdOutput(client, fmt.Sprintf("du -sb '%s' 2>/dev/null || echo 0", remoteProjPath(cfg, proj)))
	if err != nil {
		logError(err.Error())
		return
	}
	size, _ := strconv.ParseInt(strings.Fields(string(out))[0], 10, 64)
	fmt.Printf("\n%s▸ Velikost na serveru: %s%s%s\n\n", Cyan, Bold, proj, Reset)
	logInfo("Celkem", formatSize(size))
	fmt.Println()
}

func cmdRecent(cfg Config) {
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	out, err := runCmdOutput(client, fmt.Sprintf("ls -1 '%s' 2>/dev/null", getActiveProfile(cfg).RemotePath))
	if err != nil {
		logWarning("Nelze načíst projekty.")
		return
	}

	projects := strings.Split(strings.TrimSpace(string(out)), "\n")
	type recentItem struct {
		proj string
		ver  int
		msg  string
		time string
	}
	var items []recentItem

	for _, p := range projects {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		history, err := getHistory(client, cfg, p)
		if err != nil || len(history) == 0 {
			continue
		}
		last := history[len(history)-1]
		items = append(items, recentItem{p, last.Version, last.Message, last.Time})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].time > items[j].time
	})

	fmt.Printf("\n%s🕒 %sNedávná aktivita%s\n", Cyan, Bold+White, Reset)
	fmt.Printf("%s%s%s\n", DarkGray, strings.Repeat("─", 50), Reset)

	limit := minInt(10, len(items))
	for _, it := range items[:limit] {
		fmt.Printf("%s%-12s%s %sv%d%s\n", Bold+White, it.proj, Reset, Cyan, it.ver, Reset)
		fmt.Printf("  %s%s • %s%s\n\n", Gray, it.time, it.msg, Reset)
	}

	if len(items) == 0 {
		fmt.Printf("  %sŽádná aktivita.%s\n", Gray, Reset)
	}
}

func cmdRename(cfg Config, oldName, newName string) {
	if strings.ContainsAny(newName, " /\\") {
		logError("Název nesmí obsahovat mezery ani lomítka.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if !remoteExists(client, cfg, oldName) {
		logError("Projekt neexistuje.")
		return
	}
	if remoteExists(client, cfg, newName) {
		logError("Cílový název už existuje.")
		return
	}

	logAction(fmt.Sprintf("Přejmenovávám %s → %s", oldName, newName))
	if err := runCmd(client, fmt.Sprintf("mv '%s/%s' '%s/%s'",
		getActiveProfile(cfg).RemotePath, oldName, getActiveProfile(cfg).RemotePath, newName)); err != nil {
		logError("Přejmenování selhalo: " + err.Error())
		return
	}

	if getProjectName() == oldName {
		_ = setProjectName(newName)
		os.Rename(cachePath(oldName), cachePath(newName))
	}
	logSuccess(fmt.Sprintf("Projekt přejmenován na '%s'.", newName))
}

func cmdCopy(cfg Config, srcName, dstName string) {
	if strings.ContainsAny(dstName, " /\\") {
		logError("Název nesmí obsahovat mezery ani lomítka.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if !remoteExists(client, cfg, srcName) {
		logError("Zdrojový projekt neexistuje.")
		return
	}
	if remoteExists(client, cfg, dstName) {
		logError("Cílový projekt už existuje.")
		return
	}

	logAction(fmt.Sprintf("Kopíruji %s → %s", srcName, dstName))
	if err := runCmd(client, fmt.Sprintf("cp -a '%s/%s' '%s/%s'",
		getActiveProfile(cfg).RemotePath, srcName, getActiveProfile(cfg).RemotePath, dstName)); err != nil {
		logError("Kopírování selhalo: " + err.Error())
		return
	}
	logSuccess(fmt.Sprintf("Projekt zkopírován jako '%s'.", dstName))
}

func cmdLock(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if err := acquireLock(client, cfg, proj); err != nil {
		logError(err.Error())
		return
	}
	logSuccess("Projekt zamčen.")
}

func cmdUnlock(cfg Config) {
	proj := getProjectName()
	if proj == "" {
		logError("Nejsi v projektu.")
		return
	}
	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	lock, _ := getLock(client, cfg, proj)
	if !lock.Locked {
		logWarning("Projekt není zamčen.")
		return
	}
	if err := releaseLock(client, cfg, proj); err != nil {
		logError(err.Error())
		return
	}
	logSuccess("Projekt odemčen.")
}

func cmdIgnore(cfg Config, subcmd string, args []string) {
	switch subcmd {
	case "add":
		if len(args) == 0 {
			logError("Použití: styk ignore add <vzor>")
			return
		}
		pattern := strings.Join(args, " ")
		f, err := os.OpenFile(".stykignore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logError(err.Error())
			return
		}
		fmt.Fprintf(f, "\n%s\n", pattern)
		f.Close()
		logSuccess("Přidáno do .stykignore: " + pattern)
	case "list", "ls":
		data, err := os.ReadFile(".stykignore")
		if err != nil {
			logWarning(".stykignore neexistuje.")
			return
		}
		fmt.Printf("\n%s▸ .stykignore%s\n\n", Cyan, Reset)
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Printf("  %s·%s %s\n", Gray, Reset, line)
		}
		fmt.Println()
	case "rm", "remove":
		if len(args) == 0 {
			logError("Použití: styk ignore rm <vzor>")
			return
		}
		pattern := strings.Join(args, " ")
		data, err := os.ReadFile(".stykignore")
		if err != nil {
			logError(err.Error())
			return
		}
		lines := strings.Split(string(data), "\n")
		var out []string
		removed := false
		for _, line := range lines {
			if strings.TrimSpace(line) == pattern {
				removed = true
				continue
			}
			out = append(out, line)
		}
		if !removed {
			logWarning("Vzor nenalezen.")
			return
		}
		os.WriteFile(".stykignore", []byte(strings.Join(out, "\n")), 0644)
		logSuccess("Odebráno z .stykignore: " + pattern)
	default:
		logError("Použití: styk ignore <add/list/rm> [parametry]")
	}
}

func cmdDel(cfg Config, proj string) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n  %s⚠  POZOR! Nenávratné smazání projektu: %s%s%s%s\n",
		Red, Bold, proj, Reset, "")
	fmt.Printf("  %sPokud souhlasíte, napište název projektu '%s': %s", Red, proj, Reset)

	confirm, _ := reader.ReadString('\n')
	if strings.TrimSpace(confirm) != proj {
		logWarning("Operace zrušena.")
		return
	}

	client, err := connectSSH(cfg)
	if err != nil {
		logError(err.Error())
		return
	}
	defer client.Close()

	if err := runCmd(client, fmt.Sprintf("rm -rf '%s/%s'", getActiveProfile(cfg).RemotePath, proj)); err != nil {
		logError("Mazání na serveru selhalo: " + err.Error())
		return
	}

	os.RemoveAll(cachePath(proj))
	if getProjectName() == proj {
		os.Remove(".styk_id")
	}
	fmt.Println()
	logSuccess(fmt.Sprintf("Projekt '%s' byl smazán.", proj))
	fmt.Println()
}

func cmdTUI(cfg Config) {
	proj := getProjectName()
	if err := runTUI(cfg, proj); err != nil {
		logError("TUI selhalo: " + err.Error())
	}
}

func main() {
	args := os.Args[1:]
	var cmdArgs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			globalJSON = true
		case "--quiet", "-q":
			globalQuiet = true
		case "--verbose", "-V":
			globalVerbose = true
		case "--dry-run":
			globalDryRun = true
		case "-k":
			globalK = true
		case "--profile":
			if i+1 < len(args) {
				globalProfile = args[i+1]
				i++
			}
		default:
			cmdArgs = append(cmdArgs, args[i])
		}
	}

	if len(cmdArgs) < 1 {
		printHelp()
		return
	}

	cmd := cmdArgs[0]

	switch cmd {
	case "config":
		if len(cmdArgs) > 1 && cmdArgs[1] == "show" {
			cfg := mustLoadConfig()
			cmdConfigShow(cfg)
			return
		}
		if len(cmdArgs) > 1 && cmdArgs[1] == "edit" {
			cfg := mustLoadConfig()
			cmdConfigEdit(cfg)
			return
		}
		cmdConfigWizard()
		return
	case "--version", "-v":
		fmt.Printf("styk v%s\n", appVersion)
		return
	case "--help", "-h":
		printHelp()
		return
	}

	cfg := mustLoadConfig()

	if globalProfile != "" {
		if _, ok := cfg.Profiles[globalProfile]; !ok {
			logError("Profil '" + globalProfile + "' neexistuje.")
			os.Exit(1)
		}
		cfg.ActiveProfile = globalProfile
	}

	switch cmd {
	case "new":
		if len(cmdArgs) < 2 {
			logError("Použití: styk new <název>")
			return
		}
		cmdNew(cfg, cmdArgs[1])

	case "init":
		if len(cmdArgs) < 2 {
			logError("Použití: styk init <název>")
			return
		}
		cmdInit(cfg, cmdArgs[1])

	case "add":
		if len(cmdArgs) < 2 {
			logError("Použití: styk add <zpráva>")
			return
		}
		cmdAdd(cfg, strings.Join(cmdArgs[1:], " "), false)

	case "snapshot":
		if len(cmdArgs) < 2 {
			logError("Použití: styk snapshot <zpráva>")
			return
		}
		cmdAdd(cfg, strings.Join(cmdArgs[1:], " "), true)

	case "clone":
		if len(cmdArgs) < 2 {
			logError("Použití: styk clone <název>")
			return
		}
		cmdClone(cfg, cmdArgs[1])

	case "checkout", "co":
		force := false
		var verStr string
		for _, a := range cmdArgs[1:] {
			if a == "--force" || a == "-f" {
				force = true
				continue
			}
			if verStr == "" {
				verStr = a
			}
		}
		if verStr == "" {
			logError("Použití: styk checkout <verze> [--force]")
			return
		}
		cmdCheckout(cfg, verStr, force)

	case "back":
		cmdBack(cfg)

	case "diff":
		cmdDiff(cfg, cmdArgs[1:])

	case "log":
		cmdLog(cfg)

	case "tui":
		cmdTUI(cfg)

	case "status", "st":
		cmdStatus(cfg)

	case "info":
		cmdInfo(cfg)

	case "infoall":
		cmdInfoAll(cfg)

	case "stats":
		cmdStats(cfg)

	case "list", "ls":
		cmdList(cfg)

	case "tag":
		if len(cmdArgs) < 2 {
			logError("Použití: styk tag <název>")
			return
		}
		cmdTag(cfg, cmdArgs[1])

	case "tags":
		cmdTags(cfg)

	case "search":
		if len(cmdArgs) < 2 {
			logError("Použití: styk search <dotaz>")
			return
		}
		cmdSearch(cfg, strings.Join(cmdArgs[1:], " "))

	case "export":
		outPath := ""
		if len(cmdArgs) > 1 {
			outPath = cmdArgs[1]
		}
		cmdExport(cfg, outPath)

	case "import":
		if len(cmdArgs) < 3 {
			logError("Použití: styk import <cesta> <název>")
			return
		}
		cmdImport(cfg, cmdArgs[1], cmdArgs[2])

	case "mirror":
		cmdMirror(cfg)

	case "verify":
		cmdVerify(cfg)

	case "doctor":
		cmdDoctor(cfg)

	case "clean":
		cmdClean(cfg)

	case "size":
		cmdSize(cfg)

	case "recent":
		cmdRecent(cfg)

	case "rename", "mv":
		if len(cmdArgs) < 2 {
			logError("Použití: styk rename <nový_název>")
			return
		}
		cmdRename(cfg, getProjectName(), cmdArgs[1])

	case "copy", "cp":
		if len(cmdArgs) < 2 {
			logError("Použití: styk copy <nový_název>")
			return
		}
		cmdCopy(cfg, getProjectName(), cmdArgs[1])

	case "lock":
		cmdLock(cfg)

	case "unlock":
		cmdUnlock(cfg)

	case "ignore":
		if len(cmdArgs) < 2 {
			logError("Použití: styk ignore <add/list/rm> [parametry]")
			return
		}
		cmdIgnore(cfg, cmdArgs[1], cmdArgs[2:])

	case "del", "delete", "rm":
		if len(cmdArgs) < 2 {
			logError("Použití: styk del <název>")
			return
		}
		cmdDel(cfg, cmdArgs[1])

	default:
		logError(fmt.Sprintf("Neznámý příkaz '%s'. Spusť 'styk' pro nápovědu.", cmd))
		os.Exit(1)
	}
}
