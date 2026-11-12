# راهنمای فایل پیکربندی CyberStrikeAI (`config.yaml`)

> [English](../en-US/) · [中文](../zh-CN/)

این سند همهٔ کلیدهای `config.yaml` (نمونه: `config.example.yaml`) و مقدارهای آن‌ها را توضیح می‌دهد.

---

## اصول کلی (قبل از هر چیز بخوان)

- **محل فایل:** مسیر فایل با فلگ `--config config.yaml` به سرور داده می‌شود (پیش‌فرض: `config.yaml` کنار محل اجرا).
- **مسیرهای نسبی:** بیشتر مسیرها (`tools_dir`, `skills_dir`, `agents_dir`, `roles_dir`, `database.path`, `knowledge.base_path`, `log.diagnostic_dir`, `workspace_root_dir`) **نسبت به پوشهٔ همین فایل config** حل می‌شوند. پس `WorkingDirectory` در systemd باید همان پوشه باشد.
- **ویرایش گرافیکی:** تقریباً همهٔ این مقادیر از دکمهٔ **Settings** در گوشهٔ بالا-راستِ وب‌اپ هم قابل تغییرند.
- **اولویت متغیر محیطی:** برای کلیدهای API (مثل FOFA/Quake/Shodan/ZoomEye) متغیر محیطی بر مقدار فایل **اولویت دارد**.
- **ری‌استارت:** بعضی تغییرها (مثل `agent.*`, `tool_guard`, `log.diagnostic_disabled`) فقط بعد از **ری‌استارت سرویس** اعمال می‌شوند؛ بقیه بلافاصله.
- **⚠️ امنیت:** این یک سامانهٔ پرمجوز است (اجرای دستور، C2، WebShell). مقادیر امنیتی (`tool_guard`, `hitl`, `mcp.allow_global_access`) را سرسری تغییر نده.

---

## `version`
شمارهٔ نسخه‌ای که در رابط کاربری نشان داده می‌شود. خالی بگذاری، نسخهٔ پیش‌فرض نشان داده می‌شود. بعد از به‌روزرسانیِ یک‌کلیکی، از کدِ جدید همگام می‌شود.

```yaml
version: "v1.7.24"
```

---

## `server` — سرور وب و TLS

| کلید | مقدار | توضیح |
|---|---|---|
| `host` | `0.0.0.0` | آدرس گوش‌دادن. `0.0.0.0` یعنی روی همهٔ کارت‌های شبکه. برای فقط-لوکال: `127.0.0.1`. |
| `port` | `8080` | پورت سرویس. بدون TLS: `http://localhost:8080`. |
| `cors_allowed_origins` | لیست | Originهای مورد اعتمادِ اینتگریشن‌های دیگر؛ **از wildcard استفاده نکن**. افزونهٔ مرورگر خودکار شناخته می‌شود. |
| `tls_enabled` | `true/false` | فعال‌سازی HTTPS. با فعال‌شدن، دسترسی از `https://...:<port>/`. |
| `tls_http_redirect` | `true` | وقتی TLS روشن است، HTTP ساده خودکار با 308 به HTTPS هدایت می‌شود. برای خاموش‌کردن: `false`. |
| `tls_cert_path` / `tls_key_path` | مسیر PEM | **گزینهٔ A (تولید):** گواهی و کلید خصوصی واقعی. |
| `tls_auto_self_sign` | `true` | **گزینهٔ B (فقط لوکال/تست):** گواهی self-signed در حافظه؛ مرورگر هشدار «نامعتبر» می‌دهد. |

> **برای تولید:** گواهی واقعی بده (`tls_cert_path/tls_key_path`) و `tls_auto_self_sign` را خاموش کن، یا پشت nginx/پراکسیِ TLS بگذار.

---

## `auth` — احراز هویت
```yaml
auth:
  session_duration_hours: 12   # اعتبار لاگین (ساعت)؛ بعد از انقضا باید دوباره وارد شوی
```

---

## `log` — لاگ‌گیری

