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
- **Deduplication:** Prevents the same event from being reported multiple times (2-second window).
- **Type Auto-Detection:** Automatically detects webhook type from URL pattern.
- **Token Authentication:** Required URL prefix for security.
- **Multiple Targets:** Configure different webhooks for different Fizzy boards.

---

## Installation

### Binary Download

```bash
sudo wget https://github.com/monobilisim/fizzy-webhook-proxy/releases/latest/download/fizzy-webhook-proxy -O /usr/local/bin/fizzy-webhook-proxy
sudo chmod +x /usr/local/bin/fizzy-webhook-proxy
```

### Build from Source

```bash
git clone https://github.com/monobilisim/fizzy-webhook-proxy.git
cd fizzy-webhook-proxy
sudo make install
```

---

## Configuration

Configuration can be provided via:

1. **System-wide:** `/etc/default/fizzy-webhook-proxy`
2. **Local:** `.env` file in the working directory

### Quick Setup

```bash
sudo wget https://raw.githubusercontent.com/monobilisim/fizzy-webhook-proxy/main/deployment/fizzy-webhook-proxy \
  -O /etc/default/fizzy-webhook-proxy
sudo nano /etc/default/fizzy-webhook-proxy
```

---

## Environment Variables

### Required Settings

| Variable | Description | Example |
|----------|-------------|---------|
| `TOKEN` | **Required.** Security token for URL prefix. All webhook URLs will be `/{TOKEN}/{identifier}` | `my_secret_token` |
| `PORT` | HTTP server port (default: `3499`) | `3499` |

### Webhook Targets

Define targets using the pattern `{IDENTIFIER}_URL`. The identifier automatically becomes the URL path.

| Variable Pattern | URL Path | Notes |
|------------------|----------|-------|
| `ZULIP_URL` | `/{TOKEN}/zulip` | Single-word identifier |
| `IDENTIFIER1_URL` | `/{TOKEN}/identifier1` | Generic naming |
| `MY_TARGET_URL` | `/{TOKEN}/my-target` | Underscores → hyphens in URL |
| `GOOGLE_CHAT_URL` | `/{TOKEN}/google-chat` | Type auto-detected from URL |

> **Naming Rule:** Environment variable `{IDENTIFIER}_URL` creates endpoint `/{TOKEN}/{identifier}`. Underscores in the identifier become hyphens in the URL path.

**Supported Platforms (Auto-detected):**

| URL Pattern | Platform | Notes |
|-------------|----------|-------|
| Contains `slack_incoming` | Zulip | Zulip's Slack-compatible webhook |
| Contains `chat.googleapis.com` | Google Chat | Google Chat webhook |
| Contains `/message?token` | Gotify | Gotify push notification |

If auto-detection fails, set `{IDENTIFIER}_TYPE` explicitly (e.g., `ZULIP_TYPE=zulip`).

### Fizzy Link Configuration

These settings are **highly recommended** for proper link generation in notifications:

| Variable | Description | Example |
|----------|-------------|---------|
| `FIZZY_ROOT_URL` | Your Fizzy instance URL. Required for correct card/comment links. | `https://fizzy.example.com` |
| `FIZZY_ACCOUNT_SLUG` | Your Fizzy account slug. Extracted from `FIZZY_ROOT_URL` if not set. | `my-company` |

> **Important:** Without `FIZZY_ROOT_URL`, notification links may point to incorrect domains or use placeholder URLs (`fizzy.example.com`).

### Optional Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `DEBUG` | Enable verbose logging | `false` |

---

## Example Configuration

