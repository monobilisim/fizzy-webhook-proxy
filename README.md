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

```bash
sudo wget https://raw.githubusercontent.com/monobilisim/fizzy-webhook-proxy/refs/heads/main/deployment/fizzy-webhook-proxy -O /etc/default/fizzy-webhook-proxy
```

Edit `/etc/default/fizzy-webhook-proxy`:

```env
PORT=8080
TOKEN=your_secret_token

# Webhook targets: {IDENTIFIER}_URL
PERSONAL_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=...&stream=notifications&topic=personal
TEST_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=...&stream=notifications&topic=test
```

This creates endpoints:
- `/{TOKEN}/personal`
- `/{TOKEN}/test`

### Type Auto-Detection

| URL Pattern | Detected Type |
|-------------|---------------|
| `chat.googleapis.com` | google-chat |
| `slack_incoming` | zulip |
| `/message?token` | gotify |

### Identifier Naming

- `PERSONAL_URL` → `/{TOKEN}/personal`
- `STATUS_PAGE_URL` → `/{TOKEN}/status-page` (underscores become hyphens)

### Security

- **TOKEN is required** - URLs are unpredictable
- Browser shows `/identifier` only, full path with token shown in service logs

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
