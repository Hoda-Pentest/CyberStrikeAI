---
name: zero-day-discovery
description: >-
  0day autonomous discovery engine: variant analysis/patch gaps/differential/Fuzzing/taint inference/N-day weaponization/hunter mindset. Use when public vulns not found and need to discover 0day or weaponize N-day.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## 0day Autonomous Discovery Engine (dig it yourself when no vulnerability can be found anywhere online)

```
Core shift: from "matching known vulnerability databases" → "understanding how the code/protocol works and inferring where it will break". 0day is not luck, it's method.
Five main routes:
  1. Variant analysis (most productive): take one CVE patch → distill the vulnerability pattern → grep the whole codebase for the same pattern elsewhere → what the patch did not cover = 0day
  2. Patch gaps: read the patch's filtering logic; a blacklist can almost always be bypassed (some encoding/equivalent function/alias was missed) → new CVE
  3. Differential testing: two components interpret the same input inconsistently (WAF vs backend / validator vs executor) → smuggling/SSRF bypass
  4. Fuzzing: write a harness (wrap the function that handles untrusted input) + build a corpus/dictionary + crash triage (reproducibility/controllability/exploitability)
     AFL++/libFuzzer (coverage-guided, finds memory corruption) boofuzz (protocol) radamsa (black-box) restler (REST API)
  5. Taint inference (strongest with source code): a source (parameter/Header/deserialization field) reaching a sink (exec/SQL/template) with no effective sanitizer = 0day
     CodeQL write a query to automatically solve data-flow reachability / Semgrep / Joern
N-day weaponization (advisory published but no PoC anywhere online): reverse-reconstruct the exploit from the patch diff (bindiff/diaphora binary comparison,
  the vendor's regression test is often a PoC prototype) → get it working in a local lab → hit the target. Maximizes the vulnerability window; one of a red team's most valuable capabilities.
Hunter mindset (interrogate any code/endpoint/protocol layer by layer; every "yes" is a 0day candidate):
  Trust boundaries: is the input assumed trusted? under what conditions does that not hold? | State/timing: can the state be changed between two steps (TOCTOU)? can the order be disrupted?
  Parsing normalization: how many times is it parsed? is normalize before or after validation? | boundary extremes: negative/0/oversized/type confusion/encoding/null byte?
  Implicit capability: what did this feature give me "incidentally"? | Uniqueness: is the ID/token predictable? is the "secret" really secret?
Turn a component into an "assumption list" and break each one: file upload implicitly assumes "only images are uploaded/the extension is trustworthy/the filename contains no path/the content is just data" → break each one = a vulnerability, combine them = a chain.
0day verification (stricter than for a CVE): reproducible (minimal PoC) + clear root cause (which line/which assumption) + provable impact (actual read/write/execute) + false-positive excluded.
```

