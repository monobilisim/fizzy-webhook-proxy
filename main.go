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
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Configuration Types ---

type TargetType string

const (
	TargetZulip      TargetType = "zulip"
	TargetZulipDM    TargetType = "zulip-dm"
	TargetGoogleChat TargetType = "google-chat"
	TargetGotify     TargetType = "gotify"
	TargetTelegram   TargetType = "telegram"
)

type target struct {
	Name       string
	Path       string
	URL        string
	Type       TargetType
	Identifier string
	ChatID     string
	// Zulip DM specific fields
	ZulipDMAuth       string   // "email:api_key" for Basic Auth
	ZulipDMRecipients []string // User IDs or email addresses to send DMs to
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
	ID          string       `json:"id"`
	Number      int          `json:"number"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      string       `json:"status,omitempty"`
	URL         string       `json:"url,omitempty"`
	Board       *FizzyBoard  `json:"board,omitempty"`
	Column      *FizzyColumn `json:"column,omitempty"`
	Creator     *FizzyUser   `json:"creator,omitempty"`
	Assignees   []FizzyUser  `json:"assignees,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Closed      bool         `json:"closed,omitempty"`
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
	ID          string       `json:"id"`
	Number      int          `json:"number"` // Card number (e.g. 29) - For Cards
	Title       string       `json:"title"`  // For cards
	Description string       `json:"description,omitempty"`
	Status      string       `json:"status,omitempty"`
	Column      *FizzyColumn `json:"column,omitempty"`
	Assignees   []FizzyUser  `json:"assignees,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Closed      bool         `json:"closed,omitempty"`
	Card        *FizzyCard   `json:"card,omitempty"`
	Parent      *FizzyCard   `json:"parent,omitempty"` // Fallback for comments
	Body        struct {
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
	Topic   string `json:"topic,omitempty"`
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

type GotifyPayload struct {
	Message  string                 `json:"message"`
	Title    string                 `json:"title,omitempty"`
	Priority int                    `json:"priority,omitempty"`
	Extras   map[string]interface{} `json:"extras,omitempty"`
}

type TelegramPayload struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
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
	debugMode   bool
	authToken   string // Global TOKEN for URL prefix
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

	if found {
		if now.Sub(lastTime) < 2*time.Second {
			return true
		}
	}

	dedupeCache[key] = now
	return false
}

// --- Type Detection from URL ---

// detectTargetType attempts to infer the target type from the webhook URL.
// Returns empty string if type cannot be detected.
func detectTargetType(webhookURL string) TargetType {
	lowerURL := strings.ToLower(webhookURL)

	// Google Chat: chat.googleapis.com
	if strings.Contains(lowerURL, "chat.googleapis.com") {
		return TargetGoogleChat
	}

	// Zulip DM: /api/v1/messages with auth and 'to' parameter (check before slack_incoming)
	if strings.Contains(lowerURL, "/api/v1/messages") {
		if parsedURL, err := url.Parse(webhookURL); err == nil {
			if parsedURL.User != nil && parsedURL.Query().Get("to") != "" {
				return TargetZulipDM
			}
		}
	}

	// Zulip: slack_incoming in URL (Zulip's Slack-compatible webhook)
	if strings.Contains(lowerURL, "slack_incoming") {
		return TargetZulip
	}

	// Gotify: /message?token pattern
	if strings.Contains(lowerURL, "/message?token") {
		return TargetGotify
	}

	// Telegram: api.telegram.org
	if strings.Contains(lowerURL, "api.telegram.org") {
		return TargetTelegram
	}

	return ""
}

// --- Main Handler ---

func main() {
	loadDotEnv(".env")

	port := envOrDefault("PORT", "3499") // "FIZZ" on phone keypad
	debugMode = os.Getenv("DEBUG") == "true"
	authToken = os.Getenv("TOKEN")

	if authToken == "" {
		log.Fatal("TOKEN is required; set TOKEN in environment for URL prefix security")
	}

	targets := loadTargets()
	if len(targets) == 0 {
		log.Println("no webhook targets configured; set <IDENTIFIER>_URL in environment")
	}

	mux := http.NewServeMux()
	for _, t := range targets {
		t := t // capture
		mux.HandleFunc(t.Path, func(w http.ResponseWriter, r *http.Request) {
			forwardRequest(w, r, t)
		})
		// Log full path with token only in service output
		log.Printf("routing %s -> %s (%s)", t.Path, t.URL, t.Type)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if debugMode {
			log.Printf("[DEBUG] Received request on root handler: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		if len(targets) == 0 {
			fmt.Fprintln(w, "Fizzy webhook proxy: no targets configured")
			return
		}
		fmt.Fprintln(w, "Fizzy webhook proxy targets:")
		for _, t := range targets {
			// Show path without token in browser (just /identifier)
			displayPath := "/" + t.Identifier
			fmt.Fprintf(w, " - %s (%s) at %s\n", t.Name, t.Type, displayPath)
		}
	})

	log.Printf("listening on :%s", port)
	if authToken != "" {
		log.Printf("TOKEN authentication enabled (prefix: /%s/...)", authToken)
	}
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// loadTargets scans environment variables for webhook configurations.
// Pattern: {IDENTIFIER}_URL and optionally {IDENTIFIER}_TYPE
// Example: ZULIP_URL, STATUS_PAGE_URL + STATUS_PAGE_TYPE=gotify
func loadTargets() []target {
	var targets []target

	// Regex to find *_URL variables (but not ending with just _URL which would be empty identifier)
	urlSuffix := "_URL"

	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		// Skip empty values
		if value == "" {
			continue
		}

		// Check if it ends with _URL
		if !strings.HasSuffix(key, urlSuffix) {
			continue
		}

		// Skip special env vars
		if key == "FIZZY_ROOT_URL" {
			continue
		}

		// Extract identifier: remove _URL suffix
		identifier := strings.TrimSuffix(key, urlSuffix)
		if identifier == "" {
			continue
		}

		// Convert to lowercase with hyphens for URL path
		// Example: STATUS_PAGE -> status-page
		pathIdentifier := strings.ToLower(strings.ReplaceAll(identifier, "_", "-"))

		// Determine target type
		var targetType TargetType

		// First check if TYPE is explicitly set
		typeKey := identifier + "_TYPE"
		if explicitType := os.Getenv(typeKey); explicitType != "" {
			targetType = TargetType(strings.ToLower(explicitType))
		} else {
			// Try to detect from URL
			targetType = detectTargetType(value)
		}

		// If still no type, skip with warning
		if targetType == "" {
			log.Printf("warning: cannot detect type for %s, set %s_TYPE explicitly", key, identifier)
			continue
		}

		// Build path with optional token prefix
		var path string
		if authToken != "" {
			path = fmt.Sprintf("/%s/%s", authToken, pathIdentifier)
		} else {
			path = fmt.Sprintf("/%s", pathIdentifier)
		}

		chatID := ""
		telegramURL := value
		if targetType == TargetTelegram {
			if parsedURL, err := url.Parse(value); err == nil {
				chatID = parsedURL.Query().Get("chat_id")
				botToken := parsedURL.Query().Get("token")
				if botToken != "" && chatID != "" {
					telegramURL = fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
				}
			}
			if chatID == "" {
				log.Printf("warning: Telegram target %s requires chat_id in URL (e.g., ...?chat_id=-123456)", key)
				continue
			}
		}

		var zulipDMAuth string
		var zulipDMRecipients []string
		zulipDMURL := value
		if targetType == TargetZulipDM {
			if parsedURL, err := url.Parse(value); err == nil {
				if parsedURL.User != nil {
					password, _ := parsedURL.User.Password()
					zulipDMAuth = parsedURL.User.Username() + ":" + password
					parsedURL.User = nil
				}
				toParam := parsedURL.Query().Get("to")
				if toParam != "" {
					zulipDMRecipients = strings.Split(toParam, ",")
					for i := range zulipDMRecipients {
						zulipDMRecipients[i] = strings.TrimSpace(zulipDMRecipients[i])
					}
					q := parsedURL.Query()
					q.Del("to")
					parsedURL.RawQuery = q.Encode()
				}
				zulipDMURL = parsedURL.String()
			}
			if zulipDMAuth == "" {
				log.Printf("warning: Zulip DM target %s requires auth in URL (e.g., https://user:apikey@host/api/v1/messages?to=1,2,3)", key)
				continue
			}
			if len(zulipDMRecipients) == 0 {
				log.Printf("warning: Zulip DM target %s requires 'to' parameter with user IDs (e.g., ?to=8,9,11)", key)
				continue
			}
		}

		t := target{
			Name:              pathIdentifier,
			Path:              path,
			URL:               telegramURL,
			Type:              targetType,
			Identifier:        pathIdentifier,
			ChatID:            chatID,
			ZulipDMAuth:       zulipDMAuth,
			ZulipDMRecipients: zulipDMRecipients,
		}
		if targetType == TargetZulipDM {
			t.URL = zulipDMURL
		}

		targets = append(targets, t)
	}

	return targets
}

func forwardRequest(w http.ResponseWriter, r *http.Request, t target) {
	if debugMode {
		log.Printf("[DEBUG] Received request on forward handler (%s): %s %s", t.Name, r.Method, r.URL.Path)
	}
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
	case TargetZulipDM:
		forwardToZulipDM(w, r, t, fizzy)
		return
	case TargetGoogleChat:
		newBody, translateErr = translateToGoogleChat(fizzy)
	case TargetGotify:
		newBody, translateErr = translateToGotify(fizzy)
	case TargetTelegram:
		newBody, translateErr = translateToTelegram(fizzy, t.ChatID)
	default:
		newBody = body
	}

	if translateErr != nil {
		log.Printf("translation error for %s: %v", t.Name, translateErr)
		http.Error(w, "translation failed", http.StatusInternalServerError)
		return
	}

	// Create new request to destination
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
	msg := buildZulipMessage(f)
	subjectTitle := resolveSubjectTitle(f)
	payload := ZulipPayload{
		Content: msg,
		Topic:   subjectTitle,
	}
	return json.Marshal(payload)
}

func translateToGoogleChat(f FizzyPayload) ([]byte, error) {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Someone"
	}
	verb, emoji := prettyAction(f)

	finalURL := resolveFizzyURL(f)

	subjectTitle := resolveSubjectTitle(f)

	headerSubtitle := fmt.Sprintf("%s %s", actor, verb)

	header := CardHeader{
		Title:    subjectTitle,
		Subtitle: headerSubtitle,
		ImageURL: "",
	}

	var widgets []Widget

	if f.Eventable.Body.PlainText != "" {
		widgets = append(widgets, Widget{
			TextParagraph: &TextParagraph{
				Text: f.Eventable.Body.PlainText,
			},
		})
	} else if f.Card != nil && f.Card.Description != "" {
		widgets = append(widgets, Widget{
			TextParagraph: &TextParagraph{
				Text: f.Card.Description,
			},
		})
	}

	// Card Details Section
	var detailsText []string
	if f.Card != nil {
		if f.Card.Number != 0 {
			if f.Card.Title != "" {
				detailsText = append(detailsText, fmt.Sprintf("<b>Card:</b> %s (#%d)", f.Card.Title, f.Card.Number))
			} else {
				detailsText = append(detailsText, fmt.Sprintf("<b>Card #:</b> %d", f.Card.Number))
			}
		}
		if f.Card.Status != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Status:</b> %s", f.Card.Status))
		}
		if f.Card.Column != nil && f.Card.Column.Name != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Column:</b> %s", f.Card.Column.Name))
		}
		if len(f.Card.Assignees) > 0 {
			names := make([]string, len(f.Card.Assignees))
			for i, u := range f.Card.Assignees {
				names[i] = u.Name
			}
			detailsText = append(detailsText, fmt.Sprintf("<b>Assignees:</b> %s", strings.Join(names, ", ")))
		}
		if len(f.Card.Tags) > 0 {
			detailsText = append(detailsText, fmt.Sprintf("<b>Tags:</b> %s", strings.Join(f.Card.Tags, ", ")))
		}
	} else if f.Eventable.Card != nil {
		if f.Eventable.Number != 0 {
			detailsText = append(detailsText, fmt.Sprintf("<b>Card #:</b> %d", f.Eventable.Number))
		}
		if f.Eventable.Card.Status != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Status:</b> %s", f.Eventable.Card.Status))
		}
		if f.Eventable.Card.Column != nil && f.Eventable.Card.Column.Name != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Column:</b> %s", f.Eventable.Card.Column.Name))
		}
		if len(f.Eventable.Card.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Card.Assignees))
			for i, u := range f.Eventable.Card.Assignees {
				names[i] = u.Name
			}
			detailsText = append(detailsText, fmt.Sprintf("<b>Assignees:</b> %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Card.Tags) > 0 {
			detailsText = append(detailsText, fmt.Sprintf("<b>Tags:</b> %s", strings.Join(f.Eventable.Card.Tags, ", ")))
		}
	} else {
		// Fallback: Eventable itself IS the card (Card events)
		if f.Eventable.Number != 0 {
			if f.Eventable.Title != "" {
				detailsText = append(detailsText, fmt.Sprintf("<b>Card:</b> %s (#%d)", f.Eventable.Title, f.Eventable.Number))
			} else {
				detailsText = append(detailsText, fmt.Sprintf("<b>Card #:</b> %d", f.Eventable.Number))
			}
		}
		if f.Eventable.Status != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Status:</b> %s", f.Eventable.Status))
		}
		if f.Eventable.Column != nil && f.Eventable.Column.Name != "" {
			detailsText = append(detailsText, fmt.Sprintf("<b>Column:</b> %s", f.Eventable.Column.Name))
		}
		if len(f.Eventable.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Assignees))
			for i, u := range f.Eventable.Assignees {
				names[i] = u.Name
			}
			detailsText = append(detailsText, fmt.Sprintf("<b>Assignees:</b> %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Tags) > 0 {
			detailsText = append(detailsText, fmt.Sprintf("<b>Tags:</b> %s", strings.Join(f.Eventable.Tags, ", ")))
		}
	}

	if len(detailsText) > 0 {
		widgets = append(widgets, Widget{
			TextParagraph: &TextParagraph{
				Text: strings.Join(detailsText, "<br>"),
			},
		})
	}

	if f.Board.Name != "" && subjectTitle != f.Board.Name {
		widgets = append(widgets, Widget{
			DecoratedText: &DecoratedText{
				TopLabel:  "Board",
				Text:      f.Board.Name,
				StartIcon: &Icon{KnownIcon: "TICKET"},
			},
		})
	}

	widgets = append(widgets, Widget{
		ButtonList: &ButtonList{
			Buttons: []Button{
				{
					Text: "View in Fizzy",
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

	fallbackText := fmt.Sprintf("%s %s %s: %s", emoji, actor, verb, subjectTitle)

	payload := GoogleChatPayload{
		Text:    fallbackText,
		CardsV2: []CardV2{card},
	}
	return json.Marshal(payload)
}

func translateToGotify(f FizzyPayload) ([]byte, error) {
	msg := buildMessage(f)
	verb, _ := prettyAction(f)
	actor := f.Creator.Name
	if actor == "" {
		actor = "Someone"
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

func translateToTelegram(f FizzyPayload, chatID string) ([]byte, error) {
	msg := buildTelegramMessage(f)
	payload := TelegramPayload{
		ChatID:    chatID,
		Text:      msg,
		ParseMode: "MarkdownV2",
	}
	return json.Marshal(payload)
}

func buildTelegramMessage(f FizzyPayload) string {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Someone"
	}

	verb, emoji := prettyAction(f)

	subject := f.Eventable.Title
	if subject == "" {
		if f.Card != nil && f.Card.Title != "" {
			subject = f.Card.Title
		} else if f.Eventable.Card != nil && f.Eventable.Card.Title != "" {
			subject = f.Eventable.Card.Title
		} else if f.Eventable.Parent != nil && f.Eventable.Parent.Title != "" {
			subject = f.Eventable.Parent.Title
		} else if f.Board.Name != "" {
			subject = f.Board.Name
		} else {
			subject = "Fizzy Notification"
		}
	}

	var body string
	if f.Eventable.Body.PlainText != "" {
		body = f.Eventable.Body.PlainText
	}

	urlStr := resolveFizzyURL(f)

	if subject == f.Board.Name || subject == "Fizzy Notification" {
		rawURL := f.Eventable.URL
		if rawURL == "" {
			rawURL = f.URL
		}
		if rawURL == "" {
			rawURL = f.Eventable.ReactionsURL
		}

		if strings.Contains(rawURL, "/cards/") {
			parts := strings.Split(rawURL, "/cards/")
			if len(parts) > 1 {
				sub := parts[1]
				idPart := ""
				for _, r := range sub {
					if r >= '0' && r <= '9' {
						idPart += string(r)
					} else {
						break
					}
				}
				if idPart != "" {
					subject = fmt.Sprintf("Card \\#%s", idPart)
				}
			}
		}
	}

	actor = escapeTelegramMarkdownV2(actor)
	subject = escapeTelegramMarkdownV2(subject)
	body = escapeTelegramMarkdownV2(body)
	verb = escapeTelegramMarkdownV2(verb)

	var sb strings.Builder

	hideSubject := false
	if f.Action == "comment_created" && strings.Contains(subject, "Card \\\\#") {
		hideSubject = true
	}

	if hideSubject {
		sb.WriteString(fmt.Sprintf("%s *%s* %s", emoji, actor, verb))
	} else {
		sb.WriteString(fmt.Sprintf("%s *%s* %s: %s", emoji, actor, verb, subject))
	}

	if body != "" {
		sb.WriteString("\n\n")
		sb.WriteString(body)
	}

	if f.Board.Name != "" && f.Board.Name != subject {
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("Board: %s", escapeTelegramMarkdownV2(f.Board.Name)))
	}

	// Add Card Details
	var details []string
	if f.Card != nil {
		if f.Card.Number != 0 {
			if f.Card.Title != "" {
				details = append(details, fmt.Sprintf("Card: %s (\\#%d)", escapeTelegramMarkdownV2(f.Card.Title), f.Card.Number))
			} else {
				details = append(details, fmt.Sprintf("Card \\#%d", f.Card.Number))
			}
		}
		if f.Card.Status != "" {
			details = append(details, fmt.Sprintf("Status: %s", escapeTelegramMarkdownV2(f.Card.Status)))
		}
		if f.Card.Column != nil && f.Card.Column.Name != "" {
			details = append(details, fmt.Sprintf("Column: %s", escapeTelegramMarkdownV2(f.Card.Column.Name)))
		}
		if len(f.Card.Assignees) > 0 {
			names := make([]string, len(f.Card.Assignees))
			for i, u := range f.Card.Assignees {
				names[i] = escapeTelegramMarkdownV2(u.Name)
			}
			details = append(details, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Card.Tags) > 0 {
			cleanTags := make([]string, len(f.Card.Tags))
			for i, tag := range f.Card.Tags {
				cleanTags[i] = escapeTelegramMarkdownV2(tag)
			}
			details = append(details, fmt.Sprintf("Tags: %s", strings.Join(cleanTags, ", ")))
		}
	} else if f.Eventable.Card != nil {
		if f.Eventable.Number != 0 {
			details = append(details, fmt.Sprintf("Card \\#%d", f.Eventable.Number))
		}
		if f.Eventable.Card.Status != "" {
			details = append(details, fmt.Sprintf("Status: %s", escapeTelegramMarkdownV2(f.Eventable.Card.Status)))
		}
		if f.Eventable.Card.Column != nil && f.Eventable.Card.Column.Name != "" {
			details = append(details, fmt.Sprintf("Column: %s", escapeTelegramMarkdownV2(f.Eventable.Card.Column.Name)))
		}
		if len(f.Eventable.Card.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Card.Assignees))
			for i, u := range f.Eventable.Card.Assignees {
				names[i] = escapeTelegramMarkdownV2(u.Name)
			}
			details = append(details, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Card.Tags) > 0 {
			cleanTags := make([]string, len(f.Eventable.Card.Tags))
			for i, tag := range f.Eventable.Card.Tags {
				cleanTags[i] = escapeTelegramMarkdownV2(tag)
			}
			details = append(details, fmt.Sprintf("Tags: %s", strings.Join(cleanTags, ", ")))
		}
	} else {
		// Fallback: Eventable itself IS the card
		if f.Eventable.Number != 0 {
			if f.Eventable.Title != "" {
				details = append(details, fmt.Sprintf("Card: %s (\\#%d)", escapeTelegramMarkdownV2(f.Eventable.Title), f.Eventable.Number))
			} else {
				details = append(details, fmt.Sprintf("Card \\#%d", f.Eventable.Number))
			}
		}
		if f.Eventable.Status != "" {
			details = append(details, fmt.Sprintf("Status: %s", escapeTelegramMarkdownV2(f.Eventable.Status)))
		}
		if f.Eventable.Column != nil && f.Eventable.Column.Name != "" {
			details = append(details, fmt.Sprintf("Column: %s", escapeTelegramMarkdownV2(f.Eventable.Column.Name)))
		}
		if len(f.Eventable.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Assignees))
			for i, u := range f.Eventable.Assignees {
				names[i] = escapeTelegramMarkdownV2(u.Name)
			}
			details = append(details, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Tags) > 0 {
			cleanTags := make([]string, len(f.Eventable.Tags))
			for i, tag := range f.Eventable.Tags {
				cleanTags[i] = escapeTelegramMarkdownV2(tag)
			}
			details = append(details, fmt.Sprintf("Tags: %s", strings.Join(cleanTags, ", ")))
		}
	}
	if len(details) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(strings.Join(details, "\n"))
	}

	sb.WriteString(fmt.Sprintf("\n\n[View in Fizzy](%s)", urlStr))

	return sb.String()
}

func escapeTelegramMarkdownV2(text string) string {
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := text
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

// buildMessage creates a human-readable string from the Fizzy payload.
func buildMessage(f FizzyPayload) string {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Someone"
	}

	verb, emoji := prettyAction(f)

	// Determine Subject (Title)
	subject := resolveSubjectTitle(f)

	// Body Content
	var body string
	if f.Eventable.Body.PlainText != "" {
		body = fmt.Sprintf("> %s", f.Eventable.Body.PlainText)
	}

	// Extras (Board Name, etc.)
	var extras []string
	if f.Board.Name != "" && subject != f.Board.Name {
		extras = append(extras, fmt.Sprintf("Board: %s", f.Board.Name))
	}

	if f.Card != nil {
		if f.Card.Number != 0 {
			if f.Card.Title != "" {
				extras = append(extras, fmt.Sprintf("Card: %s (#%d)", f.Card.Title, f.Card.Number))
			} else {
				extras = append(extras, fmt.Sprintf("Card #: %d", f.Card.Number))
			}
		}
		if f.Card.Status != "" {
			extras = append(extras, fmt.Sprintf("Status: %s", f.Card.Status))
		}
		if f.Card.Column != nil && f.Card.Column.Name != "" {
			extras = append(extras, fmt.Sprintf("Column: %s", f.Card.Column.Name))
		}
		if len(f.Card.Assignees) > 0 {
			names := make([]string, len(f.Card.Assignees))
			for i, u := range f.Card.Assignees {
				names[i] = u.Name
			}
			extras = append(extras, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Card.Tags) > 0 {
			extras = append(extras, fmt.Sprintf("Tags: %s", strings.Join(f.Card.Tags, ", ")))
		}
		// Description if available and not already used as body
		if f.Eventable.Body.PlainText == "" && f.Card.Description != "" {
			body = fmt.Sprintf("> %s", f.Card.Description)
		}
	} else if f.Eventable.Card != nil {
		if f.Eventable.Number != 0 {
			extras = append(extras, fmt.Sprintf("Card #: %d", f.Eventable.Number))
		}
		if f.Eventable.Card.Status != "" {
			extras = append(extras, fmt.Sprintf("Status: %s", f.Eventable.Card.Status))
		}
		if f.Eventable.Card.Column != nil && f.Eventable.Card.Column.Name != "" {
			extras = append(extras, fmt.Sprintf("Column: %s", f.Eventable.Card.Column.Name))
		}
		if len(f.Eventable.Card.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Card.Assignees))
			for i, u := range f.Eventable.Card.Assignees {
				names[i] = u.Name
			}
			extras = append(extras, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Card.Tags) > 0 {
			extras = append(extras, fmt.Sprintf("Tags: %s", strings.Join(f.Eventable.Card.Tags, ", ")))
		}
	} else {
		// Fallback: Eventable itself IS the card
		if f.Eventable.Number != 0 {
			if f.Eventable.Title != "" {
				extras = append(extras, fmt.Sprintf("Card: %s (#%d)", f.Eventable.Title, f.Eventable.Number))
			} else {
				extras = append(extras, fmt.Sprintf("Card #: %d", f.Eventable.Number))
			}
		}
		if f.Eventable.Status != "" {
			extras = append(extras, fmt.Sprintf("Status: %s", f.Eventable.Status))
		}
		if f.Eventable.Column != nil && f.Eventable.Column.Name != "" {
			extras = append(extras, fmt.Sprintf("Column: %s", f.Eventable.Column.Name))
		}
		if len(f.Eventable.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Assignees))
			for i, u := range f.Eventable.Assignees {
				names[i] = u.Name
			}
			extras = append(extras, fmt.Sprintf("Assignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Tags) > 0 {
			extras = append(extras, fmt.Sprintf("Tags: %s", strings.Join(f.Eventable.Tags, ", ")))
		}
		// Description if available and not already used as body
		if f.Eventable.Body.PlainText == "" && f.Eventable.Description != "" {
			body = fmt.Sprintf("> %s", f.Eventable.Description)
		}
	}

	// Determine URL
	urlStr := resolveFizzyURL(f)

	var sb strings.Builder

	hideSubject := false
	if f.Action == "comment_created" && strings.HasPrefix(subject, "Card #") {
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

	sb.WriteString(fmt.Sprintf("\n\n[View in Fizzy](%s)", urlStr))

	return sb.String()
}

func buildZulipMessage(f FizzyPayload) string {
	actor := f.Creator.Name
	if actor == "" {
		actor = "Someone"
	}

	verb, emoji := prettyAction(f)
	subject := resolveSubjectTitle(f)
	body := f.Eventable.Body.PlainText
	urlStr := resolveFizzyURL(f)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s %s %s: %s", emoji, actor, verb, subject))

	if body != "" {
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("> %s", body))
	}

	if f.Board.Name != "" && subject != f.Board.Name {
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("Board: %s", f.Board.Name))
	}

	if f.Card != nil {
		if f.Card.Number != 0 {
			if f.Card.Title != "" {
				sb.WriteString(fmt.Sprintf("\nCard: %s (#%d)", f.Card.Title, f.Card.Number))
			} else {
				sb.WriteString(fmt.Sprintf("\nCard #: %d", f.Card.Number))
			}
		}
		if f.Card.Status != "" {
			sb.WriteString(fmt.Sprintf("\nStatus: %s", f.Card.Status))
		}
		if f.Card.Column != nil && f.Card.Column.Name != "" {
			sb.WriteString(fmt.Sprintf("\nColumn: %s", f.Card.Column.Name))
		}
		if len(f.Card.Assignees) > 0 {
			names := make([]string, len(f.Card.Assignees))
			for i, u := range f.Card.Assignees {
				names[i] = u.Name
			}
			sb.WriteString(fmt.Sprintf("\nAssignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Card.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("\nTags: %s", strings.Join(f.Card.Tags, ", ")))
		}
		if body == "" && f.Card.Description != "" {
			sb.WriteString("\n\n")
			sb.WriteString(fmt.Sprintf("> %s", f.Card.Description))
		}
	} else if f.Eventable.Card != nil {
		if f.Eventable.Number != 0 {
			sb.WriteString(fmt.Sprintf("\nCard #: %d", f.Eventable.Number))
		}
		if f.Eventable.Card.Status != "" {
			sb.WriteString(fmt.Sprintf("\nStatus: %s", f.Eventable.Card.Status))
		}
		if f.Eventable.Card.Column != nil && f.Eventable.Card.Column.Name != "" {
			sb.WriteString(fmt.Sprintf("\nColumn: %s", f.Eventable.Card.Column.Name))
		}
		if len(f.Eventable.Card.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Card.Assignees))
			for i, u := range f.Eventable.Card.Assignees {
				names[i] = u.Name
			}
			sb.WriteString(fmt.Sprintf("\nAssignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Card.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("\nTags: %s", strings.Join(f.Eventable.Card.Tags, ", ")))
		}
	} else {
		// Fallback: Eventable itself IS the card
		if f.Eventable.Number != 0 {
			if f.Eventable.Title != "" {
				sb.WriteString(fmt.Sprintf("\nCard: %s (#%d)", f.Eventable.Title, f.Eventable.Number))
			} else {
				sb.WriteString(fmt.Sprintf("\nCard #: %d", f.Eventable.Number))
			}
		}
		if f.Eventable.Status != "" {
			sb.WriteString(fmt.Sprintf("\nStatus: %s", f.Eventable.Status))
		}
		if f.Eventable.Column != nil && f.Eventable.Column.Name != "" {
			sb.WriteString(fmt.Sprintf("\nColumn: %s", f.Eventable.Column.Name))
		}
		if len(f.Eventable.Assignees) > 0 {
			names := make([]string, len(f.Eventable.Assignees))
			for i, u := range f.Eventable.Assignees {
				names[i] = u.Name
			}
			sb.WriteString(fmt.Sprintf("\nAssignees: %s", strings.Join(names, ", ")))
		}
		if len(f.Eventable.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("\nTags: %s", strings.Join(f.Eventable.Tags, ", ")))
		}
		if body == "" && f.Eventable.Description != "" {
			sb.WriteString("\n\n")
			sb.WriteString(fmt.Sprintf("> %s", f.Eventable.Description))
		}
	}

	sb.WriteString(fmt.Sprintf("\n\n[View in Fizzy](%s)", urlStr))

	return sb.String()
}

func resolveSubjectTitle(f FizzyPayload) string {
	subject := f.Eventable.Title
	if subject == "" {
		if f.Card != nil && f.Card.Title != "" {
			subject = f.Card.Title
		} else if f.Eventable.Card != nil && f.Eventable.Card.Title != "" {
			subject = f.Eventable.Card.Title
		} else if f.Eventable.Parent != nil && f.Eventable.Parent.Title != "" {
			subject = f.Eventable.Parent.Title
		} else if f.Board.Name != "" {
			subject = f.Board.Name
		} else {
			subject = "Fizzy Notification"
		}
	}

	return subject
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
		return "commented", "💬"
	case "card_created":
		return "created a card", "🃏"
	case "card_published":
		return "published a card", "📢"
	case "card_reopened":
		return "reopened the card", "🔄"
	case "card_board_changed":
		return "changed the card's board", "📋"
	case "card_moved":
		if f.Column != nil && f.Column.Name != "" {
			if f.Reason == "inactivity" {
				return fmt.Sprintf("moved the card to **%s** due to inactivity", f.Column.Name), "💤"
			}
			return fmt.Sprintf("moved the card to **%s**", f.Column.Name), "🚚"
		}
		return "moved the card", "🚚"
	case "card_assigned":
		if f.Assignee != nil && f.Assignee.Name != "" {
			return fmt.Sprintf("assigned the card to **%s**", f.Assignee.Name), "👤"
		}
		return "assigned the card to someone", "👤"
	case "card_unassigned":
		return "unassigned the card", "👤"
	case "card_postponed":
		return "postponed the card", "💤"
	case "card_closed":
		if f.Column != nil && strings.EqualFold(f.Column.Name, "Done") {
			return "completed the card", "✅"
		}
		return "closed the card", "✅"
	case "card_triaged":
		column := "somewhere"
		if f.Card != nil && f.Card.Column != nil && f.Card.Column.Name != "" {
			column = f.Card.Column.Name
		} else if f.Eventable.Card != nil && f.Eventable.Card.Column != nil && f.Eventable.Card.Column.Name != "" {
			column = f.Eventable.Card.Column.Name
		} else if f.Eventable.Column != nil && f.Eventable.Column.Name != "" {
			column = f.Eventable.Column.Name
		}
		return fmt.Sprintf("moved the card to **%s**", column), "🚚"
	case "card_sent_back_to_triage":
		return "moved the card back to **Maybe?**", "↩️"
	case "card_auto_postponed":
		return "moved to **Not Now** due to inactivity", "💤"
	case "card_resumed":
		return "resumed the card", "▶️"
	case "card_title_changed":
		return "renamed the card", "✏️"
	case "card_archived":

		// Check for "Done" or "Postponed" if possible...
		if f.Column != nil {
			if strings.EqualFold(f.Column.Name, "Done") {
				return "completed the card", "✅"
			}
			if strings.EqualFold(f.Column.Name, "Postponed") || strings.EqualFold(f.Column.Name, "Not Now") {
				return "postponed the card", "😴"
			}
		}
		return "archived the card", "📦"
	default:
		return strings.ReplaceAll(action, "_", " "), "📢"
	}
}

// --- Helpers ---

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

func forwardToZulipDM(w http.ResponseWriter, r *http.Request, t target, fizzy FizzyPayload) {
	msg := buildZulipMessage(fizzy)
	client := &http.Client{Timeout: 10 * time.Second}

	var recipients []interface{}
	for _, s := range t.ZulipDMRecipients {
		if id, err := strconv.Atoi(s); err == nil {
			recipients = append(recipients, id)
		} else {
			recipients = append(recipients, s)
		}
	}
	recipientsJSON, err := json.Marshal(recipients)
	if err != nil {
		log.Printf("error marshaling recipients: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	formData := url.Values{}
	formData.Set("type", "direct")
	formData.Set("to", string(recipientsJSON))
	formData.Set("content", msg)

	log.Printf("Forwarding to %s (zulip-dm): recipients=%s content=%s", t.Name, string(recipientsJSON), msg)

	req, err := http.NewRequestWithContext(r.Context(), "POST", t.URL, strings.NewReader(formData.Encode()))
	if err != nil {
		http.Error(w, "failed to build forward request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Fizzy-Proxy/1.0")

	authParts := strings.SplitN(t.ZulipDMAuth, ":", 2)
	if len(authParts) == 2 {
		req.SetBasicAuth(authParts[0], authParts[1])
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("forward error (%s): %v", t.Name, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

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
