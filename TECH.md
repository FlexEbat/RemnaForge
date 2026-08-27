# TECH.md

Служебный документ для того, кто продолжает разработку этого проекта (человек или ИИ-агент). Не путайте с `README.md`: тот для конечного пользователя инструмента, этот для разработки. Обновляйте оба, но по-разному. README описывает, что умеет инструмент. Этот файл фиксирует, что происходит внутри и что делать дальше.

Причина существования файла: 19 августа 2026 песочница разработки один раз обнулила локальную копию проекта (весь `internal/` пропал, кроме файла, над которым шла работа в моменте). Копия на GitHub уцелела, потому что пушили после каждого куска работы. Этот файл существует, чтобы после подобного сброса не пришлось восстанавливать контекст по памяти.

## Если вы читаете это после потери локального состояния

1. Не доверяйте `/home/claude/remnawave-go` (или где там лежит рабочая копия), пока не сверили с git.
2. Источник истины: ветка `dev` на GitHub, `https://github.com/FlexEbat/Remnwave-Easy-Install`, не `main` (в `main` мёржат через PR, `dev` может быть впереди).
3. Клонируйте `dev` заново, соберите (`go build ./...`), прочитайте секцию «Статус» ниже и раздел «Changelog». Это закрывает большую часть контекста без необходимости перечитывать весь код.
4. Не полагайтесь на `git log` в одиночку. Сообщения коммитов подробные, но changelog ниже уже сгруппирован по смыслу и читается быстрее.

## Стек и ограничения окружения

- Go 1.22+, весь код на stdlib, кроме одного скопированного (не через `go.mod`) пакета `internal/publicsuffix` (копия `golang.org/x/net/publicsuffix`, см. `internal/publicsuffix/README.md`).
- Внешние зависимости в `go.mod` не запрещены. Добавляйте их, когда это реально лучше stdlib. См. `README.md#зависимости-go-модуля`.
- Песочница разработки не имеет доступа к `proxy.golang.org`, поэтому `go get` для новых модулей может не сработать напрямую. Копируйте код вручную с зеркала на GitHub, как сделано для `publicsuffix`.
- Песочница разработки не имеет реального Docker/systemd/ufw и публичного домена. Эти пути проверяются в CI (`.github/workflows/ci.yml`, integration-джобы) на настоящей Ubuntu VM GitHub Actions, не локально.
- Оригинальный bash-проект лежит в `/mnt/user-data/uploads/remnawave-reverse-proxy-3_0_0.zip` (если файл ещё доступен в текущей сессии). Это исходник, с которым сверяются построчные комментарии `Original bash (file:N-M): funcName()` по всему коду.

## Правила стиля

Все правила (активный залог, без вводных оборотов, без длинного тире, без наречий-усилителей, комментарий объясняет причину, не соседнюю строку) описаны в `CONTRIBUTING.md`. Здесь они не дублируются, читайте оттуда.

## Замороженные контракты

Трогать только осознанно, с пониманием последствий:

