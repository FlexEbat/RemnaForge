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
- **Именование пакетов `panelonly`/`panelfull`/`caddynode` (не `panelfull`/`panelonly`-под-Caddy).** В оригинале имена файлов `install_panel.sh`/`install_panel_node.sh` не совпадают с содержимым (см. «Статус» ниже). Наши Go-пакеты названы по содержимому, а не скопированы с именами файлов оригинала. При портировании Caddy-версии этих же файлов действует та же путаница: сверяйтесь с содержимым, не с именем файла.
- **`internal/preflight.EnsureInstalled()`**: точка входа для установки зависимостей (docker/certbot/ufw/cron/unattended-upgrades/BBR). Все install-флоу (`nginxnode`, `panelonly`, `panelfull`, и теперь `caddynode`) обязаны вызывать её первой, до первого интерактивного вопроса пользователю.
- **`ui.Exit()`: единственная точка вызова `os.Exit`.** После `ui.EnableFileLogging()` голый `os.Exit()` теряет буфер pipe. Не добавляйте новые `os.Exit()` вызовы напрямую, только `ui.Exit()`.

## Статус (на момент версии 1.1.2 + WIP)

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
| Управление панелью/нодой | `src/modules/manage_panel.sh` | `internal/managepanel` |
| Установка зависимостей | `install_packages` | `internal/preflight` |
| Удаление | `remove_script` | `internal/uninstall` |
| Переустановка | `choose_reinstall_type` | `internal/reinstall` |
| Backup/Restore (делегирует стороннему `distillium/remnawave-backup-restore`, MIT, как и оригинал) | `install_remnawave.sh:2272-2277` | `internal/backuprestore` |
| Выбор языка при запуске + сохранение | `load_language`/`show_language`/`set_language` | `internal/i18n/language_select.go` |
| Логирование в файл (tee stdout/stderr, включая дочерние процессы) | `log_entry` | `internal/ui/io.go` (`EnableFileLogging`) |
| Генераторы паролей/секретов | части `install_remnawave.sh` | `internal/genutil` |
| Локализация | `src/lang/en.sh`, `src/lang/ru.sh` | `internal/i18n` |
| Главное меню | `install_remnawave.sh` | `internal/menu` |
| CI (build/vet/fmt + integration-тесты на реальной Ubuntu VM) | нет прямого аналога | `.github/workflows/ci.yml` |

¹ Имена файлов не совпадают с содержимым в оригинале: `install_panel_node.sh` = только панель, `install_panel.sh` = панель+нода. Наши пакеты названы по содержимому.

### В работе прямо сейчас (не запушено или частично)

- **`internal/caddynode`**: порт `src/caddy/install_node.sh` (179 строк в реальности, не 180 — предыдущая оценка была на глаз; реально node-only, без путаницы в имени). **Проверено полностью в этой сессии**: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` — все чисто. Оригинал скачан заново с `raw.githubusercontent.com/eGamesAPI/remnawave-reverse-proxy/main/src/caddy/install_node.sh` (доступ появился, в отличие от прошлой сессии) и построчно сверен через `diff` с нормализацией плейсхолдеров: `docker-compose.yml` и `Caddyfile` совпадают с оригиналом дословно, единственная разница — синтаксис обрамления (bash heredoc `<<EOL`/`\$`-экранирование против Go raw-string), что не меняет итоговое содержимое файлов на диске. Также сверены: сигнатура `domain.CheckDomain` и её аргументы, все 15 использованных ключей i18n (присутствуют в en/ru), команды `ufw`, путь `/opt/remnanode`, паттерн HTTP-опроса ноды и `WAITING`-сообщение без анимации спиннера — оба паттерна идентичны уже принятому `internal/nginxnode`, то есть не отклонение специфичное для caddynode, а сквозной для кодовой базы выбор.
  - **Не подключено в меню** (это осознанно пункт 4 очереди, не забыто): в `internal/menu/menu.go` для `install node only` → `Caddy` всё ещё стоит `stub("caddy install_node")`. Модуль готов к подключению, но по плану ждёт, пока допишутся `install_panel_node`/`install_panel` для Caddy, чтобы менять заглушки одним пуском, а не по одной.
  - **Замечена нестыковка в документации**: шапка-комментарий `internal/menu/menu.go` (строки 17-22) утверждает «Scope for this port: Nginx only... Caddy replaced with stub, per project decision» — это устарело, Caddy уже не вне скоупа, просто ещё не готов. Комментарий нужно поправить, когда дойдём до пункта 4 очереди (заодно с заменой самих заглушек), чтобы не редактировать этот файл дважды.
- **Caddy: `install_panel_node.sh`** (466 строк, оригинал ошибочно называет "Install Panel", по факту то же самое что и nginx `install_panel_node.sh`: только панель). Не начато.
- **Caddy: `install_panel.sh`** (526 строк, оригинал называет "Install Panel + Node": панель+нода). Не начато.
- Заглушки Caddy в `internal/menu`, `internal/managepanel/access.go`, `internal/reinstall` нужно заменить на реальные вызовы после портирования трёх модулей выше (сейчас там `stub("caddy ...")`).

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
