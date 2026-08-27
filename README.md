# Remnawave Easy-Install

Форк [remnawave-reverse-proxy](https://github.com/eGamesAPI/remnawave-reverse-proxy) от eGames.

![Go version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/license-GPLv3-blue)
![CI](https://github.com/FlexEbat/Remnwave-Easy-Install/actions/workflows/ci.yml/badge.svg?branch=dev)

## Оглавление

- [Что это](#что-это)
- [Возможности](#возможности)
- [Статус портирования](#статус-портирования)
- [Требования](#требования)
- [Установка](#установка)
- [Настройка](#настройка)
- [Использование](#использование)
- [Структура проекта](#структура-проекта)
- [Тестирование и CI](#тестирование-и-ci)
- [Разработка](#разработка)
- [Лицензия](#лицензия)

## Что это

Remnawave Easy-Install ставит и обслуживает [Remnawave](https://remna.st) на сервере: панель, ноду или обе части сразу, за Nginx или за Caddy. Инструмент выпускает TLS-сертификаты, добавляет ноды к уже работающей панели, управляет запущенным стеком и удаляет установку. Всё через интерактивное текстовое меню, без правки конфигов вручную.

Оригинальный проект написан на bash (около 7300 строк). Этот форк переносит ту же функциональность на Go, файл за файлом, и продолжает достраивать то, что оригинал ещё не покрывает в Go-версии. Каждая портированная функция несёт комментарий с номерами строк исходника, поэтому поведение можно свериться с оригиналом напрямую.

## Возможности

- Устанавливает панель Remnawave, ноду или обе части на одном сервере.
- Работает с двумя вебсерверами на выбор: Nginx или Caddy.
- Для Nginx выпускает и продлевает TLS через `certbot`: Cloudflare DNS-01, ACME HTTP-01, Gcore DNS-01. Caddy выпускает и продлевает сертификаты сам через встроенный ACME-клиент.
- Добавляет ноды к существующей панели через API Remnawave.
- Ставит на selfsteal-домен случайный HTML-шаблон для маскировки трафика.
- Управляет установленным стеком: старт, стоп, обновление образов, просмотр логов, встроенный `remnawave` CLI, временный доступ к панели на порту 8443.
- Переустанавливает панель или ноду поверх текущей установки.
- Включает и выключает IPv6 на сервере.
- Ставит зависимости (`docker`, `certbot`, `ufw`, `cron`) на чистый сервер перед первой установкой.
- Делает backup и restore панели через сторонний `distillium/remnawave-backup-restore`.
- Удаляет установку целиком или только состояние самого инструмента.
- Переключает язык интерфейса между английским и русским.

Пункт меню, который инструмент ещё не реализует, показывает `🚧 Not implemented yet` вместо того, чтобы завершиться с ошибкой или притвориться, что выполнил действие.

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
| Установка панели без ноды (Nginx) | `src/nginx/install_panel_node.sh`¹ | `internal/panelonly` |
| Установка панели с нодой (Nginx) | `src/nginx/install_panel.sh`¹ | `internal/panelfull` |
| Установка ноды за Caddy | `src/caddy/install_node.sh` | `internal/caddynode` |
| Установка панели без ноды (Caddy) | `src/caddy/install_panel.sh` | `internal/caddypanelonly` |
| Установка панели с нодой (Caddy) | `src/caddy/install_panel_node.sh` | `internal/caddypanelfull` |
| Управление панелью и нодой (Nginx и Caddy) | `src/modules/manage_panel.sh` | `internal/managepanel` |
| Переустановка панели или ноды (Nginx и Caddy) | `choose_reinstall_type` | `internal/reinstall` |
| Установка зависимостей | `install_packages` | `internal/preflight` |
| Удаление | `remove_script` | `internal/uninstall` |
| Backup и restore | делегирует стороннему `distillium/remnawave-backup-restore` (MIT), как и оригинал | `internal/backuprestore` |
| Генераторы паролей и секретов | части `install_remnawave.sh` | `internal/genutil` |
| Локализация | `src/lang/en.sh`, `src/lang/ru.sh` | `internal/i18n` |
| Главное меню | `install_remnawave.sh` | `internal/menu` |

¹ Имена этих двух файлов в оригинале не совпадают с содержимым: `install_panel_node.sh` устанавливает панель без ноды, `install_panel.sh` устанавливает панель с совмещённой нодой. Go-пакеты названы по содержимому: `panelonly` и `panelfull`. Для Caddy файлы называются так, как выглядит логичным (`install_panel.sh` без ноды, `install_panel_node.sh` с нодой), но Go-пакеты всё равно называются по содержимому (`caddypanelonly`/`caddypanelfull`), чтобы схема именования не зависела от того, насколько точно назвал файлы конкретный апстрим.

### Не реализовано

Меню показывает эти пункты с пометкой `🚧`:

- **WARP Native** (`src/modules/warp.sh`).
- Автообновление скрипта (`update_remnawave_reverse`). Оригинал скачивает новую версию bash-файла и заменяет себя. Для скомпилированного бинарника такая логика заработает после появления GitHub Releases с готовыми сборками, которых пока нет.

Пункт «Custom extensions by legiz» убран из меню полностью, не заглушкой: это расширение сторонних тем, не часть Remnawave.

## Требования

- Debian 11 или новее.
- Ubuntu 22.04 LTS или новее.
- Права root.

`internal/oscheck` читает `ID` и `VERSION_ID` из `/etc/os-release` и сравнивает числа версий, а не список кодовых имён. Новый релиз Debian или Ubuntu проходит проверку без изменений в коде.

Устанавливать `docker`, `docker compose`, `certbot` и `ufw` заранее не нужно. Перед первой установкой инструмент сам ставит недостающее: пакеты через `apt-get`, Docker через `get.docker.com`. Настраивает `ufw` (порты 22 и 443), BBR и `unattended-upgrades`. Работает на только что созданном VPS.

## Установка

Готовых бинарников пока нет, соберите инструмент из исходников:

```bash
git clone -b dev https://github.com/FlexEbat/Remnwave-Easy-Install.git
cd Remnwave-Easy-Install
go build -o remnawave-easy-install ./cmd/remnawave
```

Нужен Go 1.22 или новее. Внешних зависимостей в `go.mod` нет: весь код собирается из стандартной библиотеки Go, кроме одного скопированного пакета `internal/publicsuffix` (копия `golang.org/x/net/publicsuffix`).

## Настройка

При первом запуске инструмент спрашивает язык интерфейса (English или Русский) и сохраняет выбор в `/usr/local/remnawave_reverse/selected_language`. Повторные запуски читают файл и не спрашивают снова.

Отдельных переменных окружения или конфигурационных файлов инструмент не требует: все параметры установки (домены, IP панели, выбор вебсервера) он спрашивает интерактивно в процессе работы.

## Использование

Запустите бинарник от root:

```bash
sudo ./remnawave-easy-install
```

Инструмент покажет главное меню:

```
Remnawave Easy-Install (fork eGames)
Version X.X.X
Wiki: https://github.com/FlexEbat/Remnwave-Easy-Install

1. Install Remnawave Components
2. Reinstall panel/node
3. Manage Panel/Node

4. Install random template for selfsteal node

5. WARP Native
6. Backup and Restore

7. Manage IPv6
8. Manage certificates domain

9. Check for updates script
10. Remove script

0. Exit
```

Пункт 1 открывает подменю с четырьмя вариантами установки: панель и нода на одном сервере, только панель, добавление ноды к существующей панели, только нода. Каждый из них дальше спрашивает выбор вебсервера, Nginx или Caddy.

Пункт 3 открывает управление уже установленной панелью или нодой: старт, стоп, обновление образов, просмотр логов, встроенный `remnawave` CLI, временный доступ к панели на порту 8443.

Весь вывод программы, включая вывод дочерних процессов вроде `docker` и `certbot`, одновременно идёт на экран и пишется в `/usr/local/remnawave_reverse/remnawave_reverse.log`.

## Структура проекта

```text
cmd/remnawave/          точка входа: логирование, выбор языка, проверка ОС и root, главное меню
internal/
  ├── addnode/           добавление ноды к панели
  ├── api/                HTTP-клиент Remnawave API
  ├── backuprestore/       загрузка и запуск стороннего backup-restore
  ├── caddynode/           установка ноды за Caddy
  ├── caddypanelfull/      установка панели с совмещённой нодой за Caddy
  ├── caddypanelonly/      установка панели без ноды за Caddy
  ├── certs/              выпуск и обновление TLS-сертификатов для Nginx
  ├── domain/             извлечение базового домена, проверка DNS
  ├── genutil/             пароли, логины, секреты
  ├── i18n/                локализация EN/RU, выбор и сохранение языка
  ├── ipv6/                включение и отключение IPv6
  ├── managepanel/         управление установленной панелью и нодой
  ├── menu/                главное меню
  ├── nginxnode/           установка ноды за Nginx
  ├── oscheck/             проверка версии ОС и прав root
  ├── panelfull/           установка панели с совмещённой нодой за Nginx
  ├── panelonly/           установка панели без ноды за Nginx
  ├── preflight/           установка docker, certbot, ufw перед первым запуском
  ├── publicsuffix/        Public Suffix List, копия golang.org/x/net
  ├── reinstall/           переустановка панели или ноды поверх текущей
  ├── selfsteal/           случайный HTML-шаблон для selfsteal-домена
  ├── uninstall/           удаление инструмента и установленной панели
  └── ui/                  цвета терминала, чтение ввода, логирование в файл
```

Функция, портированная из bash, несёт комментарий `Original bash (file:N-M): funcName()` с номерами строк оригинала.

## Тестирование и CI

```bash
go test ./...
```

Часть логики трогает то, чего нет в среде разработки: реальный Docker-демон, `systemd`, `apt`, `ufw`. GitHub Actions (`.github/workflows/ci.yml`) проверяет эту часть на настоящей одноразовой Ubuntu-машине при каждом пуше и PR в `main` и `dev`:

- `build-and-test`: `go build`, `go vet`, `gofmt -l .`, юнит-тесты.
- `preflight-integration`: ставит зависимости через `internal/preflight.InstallPackages()` на чистом раннере, проверяет, что `docker info` и `certbot --version` работают после установки.
- `oscheck-integration`: прогоняет `internal/oscheck.CheckOS()` против настоящего `/etc/os-release` раннера.

Интеграционные тесты лежат в файлах с тегом сборки `integration` и не входят в обычный `go test ./...`: они меняют состояние системы (ставят пакеты, правят `sysctl.conf`, открывают порты в `ufw`). Запустить их вручную:

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