- **Формат ключей i18n.** `internal/i18n/i18n.go` сгенерирован из `src/lang/en.sh`/`ru.sh` оригинала. Не переписывайте вручную. Если нужно поправить конкретную строку, редактируйте точечно и фиксируйте это как осознанное отличие в комментарии над данными (см. существующие примеры в шапке файла). Синтетические строки, которых нет в оригинале (`IN_DEVELOPMENT`, `AvailableTemplates`, `DownloadingBackupRestore`), живут в отдельных мини-картах внизу файла с пометкой, что они не из оригинала.
- **Комментарии `// Original bash (file:N-M): funcName()`.** Обязательны над каждой портированной функцией. Позволяют свериться с оригиналом построчно. Если Go-версия ведёт себя иначе, добавляйте отдельный комментарий `BUG FIX (not a 1:1 port): ...` с объяснением, что и почему изменено.
- **Структура `docker-compose.yml`/`nginx.conf`/`Caddyfile`.** Это внешний контракт с образами `remnawave/backend`, `remnawave/node`, `remnawave/subscription-page`, `nginx`, `caddy`. Копируется из оригинала как есть (переменные окружения, порты, healthcheck). Не импровизируйте с именами переменных: они должны совпадать с тем, что ждут эти образы. При смене мажорной версии панели (см. changelog про `1.1.1`) сверяйтесь с реальным changelog панели, не гадайте.
- **Именование пакетов `panelonly`/`panelfull`/`caddynode`/`caddypanelonly`/`caddypanelfull` (не «под-Caddy»-варианты `panelfull`/`panelonly`).** В оригинале имена файлов `install_panel.sh`/`install_panel_node.sh` не совпадают с содержимым для Nginx (см. «Статус» ниже). Для Caddy — наоборот, имена файлов совпадают с содержимым, это выяснилось только после чтения обоих файлов, не по аналогии с Nginx (см. changelog `1.1.4`). Наши Go-пакеты по-прежнему названы по содержимому, а не скопированы с именами файлов оригинала — не полагайтесь на предположение, что путаница (если она вообще есть) одинакова для разных вебсерверов, сверяйтесь с содержимым файла каждый раз заново.
- **`internal/preflight.EnsureInstalled()`**: точка входа для установки зависимостей (docker/certbot/ufw/cron/unattended-upgrades/BBR). Все install-флоу (`nginxnode`, `panelonly`, `panelfull`, `caddynode`, `caddypanelonly`, `caddypanelfull`) обязаны вызывать её первой, до первого интерактивного вопроса пользователю.
- **`ui.Exit()`: единственная точка вызова `os.Exit`.** После `ui.EnableFileLogging()` голый `os.Exit()` теряет буфер pipe. Не добавляйте новые `os.Exit()` вызовы напрямую, только `ui.Exit()`.

## Статус (на момент версии 1.1.5)

Актуальная версия хранится в `internal/menu/version.go`. Бампайте при каждом пуше в `dev`.

### Реализовано и запушено в `dev`

| Модуль | Оригинал (bash) | Пакет |
|---|---|---|
| Проверка ОС/root | `check_os`/`check_root` | `internal/oscheck` |
| IPv6 | `src/modules/ipv6.sh` | `internal/ipv6` |
| API панели | `src/api/remnawave_api.sh` | `internal/api` |
| Добавление ноды | `src/modules/add_node.sh` | `internal/addnode` |
| Шаблоны selfsteal (с выбором конкретного шаблона, не рандом) | `src/modules/selfsteal_templates.sh` | `internal/selfsteal` |
| Домен-утилиты | части `install_remnawave.sh` | `internal/domain` |
| Сертификаты (полный цикл: check/issue/renew через certbot) | `check_certificates`, `get_certificates`, `handle_certificates` и меню | `internal/certs` |
| Установка ноды за Nginx | `src/nginx/install_node.sh` | `internal/nginxnode` |
| Установка панели без ноды (Nginx) | `src/nginx/install_panel_node.sh`¹ | `internal/panelonly` |
| Установка панели с нодой (Nginx) | `src/nginx/install_panel.sh`¹ | `internal/panelfull` |
| Установка ноды за Caddy | `src/caddy/install_node.sh` | `internal/caddynode` |
| Установка панели без ноды (Caddy) | `src/caddy/install_panel.sh`² | `internal/caddypanelonly` |
| Установка панели с нодой (Caddy) | `src/caddy/install_panel_node.sh`² | `internal/caddypanelfull` |
| Управление панелью/нодой (Nginx и Caddy) | `src/modules/manage_panel.sh` | `internal/managepanel` |
| Установка зависимостей | `install_packages` | `internal/preflight` |
| Удаление | `remove_script` | `internal/uninstall` |
| Переустановка (Nginx и Caddy) | `choose_reinstall_type` | `internal/reinstall` |
| Backup/Restore (делегирует стороннему `distillium/remnawave-backup-restore`, MIT, как и оригинал) | `install_remnawave.sh:2272-2277` | `internal/backuprestore` |
| Выбор языка при запуске + сохранение | `load_language`/`show_language`/`set_language` | `internal/i18n/language_select.go` |
| Логирование в файл (tee stdout/stderr, включая дочерние процессы) | `log_entry` | `internal/ui/io.go` (`EnableFileLogging`) |
| Генераторы паролей/секретов | части `install_remnawave.sh` | `internal/genutil` |
| Локализация | `src/lang/en.sh`, `src/lang/ru.sh` | `internal/i18n` |
| Главное меню (все установочные пункты для Nginx и Caddy подключены) | `install_remnawave.sh` | `internal/menu` |
| CI (build/vet/fmt + integration-тесты на реальной Ubuntu VM) | нет прямого аналога | `.github/workflows/ci.yml` |

