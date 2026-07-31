# US-A.2 — Log in via OIDC

**Milestone:** M2
**Depends on:** US-A.1 (replaces it)

**As a** member, **I want** to log in through our group's OIDC provider
(Zitadel/Keycloak), **so that** I have one account across our self-hosted services.

## Acceptance criteria

- Authorization code flow with PKCE; backend is the confidential client.
- Browser receives only an HttpOnly session cookie; no tokens ever reach the
  frontend.
- A dev provider is provisioned via compose.
- Follows `docs/agents/research/2026-07-21-oidc-go-pwa-integration.md`.
