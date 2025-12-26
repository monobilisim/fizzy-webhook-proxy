[![Contributors](https://img.shields.io/github/contributors/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/graphs/contributors)
[![Forks](https://img.shields.io/github/forks/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/network/members)
[![Stargazers](https://img.shields.io/github/stars/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/stargazers)
[![Issues](https://img.shields.io/github/issues/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/issues)
[![GPL License](https://img.shields.io/github/license/monobilisim/fizzy-webhook-proxy.svg?style=for-the-badge)](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/LICENSE)

[![Readme in English](https://img.shields.io/badge/Readme-English-blue)](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/README.md)

<div align="center">
  <a href="https://mono.net.tr/">
    <img src="https://monobilisim.com.tr/images/mono-bilisim.svg" width="340" style="max-width: 100%;">
  </a>

## Fizzy Webhook Proxy

</div>

**Fizzy Webhook Proxy** is a middleware service that receives webhook requests from Fizzy and forwards them to platforms like Zulip, Google Chat, and Gotify in a proper format.

Standard Fizzy notifications can be complex or incomplete. This service intercepts messages, cleans them up, organizes headers, and fixes broken comment links.

---

## Table of Contents

- [Table of Contents](#table-of-contents)
- [Features](#features)
- [Installation](#installation)
- [Configuration](#configuration)
  - [Single-Tenant Configuration](#single-tenant-configuration-simple-setup)
  - [Multi-Tenant Configuration](#multi-tenant-configuration-multiple-boards)
- [Starting the Service](#starting-the-service)
- [Usage](#usage)
  - [Single-Tenant Setup](#single-tenant-setup)
  - [Multi-Tenant Setup](#multi-tenant-setup)
- [Known Limitations](#known-limitations)
- [To-Do](#to-do)
- [License](#license)

---

## Features

- **Rich Notifications:** Card views for Google Chat and Zulip, clean Markdown format for Gotify.
- **Smart Links:** Fixes comment links, redirects to the relevant card and comment ID.
- **Deduplication:** Prevents the same event from being reported multiple times accidentally.
- **Easy Setup:** Runs as a single binary file.

---

## Installation

You can download the latest version from the [GitHub Releases](https://github.com/monobilisim/fizzy-webhook-proxy/releases) page.

```bash
# Download the binary and make it executable
wget https://github.com/monobilisim/fizzy-webhook-proxy/releases/latest/download/fizzy-webhook-proxy
chmod +x fizzy-webhook-proxy
sudo mv fizzy-webhook-proxy /usr/local/bin/
```

Alternatively, if you want to compile from source:
```bash
sudo make install
```

---

## Configuration

The service requires configuration in the `/etc/default/fizzy-webhook-proxy` file (or `.env` file):

```bash
sudo vim /etc/default/fizzy-webhook-proxy
```

### Single-Tenant Configuration (Simple Setup)

For a single board or project, use the basic configuration:

```env
# Service Port
PORT=8080

# Webhook URLs (You can leave unused ones empty)
ZULIP_WEBHOOK_URL=https://zulip.example.com/api/v1/external/slack_incoming?api_key=your_api_key&stream=your_stream&topic=your_topic
GOOGLE_CHAT_WEBHOOK_URL=https://chat.googleapis.com/v1/spaces/...
GOTIFY_WEBHOOK_URL=https://gotify.example.com/message?token=...

# Fizzy Link Fixing
FIZZY_ROOT_URL=https://fizzy.example.com
```

**Webhook paths:**
- Zulip: `https://your-proxy-address.com/zulip`
- Google Chat: `https://your-proxy-address.com/google-chat`
- Gotify: `https://your-proxy-address.com/gotify`

### Multi-Tenant Configuration (Multiple Boards)

For multiple boards with separate webhook destinations, use the board-specific configuration:

```env
# Service Port
PORT=8080

# Pattern: BOARD_{IDENTIFIER}_{TYPE}_URL
# Creates paths: /{type}/board-{identifier}

# Engineering Team Board
BOARD_ENG_ZULIP_URL=https://zulip.company.com/api/v1/external/slack_incoming?stream=engineering&api_key=key1
BOARD_ENG_GOOGLE_CHAT_URL=https://chat.googleapis.com/v1/spaces/eng-space/messages?key=key2

# Product Team Board
BOARD_PRODUCT_ZULIP_URL=https://zulip.company.com/api/v1/external/slack_incoming?stream=product&api_key=key3

# Support Team Board (Multiple webhook types)
BOARD_SUPPORT_ZULIP_URL=https://zulip.company.com/api/v1/external/slack_incoming?stream=support&api_key=key4
BOARD_SUPPORT_GOTIFY_URL=https://gotify.company.com/message?token=support_token

# Fizzy Link Fixing
FIZZY_ROOT_URL=https://fizzy.example.com
```

**Webhook paths for multi-tenant setup:**
- Engineering Board → Zulip: `https://your-proxy-address.com/zulip/board-eng`
- Engineering Board → Google Chat: `https://your-proxy-address.com/google-chat/board-eng`
- Product Board → Zulip: `https://your-proxy-address.com/zulip/board-product`
- Support Board → Zulip: `https://your-proxy-address.com/zulip/board-support`
- Support Board → Gotify: `https://your-proxy-address.com/gotify/board-support`

**Notes:**
- Board identifiers can contain underscores (e.g., `MY_PROJECT`)
- Underscores in board IDs are converted to hyphens in URLs (e.g., `BOARD_MY_PROJECT_ZULIP_URL` → `/zulip/board-my-project`)
- Each board can have multiple webhook types (Zulip, Google Chat, Gotify)
- You can mix single-tenant and multi-tenant configurations

---

## Starting the Service

```bash
sudo cp deployment/fizzy-webhook-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

---

## Usage

Add **Webhooks** from Fizzy project settings. We recommend selecting the following events:

- `card_created`, `card_published`
- `comment_created`
- `card_moved`, `card_board_changed`
- `card_assigned`, `card_unassigned`
- `card_closed`, `card_reopened`, `card_archived`
- `card_postponed`, `card_sent_back_to_triage`

### Single-Tenant Setup

Enter the proxy address in the URL field:
- For Zulip: `https://your-proxy-address.com/zulip`
- For Google Chat: `https://your-proxy-address.com/google-chat`
- For Gotify: `https://your-proxy-address.com/gotify`

### Multi-Tenant Setup

For each board, enter the board-specific webhook URLs:

**Example: Engineering Board**
- For Zulip: `https://your-proxy-address.com/zulip/board-eng`
- For Google Chat: `https://your-proxy-address.com/google-chat/board-eng`

**Example: Product Board**
- For Zulip: `https://your-proxy-address.com/zulip/board-product`

The board identifier in the URL (`board-eng`, `board-product`) must match the identifier in your environment variables (`BOARD_ENG_*`, `BOARD_PRODUCT_*`).

---

## Known Limitations

Due to some data deficiencies in Fizzy's webhook infrastructure:

1.  **Card Title in Comment Notifications:**
    - Fizzy does not send the card's text title (e.g., "Login Page Error") in the `comment_created` event.
    - **Solution:** The proxy extracts the card number from the URL and displays a simple title like `[Person] commented` if the title is missing. The text title cannot be accessed without API integration.

2.  **Assignee Name in Assignment Notifications:**
    - The `card_assigned` event does not include information about *who* the card was assigned to in the payload.
    - **Solution:** The notification is displayed with a generic message like "assigned the card to someone".

3.  **Duplicate Notifications:**
    - Fizzy can sometimes send the same event (especially `card_reopened`) twice within milliseconds.
    - **Solution:** The proxy has a 2-second `deduplication` mechanism; if the same event repeats, the second one is ignored.

---

## To-Do

- [ ] Add Telegram support
- [ ] Add Slack support
- [ ] Expand unit tests

---

## License

Fizzy Webhook Proxy is licensed under GPL-3.0. See the [LICENSE](https://github.com/monobilisim/fizzy-webhook-proxy/blob/main/LICENSE) file for details.