¹ Имена файлов не совпадают с содержимым в оригинале для Nginx: `install_panel_node.sh` = только панель, `install_panel.sh` = панель+нода. Наши пакеты названы по содержимому.

² Для Caddy путаница обратная и куда менее коварная: имена файлов совпадают с содержимым (`install_panel.sh` = только панель, `install_panel_node.sh` = панель+нода) — ровно как подсказывает здравый смысл по названию. Это выяснилось только после скачивания и построчного чтения обоих файлов в этой сессии; предыдущая запись в очереди (см. историю правок этого файла) ошибочно предполагала, что Caddy повторяет ту же путаницу, что и Nginx, по аналогии, не проверив. Несмотря на то что для Caddy имена файлов сами по себе не вводят в заблуждение, Go-пакеты всё равно называются по содержимому (`caddypanelonly`/`caddypanelfull`), а не `install_panel_node`-подобно, чтобы схема именования пакетов была одной и той же независимо от того, какой оригинал куда портируется — на неё можно полагаться, не проверяя каждый раз, не перепутан ли конкретный апстрим.

### Caddy: 1.1.3, детали портирования панельных установщиков

- **`internal/caddypanelonly`** (порт `src/caddy/install_panel.sh`, 465 строк) и **`internal/caddypanelfull`** (порт `src/caddy/install_panel_node.sh`, 525 строк) написаны, собраны и сверены построчно с оригиналом в этой сессии, по той же методике, что и `caddynode` (скачивание с `raw.githubusercontent.com`, `diff` с нормализацией плейсхолдеров для `.env`, `docker-compose.yml`, `Caddyfile`). Оба используют уже существующие `internal/api`, `internal/domain`, `internal/genutil`, `internal/selfsteal` без изменений — ни одна функция API не потребовала правок для поддержки Caddy-флоу.
- **BUG FIX (not a 1:1 port), намеренно и до того, как это стало живым багом**: оба `.env`-шаблона используют `APP_SECRET` вместо `JWT_AUTH_SECRET`+`JWT_API_TOKENS_SECRET`, без `SWAGGER_PATH`/`SCALAR_PATH`/`IS_DOCS_ENABLED`, и `docker-compose.yml` пинит `remnawave/backend:3`, а не `:2` — хотя оба реальных Caddy-файла в апстриме на момент скачивания всё ещё писали старые поля и `:2`. Это тот же набор правок, что `internal/panelfull`/`internal/panelonly` (Nginx) уже получили в релизе 1.1.1 из-за обновления панели до v3.2.0 (см. changelog `1.1.1` ниже) — апстримный Caddy-код просто не успели обновить вслед за Nginx-кодом в том же репозитории. Ставить новый Caddy-флоу, дословно копирующий поля, которые текущий образ панели игнорирует (и не писать то единственное поле, которое ему теперь нужно), означало бы сразу выпустить заведомо нерабочую установку. Обе версии считают строку `pg_isready` в healthcheck `remnawave-db` тоже consистентно: апстрим (и Nginx, и Caddy) экранирует её как `\$\${POSTGRES_USER}` (буквальный `${POSTGRES_USER}` для шелла внутри контейнера), но уже принятый и работающий `internal/panelfull`/`internal/panelonly` упрощает это до одинарного `${POSTGRES_USER}` (интерполяция самим docker-compose из `.env`, что даёт тот же результат, так как значение в `.env` и в рантайме контейнера совпадает) — новые Caddy-пакеты сделаны так же, для консистентности со сложившимся прецедентом, а не как отдельное новое решение.
- Домен-валидация: в отличие от `internal/panelonly` (Nginx), которая не вызывает `domain.CheckDomain` для selfsteal-домена (только читает без проверки), оба Caddy-файла (и panel-only, и panel+node) реально вызывают `check_domain "$SELFSTEAL_DOMAIN" true false` — проверено чтением конкретно этих файлов, не скопировано с nginx-аналога по шаблону. `caddypanelonly`/`caddypanelfull` эту проверку вызывают.
- Оба Caddy-пакета, как и `caddynode`, не используют `internal/certs`/`certbot` — Caddy сам обслуживает TLS через встроенный ACME.
- **Подключено в `internal/menu`**: все три заглушки `stub("caddy ...")` (`install node only`, `install panel only`, `install panel+node`, ветка `Caddy`) заменены на реальные вызовы `caddynode.InstallationNode()`, `caddypanelonly.InstallationPanelOnly()`, `caddypanelfull.InstallationPanelNode()`. Шапка-комментарий `menu.go`, утверждавшая «Nginx only, Caddy stubbed per project decision», исправлена.
- README обновлён: Caddy перенесён из «Не реализовано» в таблицу «Готово» (установка).

