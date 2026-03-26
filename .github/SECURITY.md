# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in AS-Stats, please report it responsibly:

1. **DO NOT** open a public GitHub issue for security vulnerabilities
2. Email: security@nextmap.io
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

We will acknowledge receipt within 48 hours and aim to release a fix within 7 days for critical issues.

## Security Measures

This project implements:

- **OIDC authentication** with PKCE and state validation
- **CSRF protection** via double-submit cookie pattern
- **Rate limiting** per IP with proxy-aware extraction
- **Input validation** on all API endpoints
- **Parameterized queries** (no SQL injection)
- **Security headers** (X-Content-Type-Options, X-Frame-Options, Referrer-Policy)
- **Secrets scanning** via GitHub Advanced Security
- **Dependency updates** via Dependabot (weekly)
- **CI pipeline** with linting, testing, and Docker build verification
