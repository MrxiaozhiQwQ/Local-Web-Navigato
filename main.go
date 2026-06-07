package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	stdfs "io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	host          = "0.0.0.0"
	maxFailCount  = 2
	historyLimit  = 200
	tcpTimeout    = 250 * time.Millisecond
	httpTimeout   = 1200 * time.Millisecond
	readBodyLimit = 256 * 1024
)

var (
	appPort     = envInt("PORT", 3210)
	commonPorts = []int{
		80, 81, 88, 3000, 4000, 4173, 5000, 5173, 5601, 5678, 7001, 7002,
		7080, 7443, 7547, 7681, 7860, 8000, 8001, 8008, 8010, 8080, 8081, 8088,
		8090, 8091, 8181, 8200, 8443, 8501, 8888, 9000, 9001, 9090, 9200, 9443,
		9999, 10000, 15672,
	}
	titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	iconPattern  = regexp.MustCompile(`(?is)<link\b[^>]*rel=["'][^"']*(?:icon|shortcut icon|apple-touch-icon)[^"']*["'][^>]*href=["']([^"']+)["'][^>]*>`)
	hostPattern  = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	defaultIcon  = "data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Crect width='64' height='64' rx='14' fill='%23132238'/%3E%3Cpath d='M18 22.5c0-2.5 2-4.5 4.5-4.5h19c2.5 0 4.5 2 4.5 4.5v13c0 2.5-2 4.5-4.5 4.5h-8.5l-5 6.5c-.7.8-2 .3-2-.8V40h-3.5c-2.5 0-4.5-2-4.5-4.5v-13Z' fill='%2368d5ff'/%3E%3Ccircle cx='25' cy='29' r='2.2' fill='%23132238'/%3E%3Ccircle cx='32' cy='29' r='2.2' fill='%23132238'/%3E%3Ccircle cx='39' cy='29' r='2.2' fill='%23132238'/%3E%3C/svg%3E"
)

//go:embed public/*
var embeddedAssets embed.FS

type Site struct {
	ID           string `json:"id"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Scheme       string `json:"scheme"`
	URL          string `json:"url"`
	Title        string `json:"title"`
	IconURL      string `json:"iconUrl"`
	StatusCode   int    `json:"statusCode"`
	ServerHeader string `json:"serverHeader"`
	ContentType  string `json:"contentType"`
	FirstSeenAt  string `json:"firstSeenAt"`
	LastSeenAt   string `json:"lastSeenAt"`
	FailCount    int    `json:"failCount"`
	Source       string `json:"source"`
}

type StoreFile struct {
	Sites []*Site `json:"sites"`
}

type AppSettings struct {
	Target string `json:"target"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	sites map[string]*Site
}

type Progress struct {
	Phase      string `json:"phase"`
	Scanned    int64  `json:"scanned"`
	Total      int64  `json:"total"`
	Discovered int64  `json:"discovered"`
}

type Snapshot struct {
	TargetIP           string   `json:"targetIp"`
	Target             string   `json:"target"`
	Running            bool     `json:"running"`
	LastScanStartedAt  string   `json:"lastScanStartedAt"`
	LastScanFinishedAt string   `json:"lastScanFinishedAt"`
	Progress           Progress `json:"progress"`
	Sites              []*Site  `json:"sites"`
}

type Scanner struct {
	store        *Store
	mu           sync.RWMutex
	running      bool
	targetIP     string
	started      string
	finished     string
	progress     Progress
	clients      map[chan sseMessage]struct{}
	cancel       context.CancelFunc
	restart      bool
	settingsPath string
	selfTargets  map[string]struct{}
}

type sseMessage struct {
	Event string
	Data  any
}