### Caddy: 1.1.5, управление уже установленным стеком и переустановка

Закрывает пробел, оставленный в 1.1.4: `internal/managepanel/access.go` (временный доступ к панели на 8443) и `internal/reinstall` (переустановка) теперь тоже поддерживают Caddy, не только Nginx.

- **`internal/managepanel/access.go`**: `openPanelAccessCaddy`/`closePanelAccessCaddy` — порт Caddy-веток `open_panel_access()`/`close_panel_access()` из `src/modules/manage_panel.sh:316-370` и `:432-466`. Ключевая деталь оригинала, из-за которой это портируется проще, чем можно подумать: весь `sed`/`grep`, работающий с `Caddyfile`, матчится по буквальному тексту `{$PANEL_DOMAIN}` (плейсхолдер самого Caddy, никогда не подставляется ни бэшем, ни нашим `Sprintf` при генерации), а не по реальному значению домена. Реальный домен нужен только для финальной ссылки на панель и достаётся отдельно, из `docker-compose.yml` (`caddyPanelDomain`). Добавлены helper'ы `addLineAfter`/`removeLineInBlock`, реализующие ровно то же line-based (не brace-depth-aware) сопоставление, что и оригинальный `sed` — здесь это не `BUG FIX`-улучшение поверх оригинала (в отличие от `findServerBlockForDomain`/`addListen8443` для Nginx выше, которые сознательно надёжнее оригинального `grep`/`awk`/`sed`-пайплайна): Caddy-флоу сам пишет предсказуемый, фиксированный формат файла, так что этой прочности с запасом достаточно, дублировать её не было причины.
  - **Задокументированная (не исправленная) странность апстрима**: `close_panel_access()`'s `sed -i "/pattern/a text"` для `bind unix/{$CADDY_SOCKET_PATH}` работает безусловно — вставляет эту строку всегда, независимо от того, была ли она в блоке до `open`. Для `caddypanelfull` (где строка была) это откат к исходному состоянию, всё ок. Для `caddypanelonly` (чей `Caddyfile` для блока `PANEL_DOMAIN` никогда не содержит `bind unix/...`, и чей `remnawave-caddy` вообще не объявляет `CADDY_SOCKET_PATH` в `environment`) `close` после `open` оставляет в файле ссылку на неопределённую переменную окружения Caddy. Это реальный баг апстрима, не внесённый этим портом, и не исправлен здесь: в отличие от несовместимости с v3.2.0 (которая ломает вообще любую установку с первого дня), это ломает только конкретную последовательность panel-only + open + close, то есть даёт точно такое же поведение, какое получил бы пользователь оригинального bash-скрипта сегодня. Задокументировано в коде (`closePanelAccessCaddy`) и здесь, не молча пофикшено.
  - Проверено юнит-тестом (написан, прогнан, удалён — не закоммичен, по правилам `CONTRIBUTING.md`) на реалистичных фикстурах `Caddyfile`/`docker-compose.yml`, сгенерированных по образцу `caddypanelfull`/`caddypanelonly`: open→close даёт точный round-trip для `caddypanelfull`, и демонстрирует задокументированную выше странность для `caddypanelonly`.
  - Общий хвост `close_panel_access()` (проверка/снятие правила `ufw`), дословно продублированный в оригинале в обеих ветках (Nginx и Caddy), вынесен в общую `closePort8443UFW()` вместо копирования — без изменения поведения.
