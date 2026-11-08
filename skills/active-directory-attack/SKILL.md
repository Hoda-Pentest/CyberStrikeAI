---
name: active-directory-attack
description: >-
  Internal domain attacks: BloodHound, Kerberoast, ADCS ESC1/ESC8, NTLM Relay, Coerce, DACL, DCSync, Zerologon/NoPac/PrintNightmare, mitm6, LLMNR, Linux internal network. Use when attacking Active Directory, ADCS, NTLM relay, or internal domain.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Internal Network / Domain Attacks

```
=== Internal domain (the real main battlefield in 2023+) ===
Recon: BloodHound (SharpHound collection → attack paths) | Kerberos: GetNPUsers (AS-REP)/GetUserSPNs (Kerberoast)
🚨 ADCS (Certipy all-in-one): certipy find -vulnerable | ESC1 to specify a SAN and request a domain-admin certificate | ESC8 relay to the CA to get a DC certificate
🚨 NTLM Relay (more important than PtH; PtH is often blocked by EDR): ntlmrelayx -t ldap --escalate-user / -t http CA --adcs (ESC8) / RBCD
🚨 Coercion (forced authentication): PetitPotam (MS-EFSRPC)/coercer all-protocol spray/printerbug → feed it to the relay
🚨 DACL abuse: WriteDACL → grant yourself DCSync | shadow credentials certipy shadow (GenericWrite suffices, no password change, no trace)
DCSync: secretsdump -just-dc → krbtgt hash→Golden Ticket
🚨 One-shot-kill domain CVEs (test first; a hit means instant domain admin): Zerologon (CVE-2020-1472, null the DC machine-account password → DCSync) | NoPac (CVE-2021-42278/42287, rename the machine account to request a DC TGT) | PrintNightmare (CVE-2021-34527, print-spooler RCE/load a malicious driver) | EternalBlue (MS17-010, old SMBv1 direct RCE)
🚨 IPv6/mitm6 (a must against default dual-stack internal networks): mitm6 hijacks DHCPv6+DNS → WPAD → ntlmrelayx to LDAP/ADCS (stealthier than LLMNR, the first choice on modern internal networks)
LLMNR/NBT-NS poisoning: responder captures NetNTLMv2 → hashcat cracks/relay
Linux internal network: Redis unauthorized access (CONFIG SET dir to write an SSH key) | NFS showmount | Docker 2375
```