| کلید | مقدار پیش‌فرض | توضیح |
|---|---|---|
| `level` | `info` | سطح لاگ: `debug`, `info`, `warn`, `error`. |
| `output` | `stdout` | مقصد لاگ: `stdout`, `stderr`, یا مسیر فایل. |
| `diagnostic_dir` | `log` | لاگ‌های تشخیصیِ `warn` و بالاتر را جدا، به تفکیک تاریخ، ذخیره می‌کند (نسبت به پوشهٔ کاری). |
| `diagnostic_retention_days` | `14` | روزهای نگه‌داری (شاملِ امروز). `<=0` یعنی ۱۴ روز. |
| `diagnostic_disabled` | `false` | `true` لاگ‌های تشخیصیِ اضافه را خاموش می‌کند (نیاز به ری‌استارت). |

---

## `audit` — ممیزیِ عملیاتِ پلتفرم
System Settings → Log Audit. محتوای مکالمه یا هر فراخوانیِ ابزار را ثبت **نمی‌کند**.
```yaml
audit:
  enabled: true
  retention_days: 15                  # 0 = بدون پاک‌سازیِ خودکار
  max_detail_bytes: 8192
  auth_failure_cooldown_seconds: 60   # حداقل فاصلهٔ ثبتِ شکستِ لاگین/تغییر-رمز از یک IP؛ -1 = بدون محدودیت
```

---

## `monitor` — نگه‌داریِ رکوردهای اجرای ابزار (MCP)
```yaml
monitor:
  retention_days: 90   # پیش‌فرض 90؛ 0 = بدون پاک‌سازیِ خودکار
```

---

## `storage` — پاک‌سازیِ فضای کاری
سیاستِ نگه‌داریِ فایل‌های روی دیسک (فضای کاریِ Agent، کَشِ خروجیِ ابزار، آرتیفکت‌های C2، آپلودهای مکالمه).

| کلید | مقدار | توضیح |
|---|---|---|
| `auto_clean` | `false` | `true` پاک‌سازیِ خودکارِ پس‌زمینه را فعال می‌کند. پیش‌فرض خاموش تا بعد از آپگرید دادهٔ موجود بی‌خبر پاک نشود. |
| `interval_minutes` | `60` | فاصلهٔ پاک‌سازیِ پس‌زمینه (حداقل 5). |
| `orphan_grace_days` | `1` | اگر مکالمه/پروژه حذف شد ولی پوشه‌اش ماند، بعد از چند روزِ بی‌کاری بازپس‌گیری شود. |
| `active_grace_hours` | `24` | مکالمه‌های اخیراً فعال همیشه رد می‌شوند (تا دادهٔ تسکِ در-حالِ-اجرا پاک نشود). |
| `categories.*` | `enabled` + `retention_days` | سیاستِ هر دسته جدا: `workspace` (30)، `reduction` (7)، `conversation_artifacts` (30)، `plantask` (30)، `c2_artifacts` (30، با پاک‌شدنِ payloadها لینک‌های دانلود می‌میرند)، `chat_uploads` (90)، `workflow_checkpoints` (7)، `diagnostic_logs` (14). `retention_days: 0` = فقط پوشه‌های یتیم. |

---

## `ai` — کانال‌های مدلِ هوش مصنوعی
چند کانالِ سازگار با OpenAI / Claude را نگه می‌دارد. `default_channel` می‌گوید مکالمه‌ها/تسک‌هایی که کانال مشخص نکرده‌اند از کدام استفاده کنند.

```yaml
ai:
  default_channel: qwen-max
  channels:
    qwen-max:
      name: Qwen Max
      provider: openai_compatible      # openai_compatible (پیش‌فرض) | claude (Anthropic Messages API بومیِ Eino)
      base_url: https://dashscope.aliyuncs.com/compatible-mode/v1
      api_key: sk-xxxxxxx
      model: qwen3-max
      max_total_tokens: 120000         # سقفِ کلِ توکنِ کانتکست
      max_completion_tokens: 32768     # سقفِ توکنِ خروجی
      reasoning:
        mode: on                       # auto | on | off
        effort: high                   # low | medium | high | max | xhigh
        allow_client_reasoning: true   # false = reasoningِ درخواستِ کلاینت نادیده گرفته می‌شود
        profile: openai_compat         # auto | deepseek_compat | openai_compat | output_config_effort
```