- **`internal/reinstall`**: добавлена `runInstallCaddy(reinstallOption)`, зеркало уже существующей `runInstall` с той же нумерацией `REINSTALL_OPTION` (1=панель+нода, 2=только панель, 3=только нода), но вызывающая `caddypanelfull`/`caddypanelonly`/`caddynode` вместо `panelfull`/`panelonly`/`nginxnode`. Заглушка `stub("caddy reinstall")` в ветке выбора вебсервера (`WEBSERVER_OPTION == "2"`) заменена вызовом.
- README обновлён: строки «Управление панелью и нодой»/«Переустановка» в таблице «Готово» теперь помечены «Nginx и Caddy»; пункт про частичную поддержку Caddy убран из «Не реализовано» — Caddy больше нигде не отставание от Nginx, кроме WARP (общий пробел для обоих вебсерверов).

### Не реализовано (осознанно, не забыто)

- **WARP Native** (`src/modules/warp.sh`, 300 строк) не начато, вне текущего фокуса.
- **Автообновление скрипта** (`update_remnawave_reverse`): для bash это скачать новую версию и заменить себя. Для Go-бинарника это имеет смысл только при наличии GitHub Releases с готовыми сборками, которых пока нет.
- **Механизм самоустановки** (`install_script_if_missing`): нет `install.sh`/готового бинарника для установки, только сборка из исходников. Добавлять его стоит только после появления GitHub Releases (см. пункт про автообновление, они логически связаны: сначала self-install, потом autoupdate может по-настоящему что-то обновлять).
- **Custom extensions by legiz** не заглушка. Убрано из меню совсем по явной просьбе (не часть Remnawave).

## Definition of Done для одной единицы работы

Перед тем как считать кусок работы законченным и коммитить/пушить:

1. `go build ./...`: чисто.
2. `go vet ./...`: чисто.
3. `gofmt -l .`: пустой вывод.
4. `go test ./...`: чисто. Постоянные тесты живут в `_test.go`; одноразовые проверочные тесты пишутся, гоняются, потом удаляются и не коммитятся (см. `CONTRIBUTING.md`).
5. Если менялось поведение, видимое пользователю (новый пункт меню, новый файл конфига, новое сообщение), обновить `README.md` (таблица «Статус портирования», при необходимости «Требования»/«Использование»).
6. Версия в `internal/menu/version.go` увеличена.
7. Этот файл (`TECH.md`) обновлён: пункт из «В работе» переехал в «Реализовано», новый changelog-пункт добавлен.
8. Закоммичено с подробным сообщением (что изменилось и почему, не только «что»).
9. Запушено в `dev`, пуш проверен независимым `git clone` + `go build`. Не доверяйте локальному состоянию после пуша: git остаётся источником истины.

## Changelog

Обратный хронологический порядок, самое новое сверху. Один пункт соответствует одной пушнутой единице работы. Синхронизировано с `git log` на ветке `dev`.

### 1.1.5: Caddy для управления уже установленным стеком и переустановки
Закрывает последний Caddy-пробел, оставленный в 1.1.4. `internal/managepanel/access.go`: `openPanelAccessCaddy`/`closePanelAccessCaddy` порт `open_panel_access()`/`close_panel_access()`'s Caddy-веток из `src/modules/manage_panel.sh`. Caddyfile-паттерны матчатся по буквальному тексту `{$PANEL_DOMAIN}` (плейсхолдер самого Caddy), не по реальному домену — это упростило порт. Задокументирована (не исправлена) реальная странность апстрима: `close` после `open` на panel-only Caddy-инсталляции оставляет ссылку на неопределённую `CADDY_SOCKET_PATH`, потому что `sed`-вставка в оригинале безусловна — воспроизведено как есть, с комментарием в коде. `internal/reinstall`: добавлена `runInstallCaddy`, зеркало `runInstall` для трёх Caddy install-пакетов. Проверено юнит-тестом на реалистичных фикстурах (написан, прогнан, удалён, не закоммичен). `go build`/`vet`/`fmt`/`test` чистые. README обновлён: Caddy больше нигде не отстаёт от Nginx, кроме WARP.