```bash
# ==============================================================================
# REQUIRED SETTINGS
# ==============================================================================

# HTTP server port (3499 = "FIZZ" on phone keypad)
PORT=3499

# Security token - all webhook URLs will be: /{TOKEN}/{identifier}
TOKEN=your_secret_token_here

# ==============================================================================
# FIZZY LINK CONFIGURATION (Recommended)
# ==============================================================================

# Your Fizzy instance URL - ensures notification links work correctly
FIZZY_ROOT_URL=https://fizzy.example.com

# Account slug (optional if FIZZY_ROOT_URL contains it)
# FIZZY_ACCOUNT_SLUG=my-company

# ==============================================================================
# WEBHOOK TARGETS
# ==============================================================================
# Pattern: {IDENTIFIER}_URL=https://...
# URL path: /{TOKEN}/{identifier} (underscores become hyphens)

# Zulip (auto-detected from "slack_incoming" in URL)
ZULIP_URL=https://chat.example.com/api/v1/external/slack_incoming?api_key=KEY&stream=fizzy&topic=notifications

# Google Chat (auto-detected from "chat.googleapis.com")
GOOGLE_CHAT_URL=https://chat.googleapis.com/v1/spaces/SPACE_ID/messages?key=KEY&token=TOKEN

# Gotify (auto-detected from "/message?token")
GOTIFY_URL=https://gotify.example.com/message?token=APP_TOKEN

# Multiple targets example
# IDENTIFIER1_URL=https://chat.example.com/api/v1/external/slack_incoming?...&stream=stream1
# IDENTIFIER2_URL=https://chat.googleapis.com/v1/spaces/SPACE_ID/messages?...
#
# Multi-word identifier example (underscores become hyphens in URL path):
# MY_TARGET_URL=https://...  → endpoint becomes: /{TOKEN}/my-target

# ==============================================================================
# OPTIONAL SETTINGS
# ==============================================================================

# Enable debug logging
# DEBUG=true
```

This configuration creates the following webhook endpoints:
- `https://your-proxy:3499/your_secret_token_here/zulip`
- `https://your-proxy:3499/your_secret_token_here/google-chat`
- `https://your-proxy:3499/your_secret_token_here/gotify`
- `https://your-proxy:3499/your_secret_token_here/identifier1` (if uncommented)
- `https://your-proxy:3499/your_secret_token_here/identifier2` (if uncommented)
- `https://your-proxy:3499/your_secret_token_here/my-target` (if uncommented, note: underscore → hyphen)

---

## Systemd Service

### Install Service

```bash
sudo wget https://raw.githubusercontent.com/monobilisim/fizzy-webhook-proxy/main/deployment/fizzy-webhook-proxy.service \
  -O /etc/systemd/system/fizzy-webhook-proxy.service
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

### Manage Service

```bash
sudo systemctl status fizzy-webhook-proxy   # Check status
sudo systemctl restart fizzy-webhook-proxy  # Restart after config change
sudo journalctl -u fizzy-webhook-proxy -f   # View logs
```

---

## Fizzy Webhook Setup

1. Go to your Fizzy project settings
2. Navigate to **Webhooks** section
3. Add a new webhook with URL: `https://your-proxy/{TOKEN}/{identifier}`
4. Select the events you want to receive:

| Event | Description |
|-------|-------------|
| `card_created` | New card created |
| `card_published` | Card published to board |
| `comment_created` | New comment on a card |
| `card_moved` | Card moved to different column |
| `card_board_changed` | Card moved to different board |
| `card_assigned` | Card assigned to someone |
| `card_unassigned` | Card unassigned |
| `card_closed` | Card closed/completed |
| `card_reopened` | Card reopened |
| `card_archived` | Card archived |
| `card_postponed` | Card postponed |
| `card_sent_back_to_triage` | Card sent back to triage |

---

## Known Limitations

| Limitation | Description | Workaround |
|------------|-------------|------------|
| Card title in comments | Fizzy doesn't send card title in `comment_created` events | Proxy extracts card number from URL |
| Assignee details | `card_assigned` doesn't include assignee name | Shows "assigned to someone" |
| Duplicate events | Fizzy may send the same event twice | 2-second deduplication window |
| Comment deep links | Direct comment links require search fallback | Links use search with comment anchor |

---

## Troubleshooting

### No notifications received

1. Check service status: `sudo systemctl status fizzy-webhook-proxy`
2. Verify webhook URL in Fizzy settings matches your configuration
3. Enable debug mode: `DEBUG=true` and check logs

### Links point to wrong domain

Set `FIZZY_ROOT_URL` to your actual Fizzy instance URL.

### Type detection fails

Set the type explicitly: `{IDENTIFIER}_TYPE=zulip|google-chat|gotify`

---

## License

GPL-3.0 - See [LICENSE](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/LICENSE)
