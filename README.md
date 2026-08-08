# Remnawave Easy-Install

![Go version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/license-GPLv3-blue)
![Status](https://img.shields.io/badge/status-active%20development-yellow)
![CI](https://github.com/FlexEbat/Remnwave-Easy-Install/actions/workflows/ci.yml/badge.svg?branch=dev)

Go-инструмент для установки и администрирования [Remnawave](https://remna.st) на Debian и Ubuntu: панель, ноды, TLS-сертификаты, IPv6, шаблоны для selfsteal-домена.

Это форк [`remnawave-reverse-proxy`](https://github.com/eGamesAPI/remnawave-reverse-proxy) от eGames. Оригинал написан на bash (~7300 строк в install_remnawave.sh и модулях). Этот форк переносит ту же функциональность на Go, файл за файлом, с построчными комментариями о происхождении каждой функции.

**Статус:** активная разработка. Меню показывает `🚧` рядом с непортированными пунктами вместо того, чтобы делать вид, что они работают. Список ниже точно называет, что реализовано, а что нет.

## Содержание

- [Что делает инструмент](#что-делает-инструмент)
- [Статус портирования](#статус-портирования)
- [Требования](#требования)
- [Установка](#установка)
- [Использование](#использование)
- [Архитектура](#архитектура)
- [Отличия от оригинала](#отличия-от-оригинала)
- [CI/CD](#cicd)
- [Разработка](#разработка)
- [Лицензия](#лицензия)

## Что делает инструмент

- Устанавливает панель Remnawave, ноду или обе части на одном сервере, за Nginx.
- Выпускает и продлевает TLS-сертификаты через `certbot`: Cloudflare DNS-01, ACME HTTP-01, Gcore DNS-01.
- Добавляет ноды к существующей панели через API.
- Ставит случайный HTML-шаблон на selfsteal-домен для маскировки.
- Управляет установленной панелью и нодой: старт, стоп, обновление образов, логи, CLI, временный доступ к панели на порту 8443.
- Включает и выключает IPv6.
- Ставит зависимости (`docker`, `certbot`, `ufw`, `cron`) на чистый сервер перед первой установкой.
- Удаляет установку целиком или только состояние инструмента.

Каждый пункт меню, который инструмент ещё не реализует, показывает `🚧 Not implemented yet` вместо того, чтобы падать или притворяться, что работает.

## Статус портирования

### Готово

| Модуль | Файл в оригинале | Пакет в Go |
|---|---|---|
| Проверка ОС и root | `check_os`, `check_root` | `internal/oscheck` |
| IPv6 | `src/modules/ipv6.sh` | `internal/ipv6` |
| API панели | `src/api/remnawave_api.sh` | `internal/api` |
| Добавление ноды | `src/modules/add_node.sh` | `internal/addnode` |
| Шаблоны selfsteal | `src/modules/selfsteal_templates.sh` | `internal/selfsteal` |
| Домен-утилиты | части `install_remnawave.sh` | `internal/domain` |
| Сертификаты | `check_certificates`, `get_certificates`, `handle_certificates`, `check_cert_expiry`, `fix_letsencrypt_structure` и меню сертификатов | `internal/certs` |
| Установка ноды за Nginx | `src/nginx/install_node.sh` | `internal/nginxnode` |
| Установка панели без ноды | `src/nginx/install_panel_node.sh`¹ | `internal/panelonly` |
| Установка панели с нодой | `src/nginx/install_panel.sh`¹ | `internal/panelfull` |
| Управление панелью и нодой | `src/modules/manage_panel.sh` | `internal/managepanel` |
| Переустановка панели или ноды | `choose_reinstall_type` | `internal/reinstall` |
| Установка зависимостей | `install_packages` | `internal/preflight` |
| Удаление | `remove_script` | `internal/uninstall` |
| Генераторы паролей и секретов | части `install_remnawave.sh` | `internal/genutil` |
| Локализация | `src/lang/en.sh`, `src/lang/ru.sh` | `internal/i18n` |
| Главное меню | `install_remnawave.sh` | `internal/menu` |

¹ Имена этих двух файлов в оригинале не соответствуют содержимому: `install_panel_node.sh` устанавливает панель без ноды, `install_panel.sh` устанавливает панель с совмещённой нодой. Go-пакеты названы по содержимому: `panelonly` и `panelfull`.

### Не реализовано

Меню показывает эти пункты с пометкой `🚧`:

- **Caddy.** Инструмент поддерживает только Nginx.
- **WARP Native** (`src/modules/warp.sh`).
- Backup и Restore.
- Автообновление скрипта (`update_remnawave_reverse`). В оригинале это скачивание новой версии bash-файла и замена себя. Для скомпилированного бинарника такая логика имеет смысл только после появления GitHub Releases с готовыми сборками. Релизов пока нет.

Пункт «Custom extensions by legiz» убран из меню полностью, не как заглушка: это расширение сторонних тем, не часть Remnawave.

## Требования

- Debian 11 или новее (11 bullseye, 12 bookworm, 13 trixie, включая точечные релизы вроде 13.6.0, и будущие версии).
- Ubuntu 22.04 LTS или новее (22.04 jammy, 24.04 noble, 26.04 resolute, и будущие релизы).
- Права root.

`internal/oscheck` читает `ID` и `VERSION_ID` из `/etc/os-release` и сравнивает числа, а не список кодовых имён. Новый релиз Debian или Ubuntu проходит проверку без изменений в коде.

### Зависимости на сервере

`docker`, `docker compose`, `certbot`, `ufw` не нужно ставить заранее. Перед первой установкой инструмент проверяет их наличие и ставит недостающее: пакеты через `apt-get` (`certbot`, `python3-certbot-dns-cloudflare`, `ufw`, `cron`, `unattended-upgrades` и другие) и Docker через `get.docker.com`. Настраивает `ufw` (порты 22 и 443), BBR, `unattended-upgrades`. Работает на только что созданном VPS без ручной подготовки.

Nginx запускается внутри Docker и не требует установки на хост.

### Зависимости Go-модуля

`go.mod` не содержит ни одной внешней зависимости. Весь код собирается из стандартной библиотеки Go, кроме одного пакета: `internal/publicsuffix`, копии `golang.org/x/net/publicsuffix` (см. `internal/publicsuffix/README.md` про причину и способ обновления). Это именно копия, а не `go.mod`-зависимость, потому что среда разработки не имела доступа к `proxy.golang.org` и код пришлось скопировать напрямую с зеркала на GitHub.

Если появится причина добавить настоящую зависимость через `go.mod`, добавляйте её без колебаний, когда она даёт реальное преимущество перед stdlib: меньше кода поддерживать самим, готовое и проверенное решение вместо велосипеда. Не обязательно ограничиваться stdlib только ради ограничения. Единственное условие: то, что нельзя проверить локально (поведение с реальным Docker, systemd, `apt`, сетью), должно быть покрыто интеграционными тестами в CI (см. [«CI/CD»](#cicd)). Отсутствие Docker в песочнице разработки не повод оставлять код непроверенным.

## Установка

Готовых бинарников пока нет. Собирайте из исходников:

```bash
git clone -b dev https://github.com/FlexEbat/Remnwave-Easy-Install.git
cd Remnwave-Easy-Install
go build -o remnawave-easy-install ./cmd/remnawave
sudo ./remnawave-easy-install
```

Нужен Go 1.22 или новее.

## Использование

Запустите бинарник от root. Инструмент показывает меню:

```
Remnawave Easy-Install (fork eGames)
Version X.X.X
Wiki: https://github.com/FlexEbat/Remnwave-Easy-Install

1. Install Remnawave Components
2. Reinstall panel/node
3. Manage Panel/Node
4. Install random template for node
5. WARP Native
6. Backup and Restore
7. Manage IPv6
8. Manage certificates domain
9. Check for updates script
10. Remove script
0. Exit
```

Пункт 1 открывает подменю с четырьмя вариантами установки: панель и нода на одном сервере, только панель, добавление ноды к существующей панели, только нода. Пункт 3 открывает управление уже установленной панелью или нодой: старт, стоп, обновление образов, логи, встроенный `remnawave` CLI, временный доступ к панели на порту 8443.

Номер пункта в меню не хардкожен: `internal/menu` собирает список из `mainMenuItems` и присваивает номера по порядку. Подсказка `Select action (0-N)` пересчитывает `N` от длины списка, поэтому не расходится с реальным числом пунктов при изменении меню.

## Архитектура

```
cmd/remnawave/          точка входа: логирование, выбор языка, проверки ОС и root, затем главное меню
internal/
  ├── addnode/           добавление ноды к панели
  ├── api/                HTTP-клиент Remnawave API
  ├── certs/              выпуск и обновление TLS-сертификатов
  ├── domain/             извлечение базового домена, проверка DNS
  ├── genutil/             пароли, логины, JWT-секреты
  ├── i18n/                локализация EN/RU, выбор и сохранение языка
  ├── ipv6/                включение и отключение IPv6
  ├── managepanel/         управление установленной панелью и нодой
  ├── menu/                главное меню
  ├── nginxnode/           установка ноды за Nginx
  ├── oscheck/             проверка версии ОС и прав root
  ├── panelfull/           установка панели с совмещённой нодой
  ├── panelonly/           установка панели без ноды
  ├── preflight/           установка docker, certbot, ufw перед первым запуском
  ├── publicsuffix/        Public Suffix List, вендор golang.org/x/net
  ├── reinstall/           переустановка панели или ноды поверх текущей
  ├── selfsteal/           случайный HTML-шаблон для selfsteal-домена
  ├── uninstall/           удаление инструмента и/или установленной панели
  └── ui/                  цвета терминала, чтение ввода, логирование в файл
```

Каждая функция, портированная из bash, несёт комментарий `Original bash (file:N-M): funcName()` с номерами строк оригинала. Комментарий позволяет свериться с исходным кодом при чтении Go-версии.

При первом запуске инструмент спрашивает язык (English/Русский) и сохраняет выбор в `/usr/local/remnawave_reverse/selected_language`. Повторные запуски читают файл и не спрашивают снова. Весь вывод программы, включая вывод дочерних процессов вроде `docker` и `certbot`, одновременно идёт на экран и пишется в `/usr/local/remnawave_reverse/remnawave_reverse.log`.

## Отличия от оригинала

Инструмент не копирует bash построчно там, где Go-стандартная библиотека даёт более надёжный результат.

**Внешние утилиты заменены библиотеками Go:**

| Было (bash) | Стало (Go) | Пакет |
|---|---|---|
| `curl` + `jq` | `net/http` + `encoding/json` | `api` |
| `wget` + `unzip` | `net/http` + `archive/zip` в памяти, без временных файлов | `selfsteal` |
| `dig`, `curl ifconfig.me`, битовая арифметика для Cloudflare CIDR | `net.LookupIP`, `net/http`, `net.ParseCIDR` | `domain` |
| `openssl x509 -enddate` + `date -d` | `crypto/x509` | `certs` |
| `openssl rand -hex`, `/dev/urandom` + `tr` + `fold` + `shuf` | `crypto/rand` | `api`, `selfsteal`, `genutil` |
| `curl get.docker.com -o /tmp/get-docker.sh && sh /tmp/get-docker.sh` | скрипт идёт из `net/http` прямо в stdin `sh`, файл на диск не пишется | `preflight` |

`certbot` остаётся внешней командой: переписывать реализацию ACME на Go не имеет смысла при наличии проверенного инструмента.

**Другие решения:**

- Проверка версии ОС сравнивает числа `VERSION_ID`, не список кодовых имён. Раздел [«Требования»](#требования) объясняет, зачем.
- Словарь переводов (`internal/i18n`) сгенерирован из `src/lang/en.sh` и `src/lang/ru.sh` оригинального проекта скриптом, а не перепечатан руками. Это исключает опечатки при переносе ~400 строк текста.
- Пакеты `panelonly` и `panelfull` названы по содержимому, не по именам файлов оригинала (см. сноску в разделе [«Статус портирования»](#статус-портирования)).
- `handle_certificates()` в оригинале использует захардкоженный путь `/opt/remnawave` для любого вызова, включая установку отдельной ноды, которая физически лежит в `/opt/remnanode`. Go-версия принимает путь параметром.
- Главное меню использует слайс `[]menuItem` вместо последовательности блоков `case` с ручной нумерацией.

## CI/CD

Часть логики трогает вещи, которых нет в среде разработки: реальный Docker-демон, `systemd`, `apt`, `ufw`. GitHub Actions (`.github/workflows/ci.yml`) закрывает эту проверку на настоящей одноразовой Ubuntu-машине при каждом пуше и PR в `main`/`dev`:

- **`build-and-test`**: `go build`, `go vet`, `gofmt -l .`, юнит-тесты. Быстрый обязательный чек.
- **`preflight-integration`**: ставит зависимости через `internal/preflight.InstallPackages()` на чистом раннере, проверяет, что `docker info` и `certbot --version` работают после установки.
- **`oscheck-integration`**: прогоняет `internal/oscheck.CheckOS()` против настоящего `/etc/os-release` раннера, а не синтетического файла из юнит-теста.

Интеграционные тесты лежат в файлах с тегом сборки `integration` (`internal/preflight/integration_test.go`, `internal/oscheck/integration_test.go`) и не участвуют в обычном `go test ./...`: они меняют состояние системы (ставят пакеты, правят `sysctl.conf`, открывают порты в `ufw`), поэтому их нельзя случайно запустить на рабочей машине. Явный запуск:

```bash
go test -tags=integration ./internal/preflight/...
```

## Разработка

Правила стиля для коммитов, комментариев и документации в [CONTRIBUTING.md](CONTRIBUTING.md).

Перед коммитом:

```bash
go build ./...
go vet ./...
gofmt -l .
```

Пустой вывод `gofmt -l .` означает, что форматирование в порядке.

## Лицензия

[GNU GPLv3](LICENSE), как и оригинальный проект.