### 1.1.4: Caddy panel-only и panel+node установщики, подключены в меню
Добавлены `internal/caddypanelonly` (порт `install_panel.sh`, 465 строк) и `internal/caddypanelfull` (порт `install_panel_node.sh`, 525 строк). Обнаружено и задокументировано: для Caddy имена файлов совпадают с содержимым (в отличие от Nginx, где они перепутаны), это выяснилось только после чтения обоих файлов, а не по аналогии с nginx. Применены те же v3.2.0-совместимые правки .env/docker-compose (`APP_SECRET`, `backend:3`), что уже были в `panelfull`/`panelonly` — апстримные Caddy-файлы их ещё не получили. Все три Caddy-заглушки в `internal/menu` заменены на реальные вызовы; шапка-комментарий `menu.go` про «Nginx only» исправлена. `go build`/`vet`/`fmt`/`test` чистые, шаблоны сверены `diff`'ом построчно с оригиналом. Не затронуто: Caddy-ветки в `internal/managepanel/access.go` и `internal/reinstall` — это отдельная задача (управление уже установленным стеком), не блокировавшаяся этой работой. README обновлён.

### 1.1.3: caddynode протестирован и сверен с оригиналом
Код `internal/caddynode` не менялся (изменений по сути не потребовалось): `go build`/`go vet`/`gofmt`/`go test` чистые. Оригинал `src/caddy/install_node.sh` скачан заново и построчно сверен через `diff` (нормализация bash-плейсхолдеров ↔ Go `%s`): `docker-compose.yml` и `Caddyfile` идентичны оригиналу, кроме синтаксиса heredoc-обрамления, не влияющего на итоговый файл. Проверены отдельно: аргументы `domain.CheckDomain`, все 15 i18n-ключей, `ufw`-команды, паттерны HTTP-опроса и `WAITING`-сообщения (совпадают с уже принятым `internal/nginxnode`). Модуль по-прежнему не подключён в меню, это следующий пункт очереди (после Caddy `install_panel_node`/`install_panel`), не текущего пуша.

### 1.1.2: gzip-сжатие
Добавлен стандартный блок gzip (`gzip on`, `gzip_vary`, `gzip_proxied`, `gzip_comp_level 6`, `gzip_min_length 1024`, `gzip_types` для JS/JSON/XML/wasm/шрифтов/SVG/CSS/plain text) в начало `nginx.conf` перед первым `server{}`-блоком во всех трёх nginx install-флоу (`nginxnode`, `panelonly`, `panelfull`).

### 1.1.1: совместимость с Remnawave Panel v3.2.0 (мажорный релиз)
Пользователь прислал changelog панели, сверил с каждой функцией `internal/api` вручную. Реально задело:
- `GET /api/keygen`: `pubKey` → `secretKey` (`GetPublicKey`).
- `DELETE`-эндпоинты: `200`+JSON тело → `204` без тела на успехе. Был реальный баг: `DeleteConfigProfile` считал пустое тело провалом. Раньше это было верно, теперь означало бы «любой успех = провал». Добавлен `makeAPIRequestWithStatus`, `DeleteConfigProfile` теперь проверяет код ответа, не тело.
- `.env`: `JWT_AUTH_SECRET`→`APP_SECRET`, убраны `JWT_API_TOKENS_SECRET`/`SWAGGER_PATH`/`SCALAR_PATH`/`IS_DOCS_ENABLED` (в `panelfull` и `panelonly`).
- `docker-compose.yml`: `remnawave/backend:2` → `:3`.
- Добавлен постоянный regression-тест `internal/api/panelv3_test.go` (httptest, без сети).
- Остальное из changelog панели (numeric id вместо uuid у пользователей, `ip-control`→`connections`, external squads responseHeaders split, typed errors, bulk-эндпоинты) не касается ни одного вызова, который мы делаем. Проверено построчно.

