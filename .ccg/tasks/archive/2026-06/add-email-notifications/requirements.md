# Requirements

Implement an `email` notification type for VaultFleet.

## Scope

- Add email as a notification channel beside Telegram and webhook.
- Allow SMTP configuration from the notification settings UI.
- Allow recipients to be configured for email notifications.
- Allow subject and body templates for email messages.
- Preserve existing notification event filtering and test-send behavior.
- Store SMTP credentials encrypted through the existing notification config path.
- Redact SMTP password in API responses and preserve redacted password on update.

## Non-Goals

- No attachment support.
- No per-event template editor beyond the current notification config.
- No separate global SMTP settings table.
