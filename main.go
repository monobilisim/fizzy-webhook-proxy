package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// --- Configuration Types ---

type TargetType string

const (
	TargetZulip      TargetType = "zulip"
	TargetGoogleChat TargetType = "google-chat"
	TargetGotify     TargetType = "gotify"
)

type target struct {
	Name string
	Path string
	URL  string
	Type TargetType
}

// --- Fizzy Payload Types (Generic JSON) ---

// FizzyPayload represents the incoming webhook payload from Fizzy.
// Based on documentation: https://www.pilanites.com/fizzy-webhooks-documentation/
type FizzyPayload struct {
	ID        string         `json:"id"` // Event ID (evt_123)
	Action    string         `json:"action"`
	Eventable FizzyEventable `json:"eventable"`
	Creator   FizzyUser      `json:"creator"`
	Board     FizzyBoard     `json:"board"`
	URL       string         `json:"url"`
	Assignee  *FizzyUser     `json:"assignee,omitempty"`
	Column    *FizzyColumn   `json:"column,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Card      *FizzyCard     `json:"card,omitempty"`
}

type FizzyCard struct {
	Title string `json:"title"`
}

type FizzyBoard struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type FizzyColumn struct {
	Name string `json:"name"`
}

type FizzyEventable struct {
	ID     string     `json:"id"`
	Number int        `json:"number"` // Card number (e.g. 29) - For Cards
	Title  string     `json:"title"`  // For cards
	Card   *FizzyCard `json:"card,omitempty"`
	Parent *FizzyCard `json:"parent,omitempty"` // Fallback for comments
	Body   struct {
		PlainText string `json:"plain_text"`
	} `json:"body"` // For comments
	URL          string    `json:"url"`
	ReactionsURL string    `json:"reactions_url"`
	Creator      FizzyUser `json:"creator"`
}

type FizzyUser struct {
	Name string `json:"name"`
}

// --- Destination Payload Types ---

type ZulipPayload struct {
	Content string `json:"text"`
}

type GoogleChatPayload struct {
	Text    string   `json:"text,omitempty"`
	CardsV2 []CardV2 `json:"cardsV2,omitempty"`
}

type CardV2 struct {
	CardID string `json:"cardId"`
	Card   Card   `json:"card"`
}

type Card struct {
	Header   CardHeader    `json:"header"`
	Sections []CardSection `json:"sections"`
}

type CardHeader struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	ImageURL string `json:"imageUrl,omitempty"`
}

type CardSection struct {
	Header  string   `json:"header,omitempty"`
	Widgets []Widget `json:"widgets"`
}

type Widget struct {
	DecoratedText *DecoratedText `json:"decoratedText,omitempty"`
	TextParagraph *TextParagraph `json:"textParagraph,omitempty"`
	ButtonList    *ButtonList    `json:"buttonList,omitempty"`
}

type DecoratedText struct {
	TopLabel  string `json:"topLabel,omitempty"`
	Text      string `json:"text"`
	StartIcon *Icon  `json:"startIcon,omitempty"`
}

type Icon struct {
	KnownIcon string `json:"knownIcon,omitempty"`
	IconUrl   string `json:"iconUrl,omitempty"`
}

type TextParagraph struct {
	Text string `json:"text"`
}

type ButtonList struct {
	Buttons []Button `json:"buttons"`
}

type Button struct {
	Text    string   `json:"text,omitempty"`
	Icon    *Icon    `json:"icon,omitempty"`
	OnClick *OnClick `json:"onClick,omitempty"`
}

type OnClick struct {
	OpenLink *OpenLink `json:"openLink,omitempty"`
}

type OpenLink struct {
	URL string `json:"url"`
}

func translateToGoogleChat(f FizzyPayload) ([]byte, error) {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Biri"
	}
	verb, emoji := prettyAction(f)

	finalURL := resolveFizzyURL(f)

	// Title: The Subject (Card Title, Board Name, or generic "Fizzy")
	// Subtitle: The Event (Actor + Verb)

	subjectTitle := f.Eventable.Title
	if subjectTitle == "" {
		if f.Board.Name != "" {
			subjectTitle = f.Board.Name
		} else {
			subjectTitle = "Fizzy Bildirimi"
		}
	}

	headerSubtitle := fmt.Sprintf("%s %s", actor, verb)

	// Card Header
	header := CardHeader{
		Title:    subjectTitle,
		Subtitle: headerSubtitle,
		ImageURL: "",
	}

	// Widgets
	var widgets []Widget

	// Add Comment Body if available
	if f.Eventable.Body.PlainText != "" {
		widgets = append(widgets, Widget{
			TextParagraph: &TextParagraph{
				Text: f.Eventable.Body.PlainText,
			},
		})
	}

	if f.Board.Name != "" && subjectTitle != f.Board.Name {
		widgets = append(widgets, Widget{
			DecoratedText: &DecoratedText{
				TopLabel:  "Pano",
				Text:      f.Board.Name,
				StartIcon: &Icon{KnownIcon: "TICKET"},
			},
		})
	}

	// Button Widget
	widgets = append(widgets, Widget{
		ButtonList: &ButtonList{
			Buttons: []Button{
				{
					Text: "Fizzy'de Görüntüle",
					Icon: &Icon{KnownIcon: "OPEN_IN_NEW"},
					OnClick: &OnClick{
						OpenLink: &OpenLink{URL: finalURL},
					},
				},
			},
		},
	})

	card := CardV2{
		CardID: fmt.Sprintf("fizzy-%d", time.Now().UnixNano()),
		Card: Card{
			Header:   header,
			Sections: []CardSection{{Widgets: widgets}},
		},
	}

	// Fallback text for mobile push notifications or unsupported clients
	fallbackText := fmt.Sprintf("%s %s %s: %s", emoji, actor, verb, subjectTitle)

	payload := GoogleChatPayload{
		Text:    fallbackText,
		CardsV2: []CardV2{card},
	}
	return json.Marshal(payload)
}

type GotifyPayload struct {
	Message  string                 `json:"message"`
	Title    string                 `json:"title,omitempty"`
	Priority int                    `json:"priority,omitempty"`
	Extras   map[string]interface{} `json:"extras,omitempty"`
}

// --- Deduplication ---

type DedupeKey struct {
	TargetName  string
	Action      string
	EventableID string
}

var (
	dedupeCache = make(map[DedupeKey]time.Time)
	dedupeMu    sync.Mutex
)

func isDuplicate(targetName, action, eventableID string) bool {
	if eventableID == "" {
		return false
	}
	key := DedupeKey{TargetName: targetName, Action: action, EventableID: eventableID}

	dedupeMu.Lock()
	defer dedupeMu.Unlock()

	lastTime, found := dedupeCache[key]
	now := time.Now()

	// Cleanup old entries randomly/occasionally or just let it grow (it's small enough for this use case usually, but better to be safe)
	// For simplicity, we won't do full GC here, but strict 2-second window check.

	if found {
		if now.Sub(lastTime) < 2*time.Second {
			return true
		}
	}

	dedupeCache[key] = now
	return false
}

// --- Main Handler ---

func main() {
	loadDotEnv(".env")

	port := envOrDefault("PORT", "8080")
	targets := loadTargets()
	if len(targets) == 0 {
		log.Println("no webhook targets configured; set *_WEBHOOK_URL in .env")
	}

	mux := http.NewServeMux()
	for _, t := range targets {
		t := t // capture
		mux.HandleFunc(t.Path, func(w http.ResponseWriter, r *http.Request) {
			forwardRequest(w, r, t)
		})
		log.Printf("routing %s -> %s (%s)", t.Path, t.URL, t.Type)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[DEBUG] Received request on root handler: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		if len(targets) == 0 {
			fmt.Fprintln(w, "Fizzy webhook proxy: no targets configured")
			return
		}
		fmt.Fprintln(w, "Fizzy webhook proxy targets:")
		for _, t := range targets {
			fmt.Fprintf(w, " - %s (%s) at %s\n", t.Name, t.Type, t.Path)
		}
	})

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loadTargets() []target {
	return filterTargets([]target{
		{
			Name: "zulip",
			Path: ensureLeadingSlash(envOrDefault("ZULIP_PATH", "/zulip")),
			URL:  os.Getenv("ZULIP_WEBHOOK_URL"),
			Type: TargetZulip,
		},
		{
			Name: "google-chat",
			Path: ensureLeadingSlash(envOrDefault("GOOGLE_CHAT_PATH", "/google-chat")),
			URL:  os.Getenv("GOOGLE_CHAT_WEBHOOK_URL"),
			Type: TargetGoogleChat,
		},
		{
			Name: "gotify",
			Path: ensureLeadingSlash(envOrDefault("GOTIFY_PATH", "/gotify")),
			URL:  os.Getenv("GOTIFY_WEBHOOK_URL"),
			Type: TargetGotify,
		},
	})
}

func forwardRequest(w http.ResponseWriter, r *http.Request, t target) {
	log.Printf("[DEBUG] Received request on forward handler (%s): %s %s", t.Name, r.Method, r.URL.Path)
	if t.URL == "" {
		http.Error(w, "target URL not configured", http.StatusServiceUnavailable)
		return
	}

	// Read original body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse Fizzy Payload
	var fizzy FizzyPayload
	if err := json.Unmarshal(body, &fizzy); err != nil {
		log.Printf("error parsing fizzy payload: %v", err)
		http.Error(w, fmt.Sprintf("invalid fizzy json: %v. Body was: %s", err, string(body)), http.StatusBadRequest)
		return
	}

	// Deduplication Check
	if isDuplicate(t.Name, fizzy.Action, fizzy.Eventable.ID) {
		log.Printf("[INFO] Dropping duplicate event: Target=%s Action=%s ID=%s", t.Name, fizzy.Action, fizzy.Eventable.ID)
		w.WriteHeader(http.StatusOK) // Return success to Fizzy so it doesn't retry
		return
	}

	// Translate Payload
	var newBody []byte
	var translateErr error

	switch t.Type {
	case TargetZulip:
		newBody, translateErr = translateToZulip(fizzy)
	case TargetGoogleChat:
		newBody, translateErr = translateToGoogleChat(fizzy)
	case TargetGotify:
		newBody, translateErr = translateToGotify(fizzy)
	default:
		// Should not happen with current setup
		newBody = body
	}

	if translateErr != nil {
		log.Printf("translation error for %s: %v", t.Name, translateErr)
		http.Error(w, "translation failed", http.StatusInternalServerError)
		return
	}

	// Create new request to destination
	// Note: We ignore original query params for simplicity unless needed.
	destURL := appendQuery(t.URL, r.URL.RawQuery)
	// Log the payload we are sending for debug
	log.Printf("Forwarding to %s (%s): %s", t.Name, t.Type, string(newBody))

	req, err := http.NewRequestWithContext(r.Context(), "POST", destURL, bytes.NewReader(newBody))
	if err != nil {
		http.Error(w, "failed to build forward request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Fizzy-Proxy/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("forward error (%s): %v", t.Name, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body to log it (and then write it to w)
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("failed to read upstream response body: %v", err)
	}
	log.Printf("Upstream response (%s) Status: %d Body: %s", t.Name, resp.StatusCode, string(respBodyBytes))

	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := w.Write(respBodyBytes); err != nil {
		log.Printf("response copy error (%s): %v", t.Name, err)
	}
}

// --- Translation Logic ---

func translateToZulip(f FizzyPayload) ([]byte, error) {
	msg := buildMessage(f)
	payload := ZulipPayload{
		Content: msg,
	}
	return json.Marshal(payload)
}

func translateToGotify(f FizzyPayload) ([]byte, error) {
	msg := buildMessage(f)
	verb, _ := prettyAction(f)
	actor := f.Creator.Name
	if actor == "" {
		actor = "Biri"
	}
	title := fmt.Sprintf("Fizzy: %s %s", actor, verb)
	payload := GotifyPayload{
		Message:  msg,
		Title:    title,
		Priority: 5,
		Extras: map[string]interface{}{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	}
	return json.Marshal(payload)
}

// buildMessage creates a human-readable string from the Fizzy payload.
func buildMessage(f FizzyPayload) string {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Biri"
	}

	verb, emoji := prettyAction(f)

	// Determine Subject (Title)
	subject := f.Eventable.Title
	if subject == "" {
		// Try to find title in other places (e.g. for comments)
		if f.Card != nil && f.Card.Title != "" {
			subject = f.Card.Title
		} else if f.Eventable.Card != nil && f.Eventable.Card.Title != "" {
			subject = f.Eventable.Card.Title
		} else if f.Eventable.Parent != nil && f.Eventable.Parent.Title != "" {
			subject = f.Eventable.Parent.Title
		} else if f.Board.Name != "" {
			subject = f.Board.Name
		} else {
			subject = "Fizzy Bildirimi"
		}
	}

	// Body Content
	var body string
	if f.Eventable.Body.PlainText != "" {
		body = fmt.Sprintf("> %s", f.Eventable.Body.PlainText)
	}

	// Extras (Board Name, etc.)
	var extras []string
	if f.Board.Name != "" && subject != f.Board.Name {
		extras = append(extras, fmt.Sprintf("🎫 **Pano:** %s", f.Board.Name))
	}

	// Determine URL
	urlStr := resolveFizzyURL(f)

	if subject == f.Board.Name || subject == "Fizzy Bildirimi" {
		// inspect raw URLs not the resolved one which might be a search URL
		rawURL := f.Eventable.URL
		if rawURL == "" {
			rawURL = f.URL
		}
		if rawURL == "" {
			rawURL = f.Eventable.ReactionsURL
		}

		// Check for /cards/123
		if strings.Contains(rawURL, "/cards/") {
			parts := strings.Split(rawURL, "/cards/")
			if len(parts) > 1 {
				// Extract ID part (digits)
				sub := parts[1]
				// Might be followed by /search or # etc or even /comments
				idPart := ""
				for _, r := range sub {
					if r >= '0' && r <= '9' {
						idPart += string(r)
					} else {
						break
					}
				}
				if idPart != "" {
					subject = fmt.Sprintf("Kart #%s", idPart)
				}
			}
		}
	}

	var sb strings.Builder

	hideSubject := false
	if f.Action == "comment_created" && strings.HasPrefix(subject, "Kart #") {
		hideSubject = true
	}

	if hideSubject {
		sb.WriteString(fmt.Sprintf("### %s **%s** %s", emoji, actor, verb))
	} else {
		sb.WriteString(fmt.Sprintf("### %s **%s** %s: %s", emoji, actor, verb, subject))
	}

	if body != "" {
		sb.WriteString("\n\n")
		sb.WriteString(body)
	}

	if len(extras) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(strings.Join(extras, "\n"))
	}

	sb.WriteString(fmt.Sprintf("\n\n[Fizzy'de Görüntüle ↗️](%s)", urlStr))

	return sb.String()
}

func resolveFizzyURL(f FizzyPayload) string {
	// Determine Initial URL
	urlStr := f.Eventable.URL
	if urlStr == "" {
		urlStr = f.URL
	}

	// Override domain and slug if env vars are set
	rootURL := os.Getenv("FIZZY_ROOT_URL")
	targetSlug := os.Getenv("FIZZY_ACCOUNT_SLUG")

	// 1. If we have a Card Number (Cards), construct CLEAN URL: .../cards/29
	if f.Eventable.Number != 0 {
		slug := "0000001" // Default
		if targetSlug != "" {
			slug = targetSlug
		} else if rootURL != "" {
			if u, err := url.Parse(rootURL); err == nil {
				parts := strings.Split(u.Path, "/")
				if len(parts) > 1 && parts[1] != "" {
					slug = parts[1]
				}
			}
		}

		base := "https://fizzy.example.com"
		if rootURL != "" {
			if u, err := url.Parse(rootURL); err == nil {
				u.Path = ""
				base = u.String()
			}
		} else if parsed, err := url.Parse(urlStr); err == nil {
			parsed.Path = ""
			parsed.RawQuery = ""
			base = parsed.String()
		}

		return fmt.Sprintf("%s/%s/cards/%d", base, slug, f.Eventable.Number)
	}

	// 2. Fallback for Comments (Search Strategy)
	cardUUID := ""
	if strings.Contains(urlStr, "/cards/") {
		parts := strings.Split(urlStr, "/cards/")
		if len(parts) > 1 {
			sub := parts[1]
			slashParts := strings.Split(sub, "/")
			if len(slashParts) > 0 {
				cardUUID = slashParts[0]
			}
		}
	}

	if cardUUID != "" {
		slug := "0000001"
		if targetSlug != "" {
			slug = targetSlug
		} else if rootURL != "" {
			if u, err := url.Parse(rootURL); err == nil {
				parts := strings.Split(u.Path, "/")
				if len(parts) > 1 && parts[1] != "" {
					slug = parts[1]
				}
			}
		}

		base := "https://fizzy.example.com"
		if rootURL != "" {
			if u, err := url.Parse(rootURL); err == nil {
				u.Path = ""
				base = u.String()
			}
		} else if parsed, err := url.Parse(urlStr); err == nil {
			parsed.Path = ""
			parsed.RawQuery = ""
			base = parsed.String()
		}

		commentUUID := ""
		if strings.Contains(urlStr, "/comments/") {
			parts := strings.Split(urlStr, "/comments/")
			if len(parts) > 1 {
				commentUUID = strings.Split(parts[1], "/")[0]
			}
		}

		res := fmt.Sprintf("%s/%s/search?q=%s", base, slug, cardUUID)
		if commentUUID != "" {
			res += fmt.Sprintf("#comment_%s", commentUUID)
		}
		return res
	}

	// 3. Board Fallback
	if f.Board.URL != "" {
		urlStr = f.Board.URL
		if rootURL != "" || targetSlug != "" {
			if u, err := url.Parse(urlStr); err == nil {
				if rootURL != "" {
					if rootU, err := url.Parse(rootURL); err == nil {
						u.Scheme = rootU.Scheme
						u.Host = rootU.Host
					}
				}
				slug := "0000001"
				if targetSlug != "" {
					slug = targetSlug
				} else if rootURL != "" {
					if rootU, err := url.Parse(rootURL); err == nil {
						p := strings.Split(rootU.Path, "/")
						if len(p) > 1 && p[1] != "" {
							slug = p[1]
						}
					}
				}
				parts := strings.Split(u.Path, "/")
				if len(parts) > 1 && parts[1] != "" {
					parts[1] = slug
					u.Path = strings.Join(parts, "/")
				}
				urlStr = u.String()
			}
		}
		return urlStr
	}

	return urlStr
}
func prettyAction(f FizzyPayload) (verb string, emoji string) {
	action := f.Action
	// Normalize action string just in case
	action = strings.ToLower(action)

	switch action {
	case "comment_created":
		return "yorum yaptı", "💬"
	case "card_created":
		return "kart oluşturdu", "🃏"
	case "card_published":
		return "kart yayınladı", "📢"
	case "card_reopened":
		return "kartı yeniden açtı", "🔄"
	case "card_board_changed":
		return "kartın panosunu değiştirdi", "📋"
	case "card_moved":
		if f.Column != nil && f.Column.Name != "" {
			if f.Reason == "inactivity" {
				return fmt.Sprintf("kartı hareketsizlik nedeniyle **%s** listesine taşıdı", f.Column.Name), "💤"
			}
			return fmt.Sprintf("kartı **%s** listesine taşıdı", f.Column.Name), "truck"
		}
		return "kartı taşıdı", "truck"
	case "card_assigned":
		if f.Assignee != nil && f.Assignee.Name != "" {
			return fmt.Sprintf("kartı **%s** kişisine atadı", f.Assignee.Name), "👤"
		}
		return "kartı birine atadı", "👤"
	case "card_unassigned":
		return "kart atamasını kaldırdı", "👤"
	case "card_postponed":
		return "kartı erteledi", "💤"
	case "card_closed":
		if f.Column != nil && strings.EqualFold(f.Column.Name, "Done") {
			return "kartı tamamladı", "✅"
		}
		return "kartı kapattı", "✅"
	case "card_sent_back_to_triage":
		return "kartı değerlendirmeye geri gönderdi", "↩️"
	case "card_archived":
		// Check for "Done" or "Postponed" if possible...
		if f.Column != nil {
			if strings.EqualFold(f.Column.Name, "Done") {
				return "kartı tamamladı", "✅"
			}
			if strings.EqualFold(f.Column.Name, "Postponed") || strings.EqualFold(f.Column.Name, "Not Now") {
				return "kartı erteledi", "zzz"
			}
		}
		return "kartı arşivledi", "📦"
	default:
		return strings.ReplaceAll(action, "_", " "), "📢"
	}
}

// --- Helpers ---

func filterTargets(list []target) []target {
	var out []target
	for _, t := range list {
		if t.URL != "" {
			out = append(out, t)
		}
	}
	return out
}

func ensureLeadingSlash(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func appendQuery(baseURL, rawQuery string) string {
	if rawQuery == "" {
		return baseURL
	}
	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	return baseURL + separator + rawQuery
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// loadDotEnv reads a .env file (key=value per line) into the current environment.
func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("warning: unable to read %s: %v", path, err)
		}
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if _, ok := os.LookupEnv(key); !ok {
			_ = os.Setenv(key, val)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("warning: error reading %s: %v", path, err)
	}
}
