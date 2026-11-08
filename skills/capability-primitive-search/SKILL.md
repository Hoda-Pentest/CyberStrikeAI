---
name: capability-primitive-search
description: >-
  Capability primitives + state-space search: primitives like read/write/exec/ssrf to assemble RCE equations A-F, low-severity mapping, forward/backward search, cross-domain cashing-in. Use when no single RCE, chaining low-severity vulns, or deriving novel attack chains.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Capability Primitives + State-Space Search

```
Mindset: RCE is not a single "vulnerability", it is what emerges once a set of "capabilities" is assembled. Without a single big hole, a few info-level/low-severity issues can still be combined into code execution.
  Fixed thinking (to break): "can't scan up an RCE/deserialization/upload → this site can't be cracked". Your job is not to "find an RCE vulnerability", it's to "assemble the primitives needed for execution".
  Abstract each Fact into a capability primitive (don't record "found vulnerability X", record "what capability you now have + its constraints"):
    read(path) write(path) exec(cmd) ssrf(url) sqli redirect(url) eval_expr idor(id) cred(svc,priv) coerce_auth write_acl

RCE only requires satisfying any one of the equations; break it down into obtainable primitives to assemble:
  A. Can write a file + the file is executed as code                = RCE
  B. Can control config/env + the config points to your code            = RCE
  C. Can reach the admin panel + the panel has a built-in execution feature              = RCE (not a vulnerability, a feature!)
  D. Have credentials + the service has a legitimate execution entry                = RCE (abuse of a legitimate feature)
  E. Can read arbitrarily + read credentials + the credentials can log into an execution point      = RCE
  F. Can control data + the data flows into a dangerous sink (eval/template/SQL) = RCE

Low-severity → primitive mapping (translate a "useless-looking vulnerability" into a puzzle piece):
  info leak (.git/backups/stack traces) → source/paths/keys → feed B/E/F | LFI/arbitrary read → read config keys → E, or log poisoning → A
  SSRF (even GET-only) → hit internal Redis/Consul/K8s/cloud metadata → C; cloud metadata to get temporary credentials → D
  Weak/default/reused credentials → reach a backend with task/plugin/webhook/CI features → C | CORS/CSRF/XSS → borrow the admin's browser to invoke execution-type features → C
  Controllable upload (even with extension restrictions) → combine with path traversal/parsing discrepancy/.htaccess → A | config write → modify template/log/connection string → B
  SQLi (even read-only) → read a hash/key → E, or OUTFILE → A | controllable template → SSTI → F | prototype pollution → pollute a downstream property → F

State-space search (search it yourself when there's no ready-made chain): state = the current capability set, action = use a capability to unlock a new capability, goal = Goal.
  Forward: for each capability ask "what can it unlock?"; for each pair of capabilities ask "what does combining them produce?"
    (read+write=modify config; ssrf+internal redis=RCE; sqli+FILE=webshell; coerce_auth+relay=domain SYSTEM; idor+massassign=change another user's admin)
  Backward (mainly used when stuck): fix the Goal = execute a command → pick the equation closest to the current state as the template → set whichever primitive is missing as a subgoal →
    Which low-severity/feature/info-leak on hand can assemble it? → if it can't, recursively decompose/switch equations → forward and backward meeting in the middle = the full chain emerges → verify segment by segment

Breakthrough point (when you see it, don't walk away):
  · "features are primitives": the admin backend's scheduled tasks/plugins/template editing/SQL console/file manager/import-export/webhook —— legitimate features, but once logged in they are ready-made execute/read-write primitives. In a pentester's eyes there is no distinction between "feature/vulnerability", only "capability".
  · cross-protocol jumps: SSRF's gopher/dict/file turn "can only send HTTP" into "hit Redis/send SMTP/read files".
  · credential reuse is the universal glue: by default, any password/key obtained anywhere is reused and sprayed everywhere. Lateral is often faster than vertical.
  · parsing discrepancies: the upload validator/router/reverse proxy interpret things inconsistently → there is a bypass in the gap (double extension/encoding/Host confusion).
  · time dimension: TOCTOU/predictable token/cache poisoning, turning "occasional" into "stable" and the non-exploitable into exploitable.
  · cross-domain cashing-in: for each capability obtained, ask "how much is it worth in another domain" —— capability is a universal currency. Web SSRF → reach cloud metadata and take over the account; APK hardcoding → connect directly to the internal API bypassing frontend auth; supply chain → get CI secrets and reach production.
  · creative mode (when known combinations are exhausted): re-examine the capability boundary (can only read /var/log? what about /proc/self/environ) | find sinks equivalent to RCE (writing crontab/.bashrc/CI config/LD_PRELOAD/authorized_keys/systemd unit all = RCE) | use information (error messages/timing/response length) as a side channel | invert assumptions: list "what I thought was impossible" and ask of each "why exactly is it impossible"
> A derived chain is a hypothesis (can be recorded tentatively as a note/chain); only write a confirmed Fact / record_vulnerability after actual execution + evidence. The whole chain holds only when every step has been verified.
```