### 1.1.0: Backup и Restore
Оригинал сам ничего не бэкапит: скачивает и запускает сторонний `distillium/remnawave-backup-restore` (MIT, ~3600 строк, свои Google Drive/S3/Telegram). Переписывать на Go не стали, потому что это не тот масштаб задачи (это отдельный чужой проект). `internal/backuprestore` повторяет оригинальную логику: скачать (если не скачан) → `chmod +x` → интерактивно передать управление, тот же паттерн, что уже использован для `docker exec -it` в `managepanel`. Проверено вживую: скачивание реального скрипта работает, `rw-backup` symlink-путь совпадает с тем, что проверяет наш код.

### Пять правок в один пуш (без отдельного номера версии, до перехода на 1.1.x)
- Выбор языка при запуске и сохранение в файл (`internal/i18n/language_select.go`). Раньше `main()` хардкодил английский и никогда не спрашивал.
- Логирование в файл (`ui.EnableFileLogging`, порт `log_entry()`). Раньше `LogClear()` был мёртвым кодом, потому что ничего не писало в лог-файл. Заодно нашли: голый `os.Exit()` после включения tee-логирования терял последнюю строку вывода (буфер pipe не успевал долиться). Централизовали выход через `ui.Exit()`.
- Выбор конкретного selfsteal-шаблона вместо случайного при интерактивном вызове из меню (`internal/selfsteal.InteractiveInstall`). `RandomHTML` для автоматических install-флоу остался случайным: там диалог не уместен.
- `.github/workflows/ci.yml`: build/vet/fmt/тесты на каждый пуш плюс два integration-джоба на реальной Ubuntu VM (`preflight`, `oscheck`), потому что локальная песочница не даёт проверить Docker/systemd/ufw.
- README переписан с нуля по best-practice структуре (makeareadme.com, awesome-readme), с явной атрибуцией форку eGames.

### 1.0.0: uninstaller, reinstall, style pass
- `internal/uninstall`: порт `remove_script()`.
- `internal/reinstall`: порт `choose_reinstall_type()`/`reinstall_remnawave()`.
- Найдены и исправлены при этом: перепутанные заголовки двух подменю в самом оригинале (`show_panel_access()`/`show_manage_certificates()` печатали не те `LANG[MENU_N]`), три места с незаполненным `%s` в сообщениях, мёртвая переменная `marker`, диапазон «0-11» в `INVALID_CHOICE` (общий на два разных по размеру меню, убран из текста совсем), диапазон «0-5» вместо «0-4» в install-подменю.
- `CONTRIBUTING.md`: правила стиля, применены по всей кодовой базе.

### 0.1.0: install_packages, полный nginx-путь
`internal/preflight`: установка docker/certbot/ufw/cron/unattended-upgrades/BBR на чистом сервере. С этой версии путь «чистый VPS → рабочая панель» замкнут целиком для nginx-варианта.

### 0.0.x: базовый функционал
Построчный порт bash → Go: IPv6, API-клиент, добавление ноды, selfsteal-шаблоны, домен-утилиты, сертификаты (certbot: Cloudflare DNS-01/ACME HTTP-01/Gcore DNS-01), три nginx install-флоу, управление панелью/нодой, локализация, главное меню. Найденные и исправленные баги оригинала за этот период: `ExtractDomain` не понимал составные TLD (правильный Public Suffix List вместо «последние два лейбла»), `$dir` в сообщениях об ошибках разворачивался в пустую строку при загрузке словаря переводов, отсутствие preflight-проверки docker/certbot перед установкой, недетерминированный порядок вывода из-за итерации по Go `map`, хрупкий парсинг `nginx.conf` на нестандартном форматировании.

## Работа в очереди (порядок по приоритету)

1. Доделать и протестировать `internal/caddynode` (уже написан, см. «В работе прямо сейчас»).
2. Портировать Caddy `install_panel_node.sh` (только панель, 466 строк) → пакет по аналогии с `panelonly`.
3. Портировать Caddy `install_panel.sh` (панель+нода, 526 строк) → пакет по аналогии с `panelfull`.
4. Заменить Caddy-заглушки в `menu`/`managepanel`/`reinstall` на реальные вызовы.
5. Обновить README: убрать Caddy из «Не реализовано», добавить в таблицу «Готово».
6. Дальше действуем по указанию пользователя: WARP, self-install/autoupdate, или что скажут.