- **Providerهای رایج:** OpenAI (`https://api.openai.com/v1`)، DeepSeek (`https://api.deepseek.com/v1`)، و هر API سازگار با پروتکل OpenAI.
- **`headers` (اختیاری):** هدرهای HTTP سفارشی برای گیت‌وی‌های واسط؛ `${ENV_VAR}` و `%session_id%` پشتیبانی می‌شوند. با هدرِ سفارشی فقط ریدایرکتِ هم‌منشأ مجاز است. در رابط کاربری به‌عنوان مقدارِ حساس ماسک می‌شوند.

---

## `vision` — تحلیلِ تصویر (ابزار `analyze_image`)
```yaml
vision:
  enabled: false            # true + model غیرخالی => ابزار analyze_image ثبت می‌شود
  model: qwen-vl
  api_key: ""               # خالی = از کانال AI پیش‌فرض
  base_url: ""              # خالی = از کانال AI پیش‌فرض
  max_image_bytes: 5242880  # سقفِ اندازهٔ فایلِ اصلی (۵MB)
  max_dimension: 2048       # مقیاسِ لبهٔ بلند به این تعداد پیکسل
  jpeg_quality: 82
  max_payload_bytes: 524288 # سقفِ ارسال به API بعد از انکود (۵۱۲KB)
  detail: auto              # low | high | auto
  timeout_seconds: 60
```

---

## `fofa` / `zoomeye` / `quake` / `shodan` — موتورهای نقشه‌برداریِ فضای سایبری
صفحهٔ «Asset Management → Information Gathering». بک‌اند درخواست‌ها را پراکسی می‌کند تا کلید در فرانت لو نرود. **متغیر محیطی اولویت دارد.**
```yaml
fofa:    { base_url: https://fofa.info/api/v1/search/all, api_key: "" }
zoomeye: { base_url: https://api.zoomeye.ai/v2/search,   api_key: "" }   # یا ENV: ZOOMEYE_API_KEY
quake:   { base_url: https://quake.360.net/api/v3/search/quake_service, api_key: "" }  # یا QUAKE_API_KEY
shodan:  { base_url: https://api.shodan.io, api_key: "" }                # یا SHODAN_API_KEY
```

---

## `agent` — تنظیمِ Agent (تایم‌اوت‌ها و تکرارها)
> تغییر این‌ها نیاز به **ری‌استارت** دارد و روی تسک‌های جدید اعمال می‌شود.

| کلید | پیش‌فرض | توضیح |
|---|---|---|
| `task_timeout_minutes` | `600` | تایم‌اوتِ کلِ یک تسک (دقیقه). `0` = بی‌نهایت. |
| `max_iterations` | `12000` | سقفِ کلیِ تکرار (مشترکِ single/Deep/Supervisor/Plan-Execute)؛ `agents/*.md` می‌تواند جداگانه override کند. |
| `tool_timeout_minutes` | `60` | حداکثر مدتِ اجرای یک ابزار (دقیقه)؛ `0` = بی‌نهایت (توصیه نمی‌شود). |
| `tool_wait_timeout_seconds` | `300` | حداکثر انتظار برای ابزارِ این دور؛ با تایم‌اوت `execution_id` برمی‌گردد و در پس‌زمینه ادامه می‌یابد. `0` = تا اتمام صبر کن. |
| `external_mcp_max_concurrent_per_server` | `5` | ابزارهای هم‌زمانِ هر سرور MCP خارجی. `0`=۲، منفی=نامحدود. |
| `external_mcp_max_concurrent_total` | `16` | سقفِ هم‌زمانیِ کلِ ابزارهای MCP خارجی. |
| `external_mcp_circuit_failure_threshold` | `15` | شکستِ پیاپیِ یک سرور قبل از قطعِ مدار. منفی=غیرفعال. |
| `external_mcp_circuit_cooldown_seconds` | `60` | زمانِ خنک‌شدنِ مدارِ قطع‌شده. |
| `shell_no_output_timeout_seconds` | `1200` | پایان‌دادن به `execute/exec` بعد از این ثانیه بدون خروجیِ جدید. `-1`=غیرفعال. |
| `workspace_root_dir` | `""` | ریشهٔ پوشهٔ کاریِ مکالمه. خالی=`tmp/workspace`. **از `/tmp` سیستم استفاده نکن.** |
| `system_prompt_path` | `""` | اختیاری: فایلِ system prompt سفارشی برای single-agent. |

