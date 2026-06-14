# Notifications Spec Delta

## ADDED Requirements

### Requirement: Email Notification Channel

VaultFleet SHALL support `email` as a notification type.

#### Scenario: Send email notification

- **GIVEN** an enabled email notification config subscribed to an event
- **WHEN** the event is dispatched
- **THEN** VaultFleet sends an SMTP email rendered from the configured templates

### Requirement: SMTP Configuration

VaultFleet SHALL allow operators to configure SMTP host, port, security mode, credentials, sender, and recipients for email notification configs.

#### Scenario: Store SMTP password securely

- **GIVEN** an email notification config contains an SMTP password
- **WHEN** the config is saved
- **THEN** the password is encrypted at rest and redacted from API responses

### Requirement: Email Templates

VaultFleet SHALL allow configuring subject and body templates for email notifications.

#### Scenario: Render notification template

- **GIVEN** a template references notification fields such as title, level, agent name, timestamp, and body
- **WHEN** an email notification is sent
- **THEN** those fields are rendered into the outgoing email

### Requirement: Notification Draft Testing

VaultFleet SHALL allow operators to test a notification channel using the current form configuration before saving it.

#### Scenario: Test unsaved email configuration

- **GIVEN** an operator has entered valid SMTP, recipient, and template settings in the notification form
- **WHEN** the operator tests the current configuration
- **THEN** VaultFleet sends a test notification without persisting the draft configuration

#### Scenario: Test edited email configuration with redacted password

- **GIVEN** an existing email notification config has an encrypted SMTP password
- **AND** the operator edits the notification while leaving the redacted password placeholder unchanged
- **WHEN** the operator tests the current configuration
- **THEN** VaultFleet uses the existing SMTP password for the test without persisting the draft configuration
