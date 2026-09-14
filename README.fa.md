[English](README.md) | [Русский](README.ru.md) | [中文](README.zh.md) | **فارسی**

# RemnaForge

> RemnaForge نسخه‌ی تغییرنام‌یافته‌ی Remnawave Easy-Install (فورک eGames) است. این پروژه تغییرنام یافته و به‌عنوان یک فورک مستقل از [remnawave-reverse-proxy](https://github.com/eGamesAPI/remnawave-reverse-proxy) توسعه‌اش ادامه دارد.

RemnaForge از طریق یک منوی متنی تعاملی، [Remnawave](https://docs.rw/) را روی سرور نصب و نگهداری می‌کند: پنل، یک نود، یا هر دو با هم، پشت Nginx یا Caddy. با Go نوشته شده و به‌صورت یک باینری واحد بدون نیاز به ران‌تایم جانبی عرضه می‌شود.

## فهرست مطالب

- [قبل از نصب حتماً بخوانید](#قبل-از-نصب-حتماً-بخوانید)
- [مشکلات شناخته‌شده](#مشکلات-شناخته‌شده)
- [امکانات](#امکانات)
- [پیش‌نیازها](#پیش‌نیازها)
- [نصب](#نصب)
- [اولین اجرا](#اولین-اجرا)
- [راهنمای منو](#راهنمای-منو)
- [فایل‌هایی که این ابزار می‌سازد و تغییر می‌دهد](#فایل‌هایی-که-این-ابزار-می‌سازد-و-تغییر-می‌دهد)
- [لاگ‌ها](#لاگ‌ها)
- [ساختار مخزن](#ساختار-مخزن)
- [بیلد و تست](#بیلد-و-تست)
- [مشارکت](#مشارکت)
- [مجوز](#مجوز)

## قبل از نصب حتماً بخوانید

RemnaForge پیکربندی موردنیاز [Xray-core](https://github.com/XTLS/Xray-core) (پروتکل VLESS، TLS/REALITY، رمزنگاری لایه‌ی انتقال) و پنل [Remnawave](https://github.com/remnawave/panel) را می‌سازد، اما خودش هیچ‌کدام از این دو نیست و صحت واقعی یا به‌روز بودن تنظیمات امنیتی تولیدشده را، فراتر از چیزی که پنل و نود برای بالا آمدن نیاز دارند، بررسی نمی‌کند.

پروتکل VLESS و مکانیزم‌های مرتبط با آن (XTLS، REALITY، VLESS Encryption) به‌سرعت در حال تغییرند؛ فرمت پیکربندی و توصیه‌های مربوط به تنظیمات پیش‌فرض امن، از نسخه‌ای به نسخه‌ی دیگر تغییر می‌کند. مقداری که دیروز امن بود، ممکن است امروز کافی نباشد. پیش از راه‌اندازی یک سرور در محیط واقعی، به‌خصوص وقتی کاربران واقعی در کار باشند نه یک محیط تست، این منابع را بخوانید:

- سوالات متداول رسمی درباره‌ی VLESS Encryption: [xraycore.org/en/misc/vless-encryption](https://web.archive.org/web/20260323081134/https://xraycore.org/en/misc/vless-encryption/) (نسخه‌ی آرشیوشده در archive.org، چون آدرس این سایت گاهی تغییر می‌کند).
- بحث مربوط به مهاجرت به VLESS Encryption در خود Xray-core: [github.com/XTLS/Xray-core/discussions/4113](https://github.com/XTLS/Xray-core/discussions/4113).
- کد منبع Xray-core: [github.com/XTLS/Xray-core](https://github.com/XTLS/Xray-core)، از جمله [pull request](https://github.com/XTLS/Xray-core/pulls) و [issue](https://github.com/XTLS/Xray-core/issues) های باز — مشکلات پیکربندی و محدودیت‌های شناخته‌شده‌ی فعلی معمولاً پیش از این‌که به مستندات برسند، همین‌جا مطرح می‌شوند.
- راهنمای رسمی نصب و پیکربندی: [xtls.github.io/en/document/install.html](https://xtls.github.io/en/document/install.html).
- بررسی مستقیم فرمت `transport_method` در کد منبع، برای زمانی که مستندات به‌تنهایی کافی نیست: [infra/conf/transport_method.go#L454](https://github.com/XTLS/Xray-core/blob/7d214f8b094f75322fa3990f8aadad1c912f24f5/infra/conf/transport_method.go#L454).
- مستندات و کد منبع خود پنل: [github.com/remnawave/panel](https://github.com/remnawave/panel)، [docs.rw](https://docs.rw).

اگر چیزی در این منابع با محتوای این README تناقض داشت، به منابع اعتماد کنید نه به این فایل، و یک issue باز کنید.

**پنل و نود را به‌روز نگه دارید.** فایل `docker-compose.yml` که RemnaForge می‌نویسد از تگ‌های شناور ایمیج استفاده می‌کند (`remnawave/backend:3`، `remnawave/node:latest`)، بنابراین اجرای `docker compose pull && docker compose up -d` (گزینه‌ی منوی «Manage Panel/Node» → Update) آخرین پچ همان نسخه‌ی اصلی را بدون نیاز به نصب مجدد دریافت می‌کند. پنل و نود هرکدام کانال اطلاع‌رسانی جداگانه‌ای دارند که رفع مشکلات مهم، از جمله رفع مشکلات امنیتی، را پوشش می‌دهد: [announces](https://f.docs.rw/c/announces/14)، [پنل](https://f.docs.rw/c/announces/rw-panel/16)، [نود](https://f.docs.rw/c/announces/rw-node/17). این کانال‌ها را به‌طور دوره‌ای بررسی کنید — RemnaForge خودش این موارد را پیگیری یا اطلاع‌رسانی نمی‌کند.

## مشکلات شناخته‌شده

در حال حاضر هیچ باگ فعال شناخته‌شده‌ای وجود ندارد.

**نصب‌های Caddy همراه با نود (چه هم‌مکان با پنل، چه مستقل) در اولین اجرا ایمیج Caddy را خودشان می‌سازند**، نه این‌که از ایمیج رسمی استفاده کنند: Xray ترافیک fallback مربوط به Reality را با PROXY protocol روی یک unix socket بسته‌بندی می‌کند، جایی که Caddy برای همه‌ی دامنه‌های خودش گوش می‌دهد، و ایمیج رسمی `caddy` ماژول لازم برای این کار را ندارد (`github.com/mastercactapus/caddy2-proxyprotocol`). فایل `docker-compose.yml` برای `internal/caddynode`/`internal/caddypanelfull` اکنون خودش این ایمیج را از طریق `xcaddy` می‌سازد (`docker compose up -d --build`)، بنابراین اولین اجرا چند دقیقه بیشتر از حالت عادی طول می‌کشد — چون داکر در حال کامپایل ایمیج است، نه فقط دانلود آن.

## امکانات

- نصب پنل Remnawave، یک نود، یا هر دو روی یک سرور.
- امکان انتخاب بین دو وب‌سرور: Nginx یا Caddy.
- در حالت Nginx، صدور و تمدید گواهی TLS از طریق `certbot`: Cloudflare DNS-01، ACME HTTP-01، Gcore DNS-01. در حالت Caddy، گواهی‌ها را خود Caddy از طریق کلاینت داخلی ACME صادر و تمدید می‌کند.
- پروفایل پیکربندی هر نود به‌طور پیش‌فرض فقط یک inbound دارد: VLESS+Reality (`Raw`). هر نود (چه هم‌مکان با پنل، چه مستقل) از قبل آماده است که دو inbound دیگر هم فعال کند: Hysteria2 (`HYSTERIA-BBR`، که TLS آن را خود Xray مدیریت می‌کند) و VLESS+XHTTP (`XHTTP-TLS` روی مسیر `/api/v2/stream-events`) — location/route مربوطه در nginx.conf یا Caddyfile و همچنین mount شدن گواهی نود داخل کانتینر از قبل آماده است، و بعداً از طریق گزینه‌ی منوی «Manage Node Profile» فعال می‌شود.
- ثبت یک نود روی پنلی که از قبل در حال اجراست، از طریق HTTP API آن.
- نصب یک قالب HTML (تصادفی، یا انتخاب دستی از چند منبع) روی دامنه‌ی selfsteal برای پنهان‌سازی اتصال‌های غیرمجاز.
- مدیریت استک نصب‌شده: شروع، توقف، به‌روزرسانی ایمیج‌ها، مشاهده‌ی لاگ‌های `docker compose`، CLI داخلی `remnawave` درون کانتینر، باز کردن موقت پنل روی پورت 8443 برای دسترسی بدون دامنه.
- نصب مجدد پنل یا نود روی نصب فعلی، با همان انتخاب وب‌سرور.
- روشن و خاموش کردن IPv6 در سطح سیستم‌عامل.
- نصب پیش‌نیازها روی یک سرور تازه، پیش از اولین نصب: `docker`، `docker compose`، `certbot`، `ufw`، `cron`، `unattended-upgrades`، و فعال‌سازی BBR.
- پشتیبان‌گیری و بازیابی پنل — با واگذاری این کار به ابزار شخص‌ثالث [distillium/remnawave-backup-restore](https://github.com/distillium/remnawave-backup-restore) (تحت مجوز MIT)، نه با پیاده‌سازی مستقیم آن.
- حذف کامل نصب (کانتینرها، ایمیج‌ها، volumeها) یا فقط حذف وضعیت خود RemnaForge.
- رابط کاربری به دو زبان انگلیسی و روسی، با انتخاب زبان در اولین اجرا.

## پیش‌نیازها

- Debian 11 یا جدیدتر، یا Ubuntu 22.04 LTS یا جدیدتر. نسخه‌ی سیستم‌عامل از روی `/etc/os-release` خوانده می‌شود، نه از روی یک فهرست ثابت از نام‌های کدی، پس نسخه‌های فرعی جدید Debian یا Ubuntu نیازی به به‌روزرسانی RemnaForge ندارند.
- دسترسی root (در زمان اجرا بررسی می‌شود).
- دامنه‌(هایی) که از قبل به IP سرور اشاره می‌کنند — برای نصب پنل یا نود پشت Nginx. پشت Caddy گواهی را خود Caddy صادر می‌کند، اما دامنه باز هم باید به‌درستی به سرور resolve شود.
- Go نسخه‌ی 1.22 به بالا — فقط برای بیلد از سورس، روی سرور مقصد لازم نیست.

نیازی به نصب از پیش Docker، `docker compose`، `certbot` یا `ufw` نیست: RemnaForge خودش هر چیزی را که کم باشد، در اولین باری که یکی از مسیرهای نصب به آن نیاز داشته باشد، نصب می‌کند.

## نصب

فعلاً باینری یا پکیج از پیش کامپایل‌شده وجود ندارد.

```bash
git clone https://github.com/FlexEbat/RemnaForge.git
cd RemnaForge
go build -o remnaforge ./cmd/remnawave
sudo mv remnaforge /usr/local/bin/
```

تنها وابستگی خارجی در `go.mod` پکیج `golang.org/x/net` است (بخش `publicsuffix`، برای تشخیص درست دامنه‌ی پایه زیر public suffix های چندبخشی مانند `.co.uk`). بقیه‌ی کد فقط از کتابخانه‌ی استاندارد Go استفاده می‌کند.

## اولین اجرا

```bash
sudo remnaforge
```

در همان اولین اجرا، ابزار زبان رابط کاربری (English/Русский) را می‌پرسد و انتخاب را در مسیر `/usr/local/remnawave_reverse/selected_language` ذخیره می‌کند. اجراهای بعدی مستقیماً با همان زبان شروع می‌شوند.

## راهنمای منو

```
1. Install Remnawave Components   — نصب: پنل+نود، فقط پنل، افزودن نود، فقط نود
2. Reinstall panel/node            — حذف استک فعلی و نصب مجدد
3. Manage Panel/Node                — شروع/توقف/به‌روزرسانی/لاگ/CLI/دسترسی موقت روی 8443
4. Install random template          — تغییر قالب selfsteal روی یک نود
5. WARP Native
6. Backup and Restore                — ابزار شخص‌ثالث distillium/remnawave-backup-restore
7. Manage IPv6                       — روشن یا خاموش کردن IPv6
8. Manage certificates domain        — تمدید یا صدور مجدد دستی گواهی
9. Manage Node Profile               — روشن/خاموش کردن Hysteria2/XHTTP روی نودی که از قبل ثبت شده
10. Check for updates script
11. Remove script                    — حذف RemnaForge و/یا استک نصب‌شده
0. Exit
```

گزینه‌ی 1، بعد از انتخاب نوع نصب (پنل+نود / فقط پنل / افزودن نود به پنل موجود / فقط نود)، Nginx یا Caddy را می‌پرسد، سپس بقیه‌ی موارد را به‌صورت تعاملی می‌پرسد: دامنه‌ها، IP پنل هنگام نصب یک نود مستقل، روش صدور گواهی.

گزینه‌ی 9 مثل بخش «افزودن نود» در گزینه‌ی 1 عمل می‌کند: مستقیماً URL پنل، توکن API و نام پروفایل پیکربندی را می‌پرسد، نه این‌که فایل `.env` محلی را بخواند — چون این پروفایل می‌تواند متعلق به هر پنلی باشد، نه فقط پنلی که روی همین سرور نصب شده. یک نود به‌طور پیش‌فرض فقط `Raw` (VLESS+Reality) دارد؛ این گزینه با یک منوی چک‌باکسی (عدد برای تغییر وضعیت، Enter برای اعمال) امکان افزودن یا حذف `Hysteria2`/`XHTTP` را می‌دهد، بدون این‌که به کلیدهای از قبل صادرشده‌ی inbound هایی که روشن می‌مانند دست بزند. با روشن کردن `Hysteria2` می‌پرسد آیا گواهی این نود به‌صورت wildcard صادر شده یا نه — چون این گزینه‌ی منو خودش این نود را نصب نکرده و راه دیگری برای فهمیدن این موضوع ندارد.

## فایل‌هایی که این ابزار می‌سازد و تغییر می‌دهد

- `/opt/remnawave/` یا `/opt/remnanode/` — فایل‌های `docker-compose.yml`، `.env`، `nginx.conf` یا `Caddyfile` مربوط به استک نصب‌شده.
- `/usr/local/remnawave_reverse/` — وضعیت داخلی خود RemnaForge: زبان انتخاب‌شده، فایل لاگ، نشانه‌ای برای نصب‌شدن پیش‌نیازها.
- `/etc/letsencrypt/` — گواهی‌ها و تنظیمات تمدید، فقط زمانی که Nginx همراه با certbot انتخاب شده باشد.
- `/etc/sysctl.conf`، قوانین `ufw` — هنگام روشن/خاموش کردن IPv6 و در زمان نصب اولیه‌ی پیش‌نیازها.
- `/var/www/html/` — قالب HTML مربوط به دامنه‌ی selfsteal.

حذف کامل (گزینه‌ی 11 منو، «wipe everything») مسیر `/opt/remnawave` یا `/opt/remnanode` را همراه با کانتینرها، ایمیج‌ها و volumeهای آن حذف می‌کند. مسیر `/etc/letsencrypt` را دست‌نخورده باقی می‌گذارد — گواهی‌ها روی دیسک باقی می‌مانند.

## لاگ‌ها

تمام خروجی‌ها، از جمله خروجی پردازش‌های فرزند (`docker`، `certbot`، `ufw`)، هم‌زمان روی صفحه نمایش داده می‌شوند و در `/usr/local/remnawave_reverse/remnawave_reverse.log` ذخیره می‌شوند.

## ساختار مخزن

```text
cmd/remnawave/          نقطه‌ی ورود: لاگ فایل، انتخاب زبان، بررسی سیستم‌عامل/root، منوی اصلی
templates/               قالب‌های آماده‌ی Xray JSON برای اشتراک در پنل (به‌صورت خودکار نصب نمی‌شوند، به templates/README.md مراجعه کنید)
internal/
  addnode/               ثبت یک نود روی پنل از طریق API
  api/                    کلاینت HTTP برای پنل Remnawave
  backuprestore/          دانلود و اجرای ابزار شخص‌ثالث پشتیبان‌گیری/بازیابی
  caddynode/              نصب یک نود مستقل پشت Caddy
  caddypanelfull/         نصب پنل همراه با نود هم‌مکان پشت Caddy
  caddypanelonly/         نصب پنل بدون نود پشت Caddy
  certs/                  صدور و تمدید گواهی TLS برای Nginx (certbot)
  domain/                 استخراج دامنه‌ی پایه، بررسی DNS، تشخیص Cloudflare
  genutil/                تولید رمز عبور، نام کاربری، و کلیدهای مخفی
  i18n/                   متن‌های رابط کاربری به انگلیسی/روسی، انتخاب و ذخیره‌ی زبان
  ipv6/                   روشن و خاموش کردن IPv6 در سطح سیستم‌عامل
  managepanel/            مدیریت پنل و نود نصب‌شده
  menu/                   منوی اصلی و مسیریابی دستورها
  nginxnode/              نصب یک نود مستقل پشت Nginx
  nodeprofile/            روشن/خاموش کردن Hysteria2/XHTTP روی پروفایل پیکربندی یک نود
  oscheck/                بررسی نسخه‌ی سیستم‌عامل و دسترسی root
  panelfull/              نصب پنل همراه با نود هم‌مکان پشت Nginx
  panelonly/              نصب پنل بدون نود پشت Nginx
  preflight/              نصب docker/certbot/ufw/cron پیش از اولین اجرا
  reinstall/              نصب مجدد پنل یا نود روی نصب فعلی
  selfsteal/              قالب‌های HTML برای دامنه‌ی selfsteal
  uninstall/              حذف RemnaForge و/یا استک نصب‌شده
  ui/                     رنگ‌های ترمینال، دریافت ورودی، لاگ فایل
```

## بیلد و تست

```bash
go build ./...
go vet ./...
gofmt -l .      # خروجی خالی یعنی فرمت‌بندی درست است
go test ./...
```

بخشی از تست‌ها به محیط واقعی‌ای نیاز دارند که روی ماشین توسعه وجود ندارد: یک Docker daemon واقعی، `systemd`، `apt`، `ufw`. این تست‌ها با تگ بیلد `integration` مشخص شده‌اند، در اجرای معمولی `go test ./...` قرار نمی‌گیرند، و در CI (`.github/workflows/ci.yml`) روی یک ماشین یک‌بارمصرف Ubuntu در GitHub Actions اجرا می‌شوند، با هر push یا PR به `main` و `dev`. برای اجرای دستی:

```bash
go test -tags=integration ./internal/preflight/...
```

## مشارکت

سبک کد، commit و کامنت‌نویسی در فایل [CONTRIBUTING.md](CONTRIBUTING.md) آمده است. مستندات فنی داخلی (وضعیت پیاده‌سازی هر ماژول، تصمیمات معماری ثبت‌شده) در فایل [TECH.md](TECH.md) قرار دارد.

## مجوز

[GNU GPLv3](LICENSE).
