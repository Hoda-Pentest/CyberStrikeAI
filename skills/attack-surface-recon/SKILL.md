---
name: attack-surface-recon
description: >-
  Recon/attack-surface mapping: passive whois/amass/crt.sh/FOFA/Shodan, active subfinder/httpx/naabu/katana/nuclei, DNS region/CDN/Nginx catch-all/BT Panel/UniApp fingerprints. The opening move; write findings to the project blackboard. Use when starting recon, asset mapping, fingerprinting, or CDN/DNS bypass discovery.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Reconnaissance / Attack-Surface Mapping

```
=== Recon/attack-surface mapping (the opening move, 60% of the effort is here, do not rush into exploitation unprepared; immediately upsert_project_fact for ports/fingerprints, etc.) ===
Passive first (do not touch the target, execute all):
  terminal: whois {domain} | grep -iE 'org|name|email|registrant'
  terminal: amass intel -asn {ASN} -d {domain}
  browser_navigate: https://crt.sh/?q=%.{domain} (certificate transparency to dig for subdomains)
  terminal: curl -s "https://crt.sh/?q=%.{domain}&output=json" | python3 -c "import sys,json;[print(x['name_value']) for x in json.load(sys.stdin)]" | sort -u
  terminal: dig +short {domain} @114.114.114.114; dig +short {domain} @8.8.8.8 (differences = CDN)
  terminal: curl -s "https://web.archive.org/cdx/search/cdx?url=*.{domain}/*&output=text&fl=original&collapse=urlkey" | head -100
  browser_navigate: https://fofa.info/result?qbase64=$(echo -n 'domain="{domain}"' | base64)
  browser_navigate: https://www.shodan.io/search?query=hostname:{domain}
Active pipeline (execute step by step):
  terminal: subfinder -d {domain} -silent | tee subs.txt; amass enum -d {domain} -passive >> subs.txt; sort -u subs.txt -o subs.txt
  terminal: cat subs.txt | dnsx -silent | tee alive_subs.txt
  terminal: cat alive_subs.txt | httpx -silent -title -status-code -tech-detect -cdn | tee httpx_out.txt
  terminal: naabu -l alive_subs.txt -top-ports 1000 -silent | tee ports.txt; nmap -sCV -iL <(head -20 alive_subs.txt) -oN nmap_out.txt
  terminal: katana -list alive_subs.txt -silent -d 3 | tee urls.txt; gau {domain} >> urls.txt
  terminal: cat urls.txt | grep -iE '\.(js|json)$' | httpx -silent -mc 200 | while read u; do curl -s "$u" | grep -oiE '(api|secret|key|token|password|aws|endpoint)[^"]*'; done
  terminal: ffuf -u https://{target}/FUZZ -w /usr/share/seclists/Discovery/Web-Content/raft-medium-directories.txt -mc 200,301,302,403 -fs {catch-all-size}
  terminal: nuclei -l alive_subs.txt -t /root/nuclei-templates/ -severity medium,high,critical -o nuclei_results.txt
  → httpx output identifies the framework/version → immediately trigger the `component-vuln-intel` search sequence
🚨 DNS region-restriction bypass: domestic CDNs (987dns/dnspod/Aliyun) often return 0.0.0.0 to overseas DNS; resolve with 114.114.114.114 to get the real IP. Multi-DNS comparison: dig @1.1.1.1 vs @114.114.114.114 vs @8.8.8.8, the difference is the CDN's regional policy. The real IP is hidden in the domestic DNS results.
🚨 CDN response-filtering bypass (GCCDN/PWS, etc.): the CDN returns 502 for specific URL patterns (admin path/actuator) instead of forwarding the backend response:
  Fingerprinting: Server: PWS/x.x.x.x | Via: 1.1 PS-XXX-XXXX:N (W) | X-Px: ms CS-XXX-XXXXnone (origin) | wildcard *.gccdn.net CNAME
  502 ≠ nonexistent: 502 = backend response filtered by CDN, 404 = path truly does not exist, 403 = CDN route refused. Use the 502/404/403 differences to enumerate live endpoints
  Path-traversal confirmation: /api/v1/v/..;/..;/actuator/env → 502 (exists, filtered) vs /api/v1/v/..;/..;/xxxx → 404 (Tomcat-native, does not exist)
  CDN node multi-port: scan ports 3000-9200 of the CDN IP (8000/8080/9090 often proxy the same backend but with possibly different rules, 9200 may be a standalone nginx)
  Bypass-attempt matrix (all tested this session failed, recorded to avoid duplication): URL encoding/double encoding/case variation/semicolon suffix/TE space smuggling/HTTP pipelining/Accept-Encoding/Range
  Useful information extraction: even on 502, POST actuator/env may still be executed by the backend (blind property write) → needs a refresh trigger to combine with (if refresh returns 404 the chain breaks)
🚨 Nginx catch-all trap: a PHP CMS configures rewrite for all routes → all paths return 200 of the same size = catch-all (not that the file exists!). Identification: compare 3 random paths; identical sizes = catch-all. Test real live paths with the .php suffix (nginx `location ~ \.php$` goes straight to FastCGI, returning a real 404 if it does not exist).
🚨 Pure-FTPd puredb corruption ≠ auth bypass: "Unable to read the indexed puredb file" = the virtual-user db is broken, and all usernames (including anonymous) trigger a 421 disconnect. PAM system-user authentication also goes through puredb first → all fail. Not an attack surface, skip it.
🚨 Same-IP multi-site lateral movement: reverse-lookup the IP (SecurityTrails/Weibu/VirusTotal) to find other domains on the same server → break into a weak site among them (a standalone nginx config returning a real 404 = enumerable) → BT Panel's unified management = lateral movement across all sites.
Fingerprint priority: once httpx/wappalyzer identifies the framework version → immediately switch to `component-vuln-intel` online intelligence. First map the attack surface clearly, then reason to choose the juiciest edge; do not dive headfirst into the first hole you see.
🚨 DNS-wildcard CDN identification: all subdomains (including nonexistent random ones) resolving to the CDN = wildcard DNS (*.domain → CDN CNAME). In this case subdomain enumeration is meaningless; all IPs are CDN nodes.
  Verification: dig random123456.target.com → if it also has an A record pointing to a known CDN IP = wildcard. The real origin IP must be obtained from other channels (historical DNS/certificate transparency/other domains on the same server/email headers/SSRF)
🚨 BT Panel identification: a cookie name that is a 32-char hex hash (e.g. 789d6d2de16419a1e8bbfea926c8996e) + 302 redirect to /login + nginx reverse proxy + an "入口校验失败" page = BT Panel's random security entry
  Windows version: IIS+nginx coexist, port 8888 panel, port 888 phpMyAdmin (possibly), /index.html may leak the site config (reverse-proxy target URL/site name)
  Attack: entry-path brute force (8 random alphanumerics) | historical CVEs (unauthorized API key/phar deserialization) | reuse of the panel's default FTP/MySQL accounts
🚨 Nginx 404 differential fingerprint: when nginx catch-all rewriting masks the real path, compare response sizes —— a real nginx 404 (146B) = the file exists on disk but the location block denies it, catch-all (large page) = the route did not match. Batch-probe CMS marker paths (/caches/configs/, /phpcms/modules/, /statics/, etc.) and distinguish by response size to confirm the CMS type. See references/nginx-404-differential-fingerprinting.md
🚨 UniApp/DCloud APK reversing fast path: assets/apps/__UNI__*/www/app-service.js = all the business logic (JS-obfuscated); assets/zlsioh.dat = encrypted config (API domain); lib/arm64-v8a/libirzlemnr.so = decryption library. Deobfuscate the JS with RC4+base64 (extract the a0G array + the a0m decoder); in the .so, strings-search for a 16-char random string = possible key, search for the `x digit.digit.digit.digit x` format = hardcoded IP (usually DCloud HTTPDNS infrastructure, not the target API). See references/uniapp-dcloud-apk-reversing.md
```
