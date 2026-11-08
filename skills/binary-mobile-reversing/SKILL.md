---
name: binary-mobile-reversing
description: >-
  APK/EXE/binary: UniApp/DCloud/Flutter reversing, certificate-pinning bypass, exported components, memory-corruption exploit chains, IoT firmware. Use when reversing APK/EXE, UniApp/Flutter, native .so, or memory-corruption exploits.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## APK / EXE / Binary Reversing

```
=== APK/EXE/binary ===
APK: apktool d / jadx | check the Manifest for exported components/deeplink/debuggable | rg for hardcoded keys + API addresses | .so strings/Ghidra | frida/objection dynamic
🚨 UniApp/DCloud APK reversing (H5 hybrid app, the business is entirely in JS, not in the DEX):
  Identification: __UNI__XXXXXXX + assets/apps/ + uni-jsframework.js (NOT Flutter, no libapp.so) | manifest.json contains the "uni-app" field
  Core file: assets/apps/__UNI__XXX/www/app-service.js (800KB+ obfuscated JS = all the business logic) | jadx only shows the shell (native plugin registration), the real logic is in the JS
  JS deobfuscation (RC4+string-array rotation): extract the a0G() large array (10000+ elements) + a0m(idx,key) decode function (base64→RC4) + rotation IIFE → assemble executable JS → node batch-decode → JSON dictionary (offset→plaintext)
    🔴 Trap: the rotation IIFE may end with a comma (not a semicolon! it is part of a larger expression) → node reports "Unexpected token" → extract according to the actual terminator
  Encrypted config zlsioh.dat/dcloud3.dat: header (96B offset table: off40=block1 decompression/44=compression/60=block2 offset/64=block2 size/80=block3 offset) + block1 (zlib→DEX) + block2 (encrypted, contains the API domain list ← key, requires .so to decrypt) + block3 (zlib→AndroidX class mapping)
    zlib magic 78DA = unencrypted, decompress directly | no magic = encrypted block (key in .so .rodata/assembly immediate)
  .so string deobfuscation: extract even-position characters → reverse (e.g. "mAojcl.dubdFiHaebP.nwywfwb"→"www.baidu.com") | strings -n8 lib*.so to find long strings containing dots
  ⚠️ Trap: x-separated IP strings in the .so (e.g. "x111.230.69.120x118.126.105.164") = DCloud HTTPDNS nodes (not the API backend! 140.205.11.x = Aliyun DNS, 111.230/118.126/106.52/42.193 = Tencent Cloud) | a resources.arsc error is intentional anti-decompilation protection (does not affect jadx) | fake PNGs in assets (1-8px) = integrity check, not data | fcapp.run is only an APK-download proxy
  Key API variables: $apiHost/$opHost (set dynamically at runtime, not hardcoded)/urlList/checkAvailableDomainList (HEAD domain/favicon.ico to test connectivity, 200-399 = usable)
  High-value endpoints: /app_init /user/gustRegister (guest registration without verification) /user/getOssSts (OSS temporary credentials!) /uploadFile2
  Config: manifest.json (appId/version/nativePlugins → wrs-httpserver = embedded HTTP service!) | supplierconfig.json (vivo/xiaomi/huawei/oppo appid) | dcloud_uniplugins.json (plugin manifest)
  ChengZi SDK (ChengZi site-builder): the init3 endpoint returns single-byte XOR (key=0x96, see chengzi_decrypt.py) encrypted JSON → decrypt to get the APK's real download link (fu field) + channel code
  Domain fallback: when $apiHost can't be obtained statically → Android emulator (Waydroid/AVD) + tcpdump/mitmproxy to capture runtime DNS
  See also: references/uniapp-apk-reverse-engineering.md, references/uniapp-apk-reversing.md, scripts/js_rc4_deobfuscate.js, scripts/chengzi_decrypt.py
🚨 Flutter APK fast reversing (no unpacking needed): hardening (MogoSec/Bangcle/360) protects the DEX layer, but Flutter's lib/arm64-v8a/libapp.so (Dart AOT) is usually not encrypted
  strings -n8 libapp.so|grep 'https\?://' → all hardcoded URLs/API domains/S3 addresses/CDN
  strings -n8 libapp.so|grep -E '^/' → API-path matrix (/Member/Login, /Web/VideoList, /BBS/GetSTSToken, etc.)
  strings -n8 libapp.so|grep -iE 'key|secret|token|aws|bucket' → credential/key leak
  assets/config_*.xml + assets/data_*.dat → encrypted config (may contain domains/API addresses) | .DS_Store → macOS developer-information leak | assets/xinstall* → channel-tracking SDK
🚨 Native .so string deobfuscation (general pattern): the obfuscated strings are in the .rodata section
  Common patterns: extract even-position characters + reverse (e.g. "mAojcl.dubdFiHaebP..." → take even positions → reverse = plaintext domain) | XOR constant | RC4+base64
  Identification: strings -n8 lib*.so to find 16-byte equal-length strings (AES key/IV)/strings containing dots (domains)/x-separated IP strings (HTTPDNS) | nm --dynamic to find obfuscated exported symbols (16-character random case)
  Verification: compare a known string ("classes.dex"/"io.dcloud.application") against the obfuscated version → reverse the algorithm → batch-decode
🚨 Mobile general (ordinary apps that are not UniApp/Flutter):
  Certificate-pinning bypass (to capture HTTPS): objection android sslpinning disable | frida universal-unpinning | modify smali to remove pinning and repackage
  Exported-component privilege bypass: Activities/Services/Providers/Receivers with exported=true in the Manifest → drozer/adb am start to invoke them cross-app | ContentProvider SQLi/path traversal
  Deeplink hijacking: scheme:// not validated → WebView loads an arbitrary URL (XSS/file read) | parameters flowing into the intent → component hijacking
  Insecure storage: /data/data/pkg/ (shared_prefs plaintext token/unencrypted db/logcat leak) | sdcard world-readable
  WebView: addJavascriptInterface (<4.2 RCE)/file:// to read local files/setAllowFileAccess | static: MobSF all-in-one | iOS: decrypt the app (frida-ios-dump) + Ghidra to inspect Mach-O + objection dynamic
EXE/PE: file/strings | Ghidra/IDA decompile to find hardcoded values/encryption/network logic | x64dbg dynamic debugging | vulnerabilities: overflow/format string/UAF/DLL hijacking
🚨 Memory-corruption exploit chain (crash → RCE): checksec to see protections → determine the primitive (stack overflow/UAF/format string = arbitrary read-write)
  → info leak to bypass ASLR → ROP chain (ROPgadget/pwntools ret2libc) → heap exploitation (tcache poison/__free_hook hijack)
  → arbitrary write to modify GOT/hook/vtable/exit_funcs → control-flow hijack | firmware IoT: binwalk -Me to extract + qemu-user debugging (weak protections often allow a direct stack overflow)
```
