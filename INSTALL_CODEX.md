# Установка на другом компьютере

Это локальный MCP-сервер с веб-панелью. Публикация на npm или отдельный OpenAI API key не требуются. Установка собирает бинарник под компьютер пользователя; готовый macOS-бинарник не переносится на Linux.

## Требования

- macOS или Linux; на Windows используйте WSL2 (не проверено).
- Git, Go 1.26.5+, Node.js 22.12+ и npm, make.
- C-компилятор и SQLite с заголовками: на macOS Command Line Tools, на Debian/Ubuntu `build-essential libsqlite3-dev`.
- Codex CLI, установленный по [официальной инструкции](https://developers.openai.com/codex/cli), и вход через `codex login` для ассистента внутри панели.

## Установка

```bash
git clone https://github.com/mikail-tommard/ai-workspace.git
cd ai-workspace
bash scripts/install.sh
```

Скрипт выполняет `npm ci`, `make build` и поддерживаемую команду:

```bash
codex mcp add ai-workspace -- sh /absolute/path/ai-workspace/scripts/run-mcp.sh
```

После установки заново откройте Codex. Он сам запускает MCP и панель: **http://localhost:8080**. Не запускайте параллельно `./ai-workspace` на том же порту. Папка checkout должна оставаться на месте; при переносе повторите установку.

Проверка: `codex mcp get ai-workspace`, затем попросите Codex: «Создай математическую задачу средней сложности и покажи на панели». Должна появиться интерактивная карточка. MCP-инструменты не требуют API-ключа или авторизации OpenAI сами по себе; встроенный в панель ассистент обращается к Codex CLI с авторизацией пользователя.

Проверка наличия программ без сборки и изменения конфигурации: `bash scripts/install.sh --check`. Только сборка без подключения Codex: `bash scripts/install.sh --no-register`.

## Данные и настройки

Launcher использует стабильный каталог вне checkout:

- macOS: `~/Library/Application Support/ai-workspace`.
- Linux: `${XDG_DATA_HOME:-~/.local/share}/ai-workspace`.

Там хранятся SQLite, медиа и сгенерированный токен. Они не публикуются в Git. Для переноса состояния остановите старый сервер, скопируйте **весь** каталог данных на новый компьютер и укажите его при регистрации:

```bash
codex mcp add ai-workspace \
  --env WORKSPACE_DATA_DIR=/absolute/path/to/workspace-data \
  --env WORKSPACE_ADDR=127.0.0.1:8081 \
  -- sh /absolute/path/ai-workspace/scripts/run-mcp.sh
```

Для дополнительной папки локальных изображений аналогично передайте `--env WORKSPACE_MEDIA_IMPORT_ROOTS=/absolute/path/to/photos`. Домашний каталог, путь к checkout и секреты предыдущего компьютера не зашиты в исходники.

Микрофон запускается по кнопке в браузере; качество и доступность распознавания зависят от браузера. На macOS доступны системные уведомления. На Linux панель и MCP работают, но нативные macOS-уведомления недоступны. Обычные модели Codex и браузерное распознавание могут использовать сеть.

## Обновление

Остановите MCP в Codex/закройте Codex, выполните `git pull --ff-only` и `bash scripts/install.sh`, затем откройте Codex снова. Пользовательские данные остаются вне checkout.

## Задание Codex на новом компьютере

> Клонируй https://github.com/mikail-tommard/ai-workspace.git, прочитай INSTALL_CODEX.md. Проверь зависимости и запусти bash scripts/install.sh. Подключи ai-workspace к моему Codex через STDIO, не удаляя другие MCP-настройки. Не копируй чужие токены или данные. После подключения проверь создание математической карточки на панели. Если для загрузки новой конфигурации нужен перезапуск Codex, явно сообщи об этом.

Формат регистрации проверен локальной справкой `codex mcp add --help` и [официальной документацией MCP](https://developers.openai.com/codex/mcp). Установщик не ставит системные зависимости и не меняет авторизацию пользователя.
