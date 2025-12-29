[![Contributors](https://img.shields.io/github/contributors/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/graphs/contributors)
[![Forks](https://img.shields.io/github/forks/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/network/members)
[![Stargazers](https://img.shields.io/github/stars/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/stargazers)
[![Issues](https://img.shields.io/github/issues/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/issues)
[![GPL License](https://img.shields.io/github/license/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/LICENSE)

[![Readme in English](https://img.shields.io/badge/Readme-English-blue)](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/README.md)

<div align="center">
  <a href="https://mono.tr/">
    <img src="https://monobilisim.com.tr/images/mono-bilisim.svg" width="340" style="max-width: 100%;">
  </a>

## Fizzy Webhook Proxy

</div>

**Fizzy Webhook Proxy** is a middleware service that receives webhook requests from Fizzy and forwards them to platforms like Zulip, Google Chat, and Gotify in a proper format.

Standard Fizzy notifications can be complex or incomplete. This service intercepts messages, cleans them up, organizes headers, and fixes broken comment links.

---

## Features

- **Rich Notifications:** Card views for Google Chat, clean Markdown format for Zulip and Gotify.
- **Smart Links:** Fixes comment links, redirects to the relevant card and comment ID.
- **Deduplication:** Prevents the same event from being reported multiple times.
- **Type Auto-Detection:** Automatically detects webhook type from URL.
- **Token Authentication:** Required URL prefix for security.

---

## Installation

```bash
sudo wget https://github.com/monobilisim/fizzy-webhook-proxy/releases/latest/download/fizzy-webhook-proxy -O /usr/local/bin/fizzy-webhook-proxy
sudo chmod +x /usr/local/bin/fizzy-webhook-proxy
```

Or compile from source:
```bash
sudo make install
```

---

## Configuration

You can configure the proxy in two ways:

1.  **System-wide:** Create a file at `/etc/default/fizzy-webhook-proxy`.
2.  **Locally:** Create a `.env` file in the same directory as the executable.

The service will load environment variables from these files at startup.

### Environment Variables

A comprehensive reference for all environment variables can be found in the `.env.example` file. Here is an example configuration with all supported target types:

```env
# Required: HTTP server port
PORT=8080

# Required: Authentication token for URL prefix
# All webhook URLs will be prefixed with /{TOKEN}/
# Example: TOKEN=abc123 means URLs become /abc123/zulip instead of /zulip
TOKEN=your_secret_token

# Optional: Enable debug logging
# DEBUG=true

# =============================================================================
# Webhook Targets
# =============================================================================
# Pattern: {IDENTIFIER}_URL
#
# Type auto-detection from URL:
#   - chat.googleapis.com  -> google-chat
#   - slack_incoming       -> zulip
#   - /message?token       -> gotify
#
# URL path will be: /{TOKEN}/{identifier}
# Underscores in identifier are converted to hyphens (STATUS_PAGE -> status-page)

# Example: Zulip
ZULIP_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=your_api_key&stream=your_stream&topic=your_topic

# Example: Google Chat
GOOGLE_CHAT_URL=https://chat.googleapis.com/v1/spaces/SPACE_ID/messages?key=your_key&token=your_token

# Example: Gotify
GOTIFY_URL=https://gotify.example.com/message?token=your_token

# You can also use multiple targets for different teams/projects
# PERSONAL_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=...&stream=notifications&topic=personal
# TEST_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=...&stream=notifications&topic=test

# =============================================================================
# Fizzy Link Configuration (Optional)
# =============================================================================
# FIZZY_ROOT_URL=https://fizzy.example.com
# FIZZY_ACCOUNT_SLUG=your_account_slug
```

### Endpoints

Based on the example above, the following endpoints would be created:

-   `http://localhost:8080/your_secret_token/zulip`
-   `http://localhost:8080/your_secret_token/google-chat`
-   `http://localhost:8080/your_secret_token/gotify`

You can then use these URLs in your Fizzy webhook settings.

### Type Auto-Detection

| URL Pattern           | Detected Type |
| --------------------- | ------------- |
| `chat.googleapis.com` | google-chat   |
| `slack_incoming`      | zulip         |
| `/message?token`      | gotify        |

### Identifier Naming

-   `PERSONAL_URL` → `/{TOKEN}/personal`
-   `STATUS_PAGE_URL` → `/{TOKEN}/status-page` (underscores become hyphens)

### Security

-   **TOKEN is required** - URLs are unpredictable
-   Browser shows `/identifier` only, full path with token shown in service logs

---

## Starting the Service

```bash
sudo wget https://raw.githubusercontent.com/monobilisim/fizzy-webhook-proxy/refs/heads/main/deployment/fizzy-webhook-proxy.service -O /etc/systemd/system/fizzy-webhook-proxy.service
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

---

## Usage

Add webhooks from Fizzy project settings with these events:

- `card_created`, `card_published`
- `comment_created`
- `card_moved`, `card_board_changed`
- `card_assigned`, `card_unassigned`
- `card_closed`, `card_reopened`, `card_archived`
- `card_postponed`, `card_sent_back_to_triage`

Enter: `https://your-proxy-address.com/{TOKEN}/{identifier}`

---

## Known Limitations

1. **Card Title in Comments:** Fizzy doesn't send card title in `comment_created` - proxy extracts card number from URL.
2. **Assignee Name:** `card_assigned` doesn't include assignee info - shows "assigned to someone".
3. **Duplicate Events:** Fizzy may send same event twice - proxy has 2-second deduplication.

---

## License

GPL-3.0 - See [LICENSE](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/LICENSE)
