# TECH.md

**Версия: v1.1.7** (2026-08-29)

Changelog (новое сверху, один пункт на одну пушнутую единицу работы):

- **v1.1.7**: три точечных бага, найденных ревью (не 1:1-порт баги оригинала, кроме одного, помеченного отдельно):
  - `internal/uninstall.removeScriptAndPanel()`: ошибка `docker compose down` репортилась под чужим именем — `LANG[CHANGE_DIR_FAILED]` ("Failed to change to directory %s"), скопированным из оригинального `cd dir || { ... }` guard'а, которого в Go-порте нет (используется `cmd.Dir`, не `cd`). Заведён новый ключ `DOCKER_COMPOSE_DOWN_FAILED` (en+ru), используется вместо него.
  - `internal/selfsteal.extractZip()`: добавлена защита от Zip Slip — путь каждой записи архива проверяется на выход за пределы каталога распаковки перед записью. Оригинал шеллился в `unzip`, который по умолчанию такое отклоняет; `filepath.Join` в Go — нет.
  - `internal/managepanel.closePanelAccessCaddy()`: **исправлен баг апстрима**, задокументированный как неисправленный в v1.1.5 — строка `bind unix/{$CADDY_SOCKET_PATH}` возвращалась в Caddyfile безусловно при `close`, даже на `internal/caddypanelonly`-инсталляции, где `CADDY_SOCKET_PATH` нигде не определена. Теперь строка возвращается только если `docker-compose.yml` этой инсталляции реально определяет `CADDY_SOCKET_PATH` (т.е. только на `internal/caddypanelfull`).
- **v1.1.6**: `internal/publicsuffix` (вендоренная копия) удалён. `internal/domain` импортирует `golang.org/x/net/publicsuffix` напрямую как зависимость `go.mod`, через `replace` на `github.com/golang/net` (см. раздел 2). README переписан: без бейджей, без раздела статуса портирования, без абзаца про происхождение из bash.
- **v1.1.5**: Caddy: `openPanelAccessCaddy`/`closePanelAccessCaddy` (`internal/managepanel`), `runInstallCaddy` (`internal/reinstall`). Задокументирован неисправленный баг апстрима: `close` после `open` на panel-only Caddy-инсталляции оставляет ссылку на неопределённую `CADDY_SOCKET_PATH`. **Исправлено в v1.1.7.**
- **v1.1.4**: `internal/caddypanelonly`/`internal/caddypanelfull` (установка панели без ноды / с нодой за Caddy), подключены в `internal/menu`. Для Caddy имена файлов совпадают с содержимым, для Nginx нет (см. раздел 4).
- **v1.1.3**: `internal/caddynode` протестирован и сверен построчно с оригиналом `src/caddy/install_node.sh`.
- **v1.1.2**: gzip-блок в `nginx.conf` во всех трёх nginx install-флоу (`nginxnode`, `panelonly`, `panelfull`).
- **v1.1.1**: совместимость с Remnawave Panel v3.2.0: `APP_SECRET` вместо `JWT_AUTH_SECRET`+`JWT_API_TOKENS_SECRET`, `DELETE`-эндпоинты возвращают `204` без тела, `remnawave/backend:3`. Регрессионный тест `internal/api/panelv3_test.go`.
- **v1.1.0**: Backup и Restore. Делегирует стороннему `distillium/remnawave-backup-restore` (MIT), как и оригинал.
- **v1.0.x**: выбор языка интерфейса и сохранение в файл, логирование в файл (`ui.EnableFileLogging`), выбор конкретного selfsteal-шаблона в интерактивном меню, CI (`.github/workflows/ci.yml`), README с нуля, `internal/uninstall`, `internal/reinstall`.
- **v0.1.0**: `internal/preflight`: установка docker/certbot/ufw/cron/unattended-upgrades/BBR на чистом сервере. Путь от чистого VPS до рабочей панели замкнут для nginx-варианта.
- **v0.0.x**: базовый порт: IPv6, API-клиент панели, добавление ноды, selfsteal-шаблоны, домен-утилиты, сертификаты (Cloudflare DNS-01/ACME HTTP-01/Gcore DNS-01), три nginx install-флоу, управление панелью/нодой, локализация, главное меню.

---

## 0. Как читать этот файл

Служебный документ для того, кто продолжает разработку (человек или ИИ-агент). `README.md` описывает, что умеет инструмент для конечного пользователя. Этот файл фиксирует, что происходит внутри и что делать дальше. Обновляйте оба, каждый по своему назначению.

