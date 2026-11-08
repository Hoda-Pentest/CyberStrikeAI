---
name: proxy-tool-bootstrap
description: >-
  Self-sourced proxies + tool bootstrapping: SOCKS5/HTTP/Tor route-switching sequence, Python tool bootstrapping, self-generated dictionaries, OOB infrastructure. Use when blocked by 403/429/WAF/timeout, missing tools, or needing OOB confirmation.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Self-Sourced Proxies + Tool Bootstrapping (when blocked, switch routes; when a tool is missing, write it yourself)

```
🔴 Proxy (on receiving a rejection/rate-limit/timeout → the first reaction is not to retry, but to switch routes. Not switching routes = giving up = violates blackboard trigger 2 (see `pentest-blackboard`)):
  Execution sequence (in order; move to the next step only when the previous one fails):
  ① probe the target region: terminal: curl -s "http://ip-api.com/json/{target IP}" | python3 -c "import sys,json;d=json.load(sys.stdin);print(d['country'],d['countryCode'])"
  ② obtain a SOCKS5 proxy in that region: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country={countryCode}&timeout=5000" | head -20
  ③ verify the proxy can reach the target: terminal: curl --socks5 {PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ④ 200 = usable, immediately re-run the blocked request through this proxy; non-200 = switch to the next proxy and repeat ③
  ⑤ all SOCKS5 failed → obtain an HTTP proxy: terminal: curl -s "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&country={countryCode}&timeout=5000" | head -20
  ⑥ HTTP proxy verification: terminal: curl --proxy http://{PROXY_IP:PORT} --connect-timeout 8 -s -o /dev/null -w "%{http_code}" {target URL}
  ⑦ all proxies failed → Tor: terminal: curl --socks5 127.0.0.1:9050 --connect-timeout 15 {target URL}
  Add a proxy parameter uniformly across tools: curl --socks5 / sqlmap --proxy=socks5://{P} / nmap --proxies socks5://{P} / nuclei -proxy socks5://{P} / ffuf -x socks5://{P}
  Rotation strategy: 429/403 → immediately switch to the next proxy, proactively switch every 20 requests (to avoid the new IP also being banned) | Cloudflare → proxy pool + random 2-5s interval between requests
  🚨 HTTP proxy vs SOCKS5: an HTTP proxy inserts its own error page (502/cannot display this page), making it impossible to distinguish the target's actual response → when probing you must use SOCKS5 (--socks5)
    ProxyScrape SOCKS5: https://api.proxyscrape.com/v2/?request=displayproxies&protocol=socks5&country=CN,JP&timeout=5000
    Verify SOCKS5: curl --socks5 IP:PORT --connect-timeout 5 {target} | an HTTP proxy is only suitable for anonymity/rotation against a target already confirmed reachable
Tool bootstrapping (which X || implement it in Python):
  No nmap → socket to scan ports | no ffuf → requests to brute directories | no sqlmap → manual payload detection | no hydra → requests to brute force | no nuclei → requests to fire known payloads
  Simulate complex tools with Python: crawler requests+bs4 / encoding base64/hex / hashing hashlib / crypto pycryptodome / sniffing scapy
Self-generated dictionaries: build variants from the target domain/company name | extract keywords from the web page | username + year + special-character combinations | service default credentials
OOB infrastructure (all blind vulnerabilities rely on it): interactsh-client to get an oast.fun domain | or a VPS python3 -m http.server/nc to watch for callbacks | ngrok/cloudflared tunnel
  → only count it as confirmed when you see an OOB callback (DNS query/HTTP request) → write a Fact
```