---

## `tool_guard` — مسدودسازیِ فراخوانیِ ابزار
بلافاصله با ذخیره در «Security Protection → Call Interception» اعمال می‌شود (مستقل از HITL). محافظتِ دامنه‌های دولتی به‌صورت پیش‌فرض روشن است.
```yaml
tool_guard:
  enabled: true
  rules:
    - id: government-domains
      name: Government website protection
      enabled: true
      pattern: '(?i)...gov...'     # رجکسِ Go/RE2؛ گروهِ named به‌عنوان {match} در پیام استفاده می‌شود
      message: 'Detected {match}; attacking government websites is forbidden...'  # {match},{tool},{rule}
```

---

## `hitl` — تأییدِ انسان-در-حلقه (Human-in-the-Loop)
برای ابزارهایی که در whitelist نیستند، تأییدِ انسانی یا Agentِ ممیز اعمال می‌شود.

| کلید | مقدار | توضیح |
|---|---|---|
| `default_mode` | `off` | حالتِ پیش‌فرضِ کلی: `off` \| `approval` (تأیید) \| `review_edit` (بازبینی و ویرایش). مکالمه‌های جدید این را ارث می‌برند. |
| `default_reviewer` | `human` | تأییدکنندهٔ پیش‌فرض: `human` (دستی) یا `audit_agent` (Agentِ ممیز). |
| `default_timeout_seconds` | `300` | سقفِ انتظارِ تأیید (ثانیه). `0`=بی‌نهایت. |
| `audit_backend` | `openai` | بک‌اندِ Agentِ ممیز: `openai` (مدلِ چتِ سازگار) \| `typesafe` (ساختاریِ TypeSafe Jev). |
| `audit_model.*` | — | مدلِ اختصاصیِ ممیز. برای `openai`، فیلدهای خالی از مدلِ اصلی ارث می‌برند؛ برای `typesafe`، `api_key` اجباری است. |
| `retention_days` | `90` | نگه‌داریِ لاگ‌های تصمیمِ ممیز. `0`=بدون پاک‌سازی. |
| `tool_whitelist` | لیست | ابزارهایی که **همیشه از تأیید معاف‌اند** (مثل `read_file, ls, grep, list_vulnerabilities, ...`). `[]` = هیچ معافیتِ کلی. |
| `audit_agent_prompt` | متن | پرامپتِ حالتِ approval (خالی = پیش‌فرضِ داخلی؛ از صفحهٔ HITL ویرایش‌پذیر). |
| `audit_agent_prompt_review_edit` | متن | پرامپتِ حالتِ review_edit (می‌تواند پارامترها را قبل از اجازه اصلاح کند). |

> پرامپتِ ممیزِ پیش‌فرض طوری نوشته شده که اکثرِ عملیاتِ پنتست (شاملِ payloadهای تهاجمی) را **تأیید** کند و فقط رفتارِ آشکارا مخرب (حذفِ گستردهٔ داده، توقفِ سرویس، تغییرِ رمز، DoS) را **رد** کند.

---

## `multi_agent` — چندعاملی و Eino
```yaml
multi_agent:
  enabled: true
  robot_default_agent_mode: eino_single   # eino_single | deep | plan_execute | supervisor
  batch_use_multi_agent: false            # true = هر زیرتسکِ صفِ «Batch Tasks» هم چندعاملی (هزینهٔ بیشتر)
```
زیربخش‌های مهم:
- **`eino_skills`**: فعال/غیرفعال‌کردنِ میان‌افزارِ افشای تدریجیِ Skill و ابزارهای محلیِ FS/Shell.
  - `filesystem_tools: true` ابزارهای `read_file/glob/grep/write/edit/execute` را ثبت می‌کند (در محیطِ مجاز با احتیاط).
