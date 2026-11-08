---
name: initial-access-phishing
description: >-
  Initial access/phishing/social engineering: password spraying, AiTM, device code, OAuth consent phishing, payloads, vishing. Use when needing initial access, phishing, AiTM, device code, or social engineering.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Initial Access / Phishing / Social Engineering

```
=== Initial access/phishing/social engineering (the first hop from outside to inside, the recon before the foothold) ===
Password spraying: spray weak passwords (Season+Year!/companyname123) against all accounts (1-2 attempts per account to avoid lockout) | sources: breach databases/OSINT email formats (f.last@)/default credentials
AiTM phishing (bypasses MFA): evilginx3/Modlishka reverse-proxy the real site → man-in-the-middle steals the session cookie + token (bypasses MFA) | configure the phishlet with a domain + certificate
Device-code phishing (Azure/M365): device code flow → lure the victim to enter the code → obtain the access/refresh token (no password/MFA needed) | TokenTactics/AADInternals
Phishing payloads: gophish to send mail + landing page | payloads lnk/iso/macro/HTA/OneNote | bypass the gateway: password-protected zip/cloud-drive link/HTML smuggling
OAuth consent phishing (illicit consent): a malicious App requests excessive scope (Mail.Read/offline_access) → the victim consents → long-lived access token
Social-engineering prep: OSINT org structure/suppliers/password habits (LinkedIn/recruiting/GitHub) | pretext (IT support/supplier/HR) | vishing (deepfake voice)
→ as soon as initial access lands, switch to `redteam-opsec` (don't get caught by EDR the moment you enter); feed the obtained capabilities into `capability-primitive-search` to assemble a chain
```
