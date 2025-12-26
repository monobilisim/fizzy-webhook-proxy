# Fizzy Webhook Proxy

A middleware service that receives webhook requests from Fizzy and forwards them to platforms like Zulip, Google Chat, and Gotify in a proper format.

Standard Fizzy notifications can be complex or incomplete. This service intercepts messages, cleans them up, organizes headers, and fixes broken comment links.

## Features

- **Rich Notifications:** Card views for Google Chat and Zulip, clean Markdown format for Gotify.
- **Smart Links:** Fixes comment links, redirects to the relevant card and comment ID.
- **Deduplication:** Prevents the same event from being reported multiple times accidentally.
- **Easy Setup:** Runs as a single binary file.

## Installation (Binary)

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

## Configuration

The service requires configuration in the `/etc/default/fizzy-webhook-proxy` file (or `.env` file):

```bash
sudo vim /etc/default/fizzy-webhook-proxy
```

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

## Starting the Service

```bash
sudo cp deployment/fizzy-webhook-proxy.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now fizzy-webhook-proxy
```

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

## Usage

Add **Webhooks** from Fizzy project settings. We recommend selecting the following events:

- `card_created`, `card_published`
- `comment_created`
- `card_moved`, `card_board_changed`
- `card_assigned`, `card_unassigned`
- `card_closed`, `card_reopened`, `card_archived`
- `card_postponed`, `card_sent_back_to_triage`

Enter the proxy address in the URL field:
- For Zulip: `https://your-proxy-address.com/zulip`
- For Google Chat: `https://your-proxy-address.com/google-chat`
- For Gotify: `https://your-proxy-address.com/gotify`

## To-Do

- [ ] Add Telegram support
- [ ] Add Slack support
- [ ] Expand unit tests