Если читаете это после потери локального состояния песочницы:

1. Не доверяйте рабочей копии на диске, пока не сверили с git.
2. Источник истины: ветка `dev` на `https://github.com/FlexEbat/Remnwave-Easy-Install`, не `main` (в `main` мёржат через PR, `dev` может быть впереди).
3. Клонируйте `dev` заново, соберите `go build ./...`, прочитайте changelog в шапке и раздел 5 («Статус портирования»). Это закрывает большую часть контекста без перечитывания всего кода.
4. Changelog в шапке уже сгруппирован по смыслу. Читайте его, не `git log` построчно.

---

## 1. Проект

Remnawave Easy-Install ставит и обслуживает [Remnawave](https://remna.st) на сервере через интерактивное текстовое меню: панель, ноду или обе части сразу, за Nginx или за Caddy.

Форк [eGamesAPI/remnawave-reverse-proxy](https://github.com/eGamesAPI/remnawave-reverse-proxy), оригинал на bash (около 7300 строк). Этот проект переносит ту же функциональность на Go, файл за файлом, и достраивает то, что оригинал ещё не покрывает в Go-версии. Каждая портированная функция несёт комментарий `Original bash (file:N-M): funcName()` с номерами строк исходника (раздел 4).

---

## 2. Стек и ограничения среды

- Go 1.22+, весь код на stdlib, кроме `golang.org/x/net/publicsuffix` (`internal/domain`, определение базового домена под многоуровневыми суффиксами вроде `.co.uk`).
- Внешние зависимости в `go.mod` не запрещены. Добавляйте их, когда это реально лучше stdlib.
- Песочница разработки не имеет доступа ни к `proxy.golang.org`, ни к `golang.org` напрямую. `go get golang.org/x/net` не резолвит vanity-import-путь даже с `GOPROXY=direct`. `github.com` доступен, `github.com/golang/net` зеркалирует тот же код: `go.mod` тянет `golang.org/x/net` через `replace golang.org/x/net vX.Y.Z => github.com/golang/net vX.Y.Z`. Команды для обновления версии: `go get github.com/golang/net@vX.Y.Z`, затем `go mod edit -replace=golang.org/x/net@vX.Y.Z=github.com/golang/net@vX.Y.Z`, затем `GOPROXY=direct GOSUMDB=off go mod download golang.org/x/net`. `replace` работает одинаково при полном доступе к сети, CI и другие разработчики не замечают разницы.
- Песочница не имеет реального Docker/systemd/ufw и публичного домена. Эти пути проверяет CI (`.github/workflows/ci.yml`, integration-джобы `preflight-integration`/`oscheck-integration`) на настоящей Ubuntu VM GitHub Actions, не локальная разработка.
- Оригинальный bash-проект: `github.com/eGamesAPI/remnawave-reverse-proxy`, файлы читаются напрямую с `raw.githubusercontent.com` при необходимости сверки (в песочнице доступен).

---

## 3. Структура папок

```
cmd/remnawave/          точка входа: логирование, выбор языка, проверка ОС и root, главное меню
internal/
  addnode/               добавление ноды к панели
  api/                    HTTP-клиент Remnawave API
  backuprestore/          загрузка и запуск стороннего backup-restore
  caddynode/              установка ноды за Caddy
  caddypanelfull/         установка панели с совмещённой нодой за Caddy
  caddypanelonly/         установка панели без ноды за Caddy
  certs/                  выпуск и обновление TLS-сертификатов для Nginx
  domain/                 извлечение базового домена, проверка DNS
  genutil/                пароли, логины, секреты
  i18n/                   локализация EN/RU, выбор и сохранение языка
  ipv6/                   включение и отключение IPv6
  managepanel/            управление установленной панелью и нодой
  menu/                   главное меню
  nginxnode/              установка ноды за Nginx
  oscheck/                проверка версии ОС и прав root
  panelfull/              установка панели с совмещённой нодой за Nginx
  panelonly/              установка панели без ноды за Nginx
  preflight/              установка docker, certbot, ufw перед первым запуском
  reinstall/              переустановка панели или ноды поверх текущей
  selfsteal/              случайный или выбранный HTML-шаблон для selfsteal-домена
  uninstall/              удаление инструмента и установленной панели
  ui/                     цвета терминала, чтение ввода, логирование в файл
```

---

## 4. Контракты (заморожены)

Трогать только осознанно, с пониманием последствий:

- **Формат ключей i18n.** `internal/i18n/i18n.go` сгенерирован из `src/lang/en.sh`/`ru.sh` оригинала. Не переписывайте вручную целиком. Точечную правку конкретной строки фиксируйте комментарием над данными. Синтетические строки без аналога в оригинале (`IN_DEVELOPMENT`, `AvailableTemplates`, `DownloadingBackupRestore`) живут в отдельных мини-картах внизу файла с пометкой об этом.
- **Комментарии `Original bash (file:N-M): funcName()`.** Обязательны над каждой портированной функцией. Позволяют свериться с оригиналом построчно. Если Go-версия ведёт себя иначе, добавляйте отдельный комментарий `BUG FIX (not a 1:1 port): ...` с объяснением, что и почему изменено.
- **Структура `docker-compose.yml`/`nginx.conf`/`Caddyfile`.** Внешний контракт с образами `remnawave/backend`, `remnawave/node`, `remnawave/subscription-page`, `nginx`, `caddy`. Копируется из оригинала как есть: переменные окружения, порты, healthcheck. Не изобретайте имена переменных, они должны совпадать с тем, что ждут эти образы. При смене мажорной версии панели сверяйтесь с реальным changelog панели (пример: v1.1.1), не предполагайте.
- **Именование Go-пакетов по содержимому, не по имени файла оригинала.** У Nginx имена файлов не совпадают с содержимым: `install_panel_node.sh` = только панель, `install_panel.sh` = панель+нода. У Caddy наоборот, имена совпадают с содержимым. Обнаружено только чтением обоих файлов в v1.1.4, не по аналогии с Nginx. Из этого следует общее правило: путаница (если она есть) не обязана повторяться между разными вебсерверами или разными версиями апстрима. Каждый раз сверяйтесь с содержимым файла, а не с предыдущим выводом по аналогичному файлу.
- **`internal/preflight.EnsureInstalled()`.** Точка входа для установки зависимостей (docker/certbot/ufw/cron/unattended-upgrades/BBR). Все install-флоу (`nginxnode`, `panelonly`, `panelfull`, `caddynode`, `caddypanelonly`, `caddypanelfull`) вызывают её первой, до первого интерактивного вопроса пользователю.
- **`ui.Exit()`: единственная точка вызова `os.Exit`.** После `ui.EnableFileLogging()` голый `os.Exit()` теряет буфер pipe. Новые прямые вызовы `os.Exit()` не добавлять, только `ui.Exit()`.

---

## 5. Статус портирования

### Готово

| Модуль | Оригинал (bash) | Пакет |
|---|---|---|
| Проверка ОС и root | `check_os`, `check_root` | `internal/oscheck` |
| IPv6 | `src/modules/ipv6.sh` | `internal/ipv6` |
| API панели | `src/api/remnawave_api.sh` | `internal/api` |
| Добавление ноды | `src/modules/add_node.sh` | `internal/addnode` |
| Шаблоны selfsteal | `src/modules/selfsteal_templates.sh` | `internal/selfsteal` |
| Домен-утилиты | части `install_remnawave.sh` | `internal/domain` |
| Сертификаты | `check_certificates`, `get_certificates`, `handle_certificates` и меню | `internal/certs` |
| Установка ноды за Nginx | `src/nginx/install_node.sh` | `internal/nginxnode` |
| Установка панели без ноды (Nginx) | `src/nginx/install_panel_node.sh` | `internal/panelonly` |
| Установка панели с нодой (Nginx) | `src/nginx/install_panel.sh` | `internal/panelfull` |
| Установка ноды за Caddy | `src/caddy/install_node.sh` | `internal/caddynode` |
| Установка панели без ноды (Caddy) | `src/caddy/install_panel.sh` | `internal/caddypanelonly` |
| Установка панели с нодой (Caddy) | `src/caddy/install_panel_node.sh` | `internal/caddypanelfull` |
| Управление панелью и нодой (Nginx и Caddy) | `src/modules/manage_panel.sh` | `internal/managepanel` |
| Переустановка (Nginx и Caddy) | `choose_reinstall_type` | `internal/reinstall` |
| Установка зависимостей | `install_packages` | `internal/preflight` |
| Удаление | `remove_script` | `internal/uninstall` |
| Backup и restore | делегирует `distillium/remnawave-backup-restore` (MIT), как оригинал | `internal/backuprestore` |
| Локализация, выбор языка, логирование в файл | `src/lang/*.sh`, `log_entry` | `internal/i18n`, `internal/ui` |
| Главное меню | `install_remnawave.sh` | `internal/menu` |
| CI | нет прямого аналога | `.github/workflows/ci.yml` |

### Не реализовано

- **WARP Native** (`src/modules/warp.sh`, около 300 строк). Не начато.
- **Автообновление скрипта** (`update_remnawave_reverse`). Оригинал скачивает новую версию bash-файла и заменяет себя. Для бинарника это заработает после появления GitHub Releases с готовыми сборками, которых пока нет.
- **Механизм самоустановки** (`install_script_if_missing`). Нет `install.sh`, только сборка из исходников. Логически связано с автообновлением: сначала self-install, потом autoupdate может что-то реально обновлять.
- **Custom extensions by legiz** не заглушка, убрано из меню по явной просьбе: расширение сторонних тем, не часть Remnawave.

---

## 6. Правила письма

Активный залог, конкретика вместо общих фраз, без вводных оборотов, без длинного тире, без наречий-усилителей. Комментарий объясняет причину решения, не пересказывает соседнюю строку. Закомментированный код не оставлять. Полные правила с примерами: `CONTRIBUTING.md`.

---

## 7. Definition of Done для одной единицы работы

1. `go build ./...`: чисто.
2. `go vet ./...`: чисто.
3. `gofmt -l .`: пустой вывод.
4. `go test ./...`: чисто. Постоянные тесты живут в `_test.go`. Одноразовые проверочные тесты пишутся, гоняются, потом удаляются, не коммитятся.
5. Если менялось поведение, видимое пользователю, обновить `README.md`.
6. Версия в `internal/menu/version.go` увеличена.
7. Этот файл обновлён: новый пункт в changelog шапки, изменения в разделах 4 и 5, если контракт или статус портирования сдвинулись.
8. Закоммичено с подробным сообщением: что изменилось и почему, не только что.
9. Запушено в `dev`, пуш проверен независимым `git clone` + `go build`. Локальному состоянию после пуша не доверять, git остаётся источником истины.

---

## 8. STATUS GAP

Нужного факта о проекте здесь нет: непонятно, что на самом деле делает файл оригинала, как называется контракт, какой пакет уже покрывает нужную функциональность. Работа останавливается до проверки, предположения по аналогии с похожим модулем не подставляются.

Порядок действий:

1. Скачать и прочитать реальный файл оригинала (`raw.githubusercontent.com/eGamesAPI/remnawave-reverse-proxy/main/...`), не полагаться на память или на то, как устроен похожий файл для другого вебсервера или другой версии.
2. Если после чтения факт подтверждён, зафиксировать его в разделе 4 или 5 этого файла в том же коммите, где он использован.
3. Если факт противоречит уже записанному здесь (как в v1.1.4 с именами Caddy-файлов), явно исправить старую запись, не оставлять обе версии рядом.

---

## 9. Правила поведения в сессии

- Проверять реальный файл оригинала перед портированием, не предполагать по аналогии с уже портированным похожим модулем (раздел 8).
- Хирургические правки: соседний рабочий код не улучшать и не рефакторить без причины, связанной с текущей задачей.
- Баги оригинала не переносить по умолчанию. При сомнении, воспроизводить баг или чинить, чинить и документировать решение отдельным комментарием `BUG FIX (not a 1:1 port)`.
- Одна единица работы за один пуш. Definition of Done (раздел 7) выполняется перед каждым пушем, не в конце большой серии правок.
- Каждый пуш проверяется независимым клоном, локальному состоянию не доверять.

---

## 10. Ревью после единицы работы

Отдельным проходом, после того как код написан и Definition of Done зелёный:

1. Каждая портированная функция несёт `Original bash (file:N-M): funcName()`.
2. Отличия от оригинала помечены `BUG FIX (not a 1:1 port)` с объяснением причины.
3. Мёртвого кода нет: неиспользуемые функции, закомментированные блоки, заглушки без пометки `🚧`.
4. Имена переменных окружения в `docker-compose.yml`/`.env`/`nginx.conf`/`Caddyfile` совпадают с тем, что ждут образы `remnawave/*` (раздел 4).
5. Раздел 5 отражает реальный статус: ничего не отмечено готовым, если не собрано и не проверено.