type probeResult struct {
	site *Site
	ok   bool
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	dataDir := envString("DATA_DIR", filepath.Join(root, "data"))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}

	store := NewStore(filepath.Join(dataDir, "sites.json"))
	scanner := NewScanner(store, filepath.Join(dataDir, "settings.json"))
	publicFS, err := stdfs.Sub(embeddedAssets, "public")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		scanner.TriggerScan()
		respondJSON(w, http.StatusOK, scanner.Snapshot())
	})
	mux.HandleFunc("/api/scan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		scanner.TriggerScan()
		respondJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Target string `json:"target"`
			Rescan bool   `json:"rescan"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		snapshot, err := scanner.UpdateTarget(payload.Target, payload.Rescan)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, http.StatusOK, snapshot)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		scanner.ServeEvents(w, r)
	})

	fileServer := http.FileServer(http.FS(publicFS))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" {
			payload, err := stdfs.ReadFile(publicFS, "index.html")
			if err != nil {
				http.Error(w, "index page not found", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		fileServer.ServeHTTP(w, r)
	}))

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, appPort),
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("Local Web Nav running at http://127.0.0.1:%d\n", appPort)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func NewStore(path string) *Store {
	s := &Store{
		path:  path,
		sites: map[string]*Site{},
	}
	s.load()
	return s
}

func (s *Store) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.path)
	if err != nil {
		return
	}

	var file StoreFile
	if err := json.Unmarshal(payload, &file); err != nil {
		log.Printf("load store failed: %v\n", err)
		return
	}

	for _, site := range file.Sites {
		copySite := *site
		s.sites[copySite.ID] = normalizeSite(&copySite)
	}
}

func (s *Store) saveLocked() {
	items := make([]*Site, 0, len(s.sites))
	for _, site := range s.sites {
		copySite := *site
		items = append(items, &copySite)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].LastSeenAt > items[j].LastSeenAt
	})

	payload, err := json.MarshalIndent(StoreFile{Sites: items}, "", "  ")
	if err != nil {
		log.Printf("marshal store failed: %v\n", err)
		return
	}
	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		log.Printf("save store failed: %v\n", err)
	}
}

func (s *Store) ListByIP(ip string) []*Site {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*Site, 0)
	for _, site := range s.sites {
		if site.IP != ip {
			continue
		}
		copySite := *site
		items = append(items, &copySite)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].LastSeenAt > items[j].LastSeenAt
	})

	if len(items) > historyLimit {
		items = items[:historyLimit]
	}
	return items
}

func (s *Store) Upsert(site *Site) *Site {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format(time.RFC3339)
	id := siteID(site.Scheme, site.IP, site.Port)
	existing, ok := s.sites[id]
	if !ok {
		existing = &Site{
			ID:          id,
			FirstSeenAt: now,
		}
	}

	existing.ID = id
	existing.IP = site.IP
	existing.Port = site.Port
	existing.Scheme = site.Scheme
	existing.URL = site.URL
	existing.Title = choose(site.Title, fmt.Sprintf("%s:%d", site.IP, site.Port))
	existing.IconURL = choose(site.IconURL, defaultIcon)
	existing.StatusCode = site.StatusCode
	existing.ServerHeader = site.ServerHeader
	existing.ContentType = site.ContentType
	existing.LastSeenAt = now
	existing.FailCount = 0
	existing.Source = choose(site.Source, "scan")

	s.sites[id] = existing
	s.saveLocked()

	copySite := *existing
	return &copySite
}

func (s *Store) MarkFailure(ip string, port int) []*Site {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := make([]*Site, 0)
	changed := false
	now := time.Now().Format(time.RFC3339)
	for id, site := range s.sites {
		if site.IP != ip || site.Port != port {
			continue
		}
		changed = true
		site.FailCount++
		site.LastSeenAt = now
		if site.FailCount >= maxFailCount {
			copySite := *site
			removed = append(removed, &copySite)
			delete(s.sites, id)
		}
	}
	if changed {
		s.saveLocked()
	}
	return removed
}

func (s *Store) RemoveByIPPort(ip string, port int) []*Site {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := make([]*Site, 0)
	for id, site := range s.sites {
		if site.IP != ip || site.Port != port {
			continue
		}
		copySite := *site
		removed = append(removed, &copySite)
		delete(s.sites, id)
	}
	if len(removed) > 0 {
		s.saveLocked()
	}
	return removed
}

func (s *Store) PruneSelfEntries(port int, selfTargets map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for id, site := range s.sites {
		if site.Port != port {
			continue
		}
		if _, ok := selfTargets[normalizeTargetKey(site.IP)]; !ok {
			continue
		}
		delete(s.sites, id)
		changed = true
	}
	if changed {
		s.saveLocked()
	}
}

func NewScanner(store *Store, settingsPath string) *Scanner {
	selfTargets := localTargetAliases()
	store.PruneSelfEntries(appPort, selfTargets)
	target := loadSavedTarget(settingsPath)
	if target == "" {
		target = localIPv4()
	}

	return &Scanner{
		store:        store,
		targetIP:     target,
		progress:     Progress{Phase: "idle"},
		clients:      map[chan sseMessage]struct{}{},
		settingsPath: settingsPath,
		selfTargets:  selfTargets,
	}
}

func (s *Scanner) Snapshot() Snapshot {
	s.mu.RLock()
	targetIP := s.targetIP
	running := s.running
	started := s.started
	finished := s.finished
	progress := s.progress
	s.mu.RUnlock()

	return Snapshot{
		TargetIP:           targetIP,
		Target:             targetIP,
		Running:            running,
		LastScanStartedAt:  started,
		LastScanFinishedAt: finished,
		Progress:           progress,
		Sites:              s.store.ListByIP(targetIP),
	}
}

func (s *Scanner) TriggerScan() {
	s.mu.Lock()
	if strings.TrimSpace(s.targetIP) == "" {
		s.targetIP = localIPv4()
	}
	if s.running {
		s.restart = true
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.running = true
	s.cancel = cancel
	s.restart = false
	s.started = time.Now().Format(time.RFC3339)
	s.progress = Progress{
		Phase:      "preparing",
		Scanned:    0,
		Total:      65535,
		Discovered: int64(len(s.store.ListByIP(s.targetIP))),
	}
	s.mu.Unlock()

	s.broadcast("status", s.Snapshot())

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.cancel = nil
			s.finished = time.Now().Format(time.RFC3339)
			s.progress.Phase = "done"
			s.progress.Discovered = int64(len(s.store.ListByIP(s.targetIP)))
			shouldRestart := s.restart
			s.restart = false
			s.mu.Unlock()
			s.broadcast("status", s.Snapshot())
			if shouldRestart {
				s.TriggerScan()
			}
		}()
		s.scan(ctx)
	}()
}

func (s *Scanner) UpdateTarget(target string, rescan bool) (Snapshot, error) {
	sanitized := strings.TrimSpace(target)
	if sanitized == "" {
		sanitized = localIPv4()
	} else {
		var err error
		sanitized, err = sanitizeTarget(sanitized)
		if err != nil {
			return Snapshot{}, err
		}
	}

	s.mu.Lock()
	s.targetIP = sanitized
	s.mu.Unlock()
	saveTarget(s.settingsPath, sanitized)
	s.broadcast("status", s.Snapshot())

	if rescan {
		s.TriggerScan()
	}
	return s.Snapshot(), nil
}

func (s *Scanner) scan(ctx context.Context) {
	s.mu.RLock()
	ip := s.targetIP
	s.mu.RUnlock()

	history := s.store.ListByIP(ip)
	prioritySet := map[int]struct{}{}
	priorityPorts := make([]int, 0, len(history)+len(commonPorts))

	for _, site := range history {
		if _, ok := prioritySet[site.Port]; !ok {
			prioritySet[site.Port] = struct{}{}
			priorityPorts = append(priorityPorts, site.Port)
		}
	}
	for _, port := range commonPorts {
		if _, ok := prioritySet[port]; !ok {
			prioritySet[port] = struct{}{}
			priorityPorts = append(priorityPorts, port)
		}
	}

	otherPorts := make([]int, 0, 65535-len(priorityPorts))
	for port := 1; port <= 65535; port++ {
		if _, ok := prioritySet[port]; ok {
			continue
		}
		otherPorts = append(otherPorts, port)
	}

	total := len(priorityPorts) + len(otherPorts)
	s.setProgress("history-and-common", 0, int64(total))
	s.scanGroup(ctx, ip, priorityPorts, "history-and-common")
	if ctx.Err() != nil {
		return
	}
	s.scanGroup(ctx, ip, otherPorts, "full-range")
}

func (s *Scanner) scanGroup(ctx context.Context, ip string, ports []int, phase string) {
	s.setPhase(phase)
	if len(ports) == 0 {
		return
	}

	workerCount := runtime.NumCPU() * 128
	if workerCount < 256 {
		workerCount = 256
	}
	if workerCount > 2048 {
		workerCount = 2048
	}

	portCh := make(chan int, workerCount)
	var wg sync.WaitGroup
	var scanned atomic.Int64

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case port, ok := <-portCh:
					if !ok {
						return
					}
					s.inspectPort(ctx, ip, port)
				}
				current := s.bumpScanned()
				scanned.Store(current)
				if current%50 == 0 {
					s.broadcast("progress", s.Snapshot())
				}
			}
		}()
	}

	stopped := false
	for _, port := range ports {
		select {
		case <-ctx.Done():
			stopped = true
		case portCh <- port:
		}
		if stopped {
			break
		}
	}
	close(portCh)
	wg.Wait()

	if scanned.Load() > 0 {
		s.broadcast("progress", s.Snapshot())
	}
}

func (s *Scanner) inspectPort(ctx context.Context, ip string, port int) {
	if s.isSelfTarget(ip) && port == appPort {
		return
	}
	if !probeTCP(ctx, ip, port) {
		removed := s.store.RemoveByIPPort(ip, port)
		for _, item := range removed {
			s.broadcast("site-removed", item)
		}
		return
	}

	site, ok := probeWeb(ctx, ip, port)
	if !ok {
		removed := s.store.MarkFailure(ip, port)
		for _, item := range removed {
			s.broadcast("site-removed", item)
		}
		return
	}

	saved := s.store.Upsert(site)
	s.mu.Lock()
	s.progress.Discovered = int64(len(s.store.ListByIP(ip)))
	s.mu.Unlock()
	s.broadcast("site-found", saved)
}

func (s *Scanner) setProgress(phase string, scanned, total int64) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.progress.Scanned = scanned
	s.progress.Total = total
	s.mu.Unlock()
	s.broadcast("status", s.Snapshot())
}

func (s *Scanner) setPhase(phase string) {
	s.mu.Lock()
	s.progress.Phase = phase
	s.mu.Unlock()
	s.broadcast("status", s.Snapshot())
}

func (s *Scanner) bumpScanned() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress.Scanned++
	return s.progress.Scanned
}

func (s *Scanner) ServeEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}

	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream; charset=utf-8")
	headers.Set("Cache-Control", "no-store")
	headers.Set("Connection", "keep-alive")

	ch := make(chan sseMessage, 64)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	s.writeEvent(w, "hello", s.Snapshot())
	flusher.Flush()
	s.TriggerScan()

	ctx := r.Context()
	notify := time.NewTicker(25 * time.Second)
	defer notify.Stop()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
		close(ch)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-notify.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case msg := <-ch:
			s.writeEvent(w, msg.Event, msg.Data)
			flusher.Flush()
		}
	}
}

func (s *Scanner) writeEvent(w http.ResponseWriter, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
}

func (s *Scanner) broadcast(event string, data any) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- sseMessage{Event: event, Data: data}:
		default:
		}
	}
}

func (s *Scanner) isSelfTarget(target string) bool {
	_, ok := s.selfTargets[normalizeTargetKey(target)]
	return ok
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func localIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String()
		}
	}
	return "127.0.0.1"
}

func siteID(scheme, ip string, port int) string {
	return fmt.Sprintf("%s_%s_%d", scheme, ip, port)
}

func normalizeSite(site *Site) *Site {
	copySite := *site
	if copySite.Title == "" {
		copySite.Title = fmt.Sprintf("%s:%d", copySite.IP, copySite.Port)
	}
	if copySite.IconURL == "" {
		copySite.IconURL = defaultIcon
	}
	if copySite.Source == "" {
		copySite.Source = "scan"
	}
	if copySite.FirstSeenAt == "" {
		copySite.FirstSeenAt = time.Now().Format(time.RFC3339)
	}
	if copySite.LastSeenAt == "" {
		copySite.LastSeenAt = copySite.FirstSeenAt
	}
	return &copySite
}

func choose(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func probeTCP(ctx context.Context, ip string, port int) bool {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: tcpTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func probeWeb(ctx context.Context, ip string, port int) (*Site, bool) {
	for _, scheme := range []string{"http", "https"} {
		target := fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(ip, strconv.Itoa(port)))
		site, ok := fetchSite(ctx, target, ip, port, scheme)
		if ok {
			return site, true
		}
	}
	return nil, false
}

func fetchSite(ctx context.Context, target, ip string, port int, scheme string) (*Site, bool) {
	client := &http.Client{
		Timeout: httpTimeout,
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: httpTimeout,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", "LocalWebNav/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, readBodyLimit))
	if err != nil {
		return nil, false
	}

	bodyText := string(body)
	if !looksLikeHTML(resp.Header.Get("Content-Type"), bodyText) {
		return nil, false
	}

	title := extractTitle(bodyText)
	if title == "" {
		title = resp.Header.Get("Server")
	}
	if title == "" {
		title = fmt.Sprintf("%s:%d", ip, port)
	}

	iconURL := extractIcon(resp.Request.URL.String(), bodyText)
	if iconURL == "" {
		iconURL = defaultIcon
	}

	return &Site{
		IP:           ip,
		Port:         port,
		Scheme:       scheme,
		URL:          target,
		Title:        title,
		IconURL:      iconURL,
		StatusCode:   resp.StatusCode,
		ServerHeader: resp.Header.Get("Server"),
		ContentType:  resp.Header.Get("Content-Type"),
		Source:       "scan",
	}, true
}

func looksLikeHTML(contentType, body string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml") {
		return true
	}
	snippet := strings.ToLower(body)
	return strings.Contains(snippet, "<html") || strings.Contains(snippet, "<title") || strings.Contains(snippet, "<!doctype html")
}

func extractTitle(body string) string {
	matches := titlePattern.FindStringSubmatch(body)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(matches[1]))
}

func extractIcon(baseURL, body string) string {
	matches := iconPattern.FindStringSubmatch(body)
	if len(matches) >= 2 {
		if parsed, err := url.Parse(baseURL); err == nil {
			if iconURL, err := parsed.Parse(strings.TrimSpace(matches[1])); err == nil {
				return iconURL.String()
			}
		}
	}
	if parsed, err := url.Parse(baseURL); err == nil {
		if iconURL, err := parsed.Parse("/favicon.ico"); err == nil {
			return iconURL.String()
		}
	}
	return ""
}

func loadSavedTarget(path string) string {
	payload, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	var settings AppSettings
	if err := json.Unmarshal(payload, &settings); err != nil {
		return ""
	}

	target, err := sanitizeTarget(settings.Target)
	if err != nil {
		return ""
	}
	return target
}

func saveTarget(path, target string) {
	payload, err := json.MarshalIndent(AppSettings{Target: target}, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, payload, 0o644)
}

func sanitizeTarget(value string) (string, error) {
	target := strings.TrimSpace(value)
	if target == "" {
		return "", errors.New("target is required")
	}

	if strings.Contains(target, "://") {
		parsed, err := url.Parse(target)
		if err != nil {
			return "", errors.New("invalid target")
		}
		target = parsed.Hostname()
	}

	if host, _, err := net.SplitHostPort(target); err == nil && host != "" {
		target = host
	}

	target = strings.TrimSpace(strings.Trim(target, "[]"))
	if target == "" {
		return "", errors.New("invalid target")
	}

	if ip := net.ParseIP(target); ip != nil {
		return target, nil
	}

	lower := strings.ToLower(target)
	if lower == "localhost" {
		return lower, nil
	}
	if !hostPattern.MatchString(lower) || strings.Contains(lower, "..") || strings.HasPrefix(lower, ".") || strings.HasSuffix(lower, ".") || strings.HasPrefix(lower, "-") || strings.HasSuffix(lower, "-") {
		return "", errors.New("target must be a valid IP or hostname")
	}
	return lower, nil
}

func localTargetAliases() map[string]struct{} {
	values := map[string]struct{}{
		"127.0.0.1": {},
		"localhost": {},
		"::1":       {},
	}

	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		values[normalizeTargetKey(hostname)] = struct{}{}
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return values
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil {
				continue
			}
			values[normalizeTargetKey(ip.String())] = struct{}{}
		}
	}
	return values
}

func normalizeTargetKey(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]"))
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 || parsed > 65535 {
		return fallback
	}
	return parsed
}
