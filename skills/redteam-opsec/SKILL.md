---
name: redteam-opsec
description: >-
  OPSEC covert-operation discipline: IP-blacklist bypass, rate/timing, traffic obfuscation, minimal footprint, anti-forensics, progressive exposure. Use when maintaining stealth, bypassing IP bans, or planning covert red-team ops.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## OPSEC / Covert-Operation Discipline (AV evasion / stability / stealth)

```
Core: getting in ≠ staying in stably. Being discovered reduces the operation to zero. Before each action, ask "what does this step look like in the defender's eyes".
🚨 IP-blacklist bypass (try it the moment you are banned): X-Forwarded-For: random IP (when the CDN trusts this header it directly bypasses the application-layer blacklist; when it returns code:10010/黑名单, try XFF/X-Real-IP/CF-Connecting-IP/True-Client-IP one by one)
  Verification: a normal request returns "黑名单" + your IP → after adding the XFF header it returns a normal 200 → all subsequent requests carry XFF
  Advanced: switch the XFF value every N requests (to avoid the new IP also being banned) | some CDNs trust only the first XFF value, others the last
Rate and timing: throttle scans (nuclei -rl / nmap -T2 --max-rate / ffuf -p delay) to avoid WAF blocking + IDS threshold alerts | high-risk actions at low frequency + random jitter | avoid business peaks and also late night (matching the target's work hours is least conspicuous)
Traffic obfuscation: blend with normal business (common UA/Referer/legitimate paths) | probe defenses before heavy weapons (process list/known EDR/SIEM agent signatures) → if present, prefer quiet techniques; only use automated bulk operations when there is no monitoring
Minimal footprint: prefer in-memory execution without touching disk (DDexec/memfd/reflective loading) | webshell with a strong password + uncommon path + feature disguise | tunnel over 443/DNS blending with common egress | delete tools right after use (/dev/shm memory disk, leave no remains)
Anti-forensics: command history unset HISTFILE / set +o history | selectively delete your own log entries (truncating everything instead triggers an alert) | timestamps touch -r referencing a neighboring file to preserve mtime | don't touch monitoring/audit services (stopping them is itself an alert)
Progressive exposure: passive recon (certificate transparency/passive DNS/search engines/asset engines) → confirm there is no strong monitoring → only then active scanning → only at the end actual exploitation. Whatever can be obtained from public intelligence, never touch the target actively. At each escalation ask "is it worth the exposure".
> Stealth is not fastidiousness, it is a red team's survival skill. One reckless full-port full-speed scan can reduce the entire operation to zero.
```