- **`eino_middleware`**: تنظیمِ دقیقِ reduction (کوتاه‌سازیِ خروجیِ بزرگ)، `tool_search` (نمایشِ تدریجیِ ابزارها برای صرفه‌جوییِ توکن)، `summarization` (فشرده‌سازیِ کانتکست)، `plantask` (تختهٔ تسکِ ساختاری)، failover و retryِ مدل، و بودجه‌های کاراکتریِ ورودیِ کاربر.
- **`eino_callbacks` + `otel`**: ردیابیِ سطحِ فریم‌ورک با OpenTelemetry. `exporter: stdout | otlphttp | none`.

> این بخش بزرگ و پیشرفته است؛ مقادیر پیش‌فرض برای اکثرِ استقرارها مناسب‌اند. برای جزئیاتِ هر فیلد به کامنتِ `config.example.yaml` و `internal/config/config.go` نگاه کن.

---

## `database` — پایگاه‌داده (SQLite)
```yaml
database:
  path: data/conversations.db        # تاریخچهٔ مکالمه و پیام‌ها
  knowledge_db_path: data/knowledge.db  # دانش‌نامه و وکتورها (خالی = از همان DB مکالمه)
```

---

## `security` — ایزولاسیونِ فرآیند و ابزارها
```yaml
security:
  process_isolation:
    mode: auto               # auto: روی Windows Job Object؛ روی لینوکس watchdog مگر cgroup_root ست شود
    cgroup_root: ""          # لینوکسِ تولید: سرویسِ systemd با Delegate=cpu memory pids + mode: required
    max_processes: 256
    memory_max_bytes: 2147483648   # ۲GB
    cpu_quota_micros: 0      # 0=بدون سقفِ CPU؛ 100000=یک هسته
  tools_dir: tools           # پوشهٔ فایل‌های پیکربندیِ ابزار (نسبت به config)
  tool_description_mode: full  # short (short_description، کم‌توکن) | full (description کامل)
```

---

## `mcp` — سرورِ MCP (پروتکلِ ابزار)
```yaml
mcp:
  enabled: false             # فعال‌سازیِ سرورِ MCP (حالتِ http)
  host: 127.0.0.1            # فقط وقتی دسترسیِ راه‌دور لازم است صریح تغییر بده + کنترلِ دسترسیِ شبکه بگذار
  port: 8081
  auth_header: "X-MCP-Token"
  auth_header_value: ""
  allow_global_access: false # ⚠️ پرخطر: کلیدِ ثابت را به هویتِ سرویسِ کلی نگاشت می‌کند؛ به‌جایش Bearer Tokenِ کاربر
```

## `external_mcp` — سرورهای MCP خارجی
```yaml
external_mcp:
  servers: {}   # نقشهٔ سرورهای MCP خارجی (از رابط کاربری هم افزوده می‌شود)
```

## `c2` — کنترل-و-فرمانِ داخلی
```yaml
c2:
  enabled: true   # false = روی ماشینی که فقط برای مکالمه/دانش‌نامه است؛ هیچ listener و ابزارِ C2 ثبت نمی‌شود
```

---

## `knowledge` — دانش‌نامه و بازیابیِ برداری
```yaml
knowledge:
  enabled: false
  base_path: knowledge_base
  embedding:
    provider: openai
    model: text-embedding-v4
    base_url: https://dashscope.aliyuncs.com/compatible-mode/v1  # خالی = از OpenAI config
    api_key: sk-xxxxxxx                                          # خالی = از OpenAI config
  retrieval:
    top_k: 5                       # تعدادِ نتیجهٔ Top-K
    similarity_threshold: 0.4      # آستانهٔ شباهتِ کسینوسی (0-1)
    multi_query: { max_queries: 4 }   # بازنویسیِ کوئری + ترکیبِ چند بازیابی
    rerank: { provider: "", model: "" }  # خالی = از base_url استنتاج (dashscope→gte-rerank)
```
- **`indexing`**: تنظیمِ تکه‌تکه‌سازی و محدودیتِ نرخ برای دورزدنِ خطاهای API:
  - `chunk_size: 512` / `chunk_overlap: 50` — اندازه و هم‌پوشانیِ تکه‌ها (توکن).
  - `max_rpm` / `rate_limit_delay_ms: 300` — کنترلِ نرخ برای جلوگیری از 429.
  - `max_retries: 3` / `retry_delay_ms: 1000` — تلاشِ مجدد.
  - `batch_size: 10` — سقفِ آیتم در هر درخواستِ embedding (بزرگ‌تر از حدِ provider نشود).
  - `chunk_strategy: markdown_then_recursive`.

