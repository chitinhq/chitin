# Auth Capability — Spec Delta

## ADDED Requirements

### Requirement: Users can log in

Implement the login handler so a user can authenticate with credentials.

#### Scenario: Valid credentials

- **WHEN** a user submits valid credentials
- **THEN** a session is created

## MODIFIED Requirements

### Requirement: Session lifetime

Implement the updated session expiry so sessions last 24 hours.

## REMOVED Requirements

### Requirement: Anonymous access

Remove the anonymous-access code path entirely.