---

## `robots` — ربات‌های پیام‌رسان
از موبایل با WeCom/DingTalk/Lark/Telegram/Slack/Discord/QQ با اپ حرف بزن. هر کدام `enabled` و `auth.mode` (`user_binding` | `service_account`) دارند.
```yaml
robots:
  wechat:   { enabled: false, ... }   # WeChat iLink (اسکنِ QR)
  wecom:    { enabled: false, token, encoding_aes_key, corp_id, secret, agent_id }
  dingtalk: { enabled: false, client_id, client_secret }
  lark:     { enabled: false, app_id, app_secret, verify_token }
  telegram: { enabled: false, bot_token, bot_username, allow_group_messages }
  slack:    { enabled: false, bot_token, app_token }
  discord:  { enabled: false, bot_token, allow_guild_messages }
  qq:       { enabled: false, app_id, client_secret, sandbox }
```
> `auth.mode: service_account` برای ربات‌های اختصاصی؛ باید فرستنده‌های واقعی را با `allowed_external_users` محدود کنی.

---

## `skills_dir` / `agents_dir` / `roles_dir`
```yaml
skills_dir: skills   # هر زیرپوشه یک SKILL.md استاندارد (front matter فقط name, description) + فایل‌های جانبی
agents_dir: agents   # زیرعامل‌های Markdown؛ front matter: name/id/description/tools/bind_role/kind
roles_dir: roles     # هر نقش یک .yaml (مثل roles/CTF.yaml, roles/default.yaml)
```
> **مهم:** مقدارِ `description` در SKILL.md اگر شاملِ `:` باشد باید داخلِ کوتیشن یا با `>-` باشد، وگرنه بارگذاری با خطای YAML panic می‌کند.

---

## `project` — مدیریتِ پروژه و تختهٔ حقایق (Fact Blackboard)
```yaml
project:
  enabled: true
  context_checkpoints: true        # هنگامِ فشرده‌سازی، خلاصهٔ ادامه را به‌عنوان note موقت ذخیره کن
  fact_index_max_runes: 65000      # بودجهٔ کاراکترِ شاخصِ حقایق
  fact_index_path_max_runes: 10000 # بودجهٔ بخشِ روابطِ حقایق
  fact_summary_max_runes: 24000
  default_inject_deprecated: false
```

---

## `update` — به‌روزرسانیِ یک‌کلیکی (اختیاری)
```yaml
# update:
#   repo: https://github.com/AIPentest/CyberStrikeAI.git   # خالی = مخزنِ رسمی
```
- خالی بگذاری، از مخزنِ رسمی آپدیت می‌شود. برای fork خودت فقط همین یک‌جا را عوض کن.
- پوشه‌های غیرِ git (نصبِ استخراج‌شده) هم از صفحهٔ «One-click Update» می‌توانند به worktreeِ git متصل شوند.
- آپدیتِ یک‌کلیکی باینری را می‌سازد و swap می‌کند و منتظرِ ری‌استارتِ systemd می‌ماند (پس در یونیتِ systemd حتماً `Restart=always` باشد).

---

## جمع‌بندیِ نکاتِ عملیاتی

1. **مسیرها نسبی‌اند** → `WorkingDirectory` را درست ست کن.
2. **کلیدهای API** → ترجیحاً از متغیر محیطی (اولویت بالاتر و امن‌تر).
3. **TLS تولید** → گواهی واقعی، نه self-signed.
4. **امنیت** → `tool_guard` و `hitl` را روشن نگه دار؛ `mcp.allow_global_access` را خاموش.
5. **self-update** → کاربرِ سرویس باید مالکِ پوشهٔ نصب باشد و `go` در PATH باشد.
6. **فارسی/چینی نگه‌داشته‌شده** → `web/static` عمداً لوکالِ zh-CN دارد؛ این بخشِ قابلیتِ چندزبانه است و نباید حذف شود.
