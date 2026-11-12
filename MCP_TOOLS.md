# MCP-инструменты

Все инструменты возвращают MCP `CallToolResult` с сериализованным JSON в `content[0].text`, объектом `structuredContent` и `isError`. Неизвестный инструмент — JSON-RPC `-32602`. Ошибка в параметрах или выполнении известного инструмента — `isError:true`, `structuredContent.error`. Изменение сохраняется до возврата успешного результата.

Корневые схемы запрещают дополнительные поля. Создание/обновление config использует `anyOf` закрытых схем, затем Registry проверяет соответствие **конкретному** типу виджета. `null`, неизвестные типы и поля, неверные размеры и диапазоны отклоняются. Схемы публикуются в `tools/list`; тот же каталог доступен авторизованному браузеру в `/api/tools`.

## Общие параметры и результаты

- `workspace_id?: string` — существующий ID; по умолчанию `default`. Не передаётся в `workspace_create` и `workspace_list`.
- `widget_id: string` — ID существующего виджета данного пространства.
- `title: string` — 1–200 символов, если не оговорено иначе.
- `tab_id?: string` — ID существующей вкладки; при создании по умолчанию активная.
- Создание/мутация возвращают `workspace_id`, `version`, публичный `state`; создание/обновление карточки — также `widget`. Удаление workspace возвращает только ID/версию.
- Чтение не увеличивает версию и не создаёт события. Каждая успешная мутация увеличивает версию ровно один раз и сохраняет событие.

Не подставляйте названия вместо ID: сначала прочитайте workspace или список виджетов. Два запроса создания не являются идемпотентными. При потере ответа сначала перечитайте состояние.

## Рабочие пространства

| Инструмент | Параметры | Поведение / результат |
|---|---|---|
| `workspace_create` | **name** (1–100) | Новое пространство, вкладка main, пустой список карточек. ID создаётся сервером. |
| `workspace_list` | `{}` | `workspaces:[{id,name,version,widget_count}]`. |
| `workspace_get` | workspace_id? | `state` со вкладками, настройками, виджетами и версией. |
| `workspace_get_state` | workspace_id? | То же полное текущее состояние для синхронизации AI. |
| `workspace_clear` | workspace_id? | Удаляет активные карточки, сохраняет вкладки и настройки. |
| `workspace_delete` | workspace_id? | Удаляет активное пространство; default удалить нельзя. Недавний audit trail сохраняется. |

Максимум 50 пространств. Браузерные подписки разделены по ID; доступ принадлежит одному владельцу локальной установки.

## Виджеты

| Инструмент | Параметры | Поведение / результат |
|---|---|---|
| `widget_create` | **type, title, config**, workspace_id?, tab_id? | type: markdown/code/chart/table/notification/image/gallery/video/audio/file/checklist/metric/progress. Математика и quiz создаются специальными math-инструментами. |
| `widget_update` | **widget_id**, title?, config?, math?, workspace_id? | Нужно хотя бы одно изменяемое поле. Config заменяется полностью. Для math_problem используйте math={kind?,difficulty?}: сервер регенерирует задачу в той же карточке и сбрасывает попытки/решение. Сырой математический config менять нельзя. |
| `widget_delete` | **widget_id**, workspace_id? | Удаляет карточку. |
| `widget_list` | workspace_id? | `widgets:[Widget]`, включая ID, tab_id и position. |
| `widget_get` | **widget_id**, workspace_id? | `widget` с безопасным публичным содержимым. |
| `widget_move` | **widget_id, position**, tab_id?, workspace_id? | Порядок отображения и, при необходимости, вкладка. Position: целое 0–199. |

Максимум 200 виджетов, workspace целиком не более 8 MiB.

### Конфигурации

```json
{"type":"markdown","config":{"content":"# Заголовок\n**Текст**"}}
```

```json
{"type":"code","config":{"language":"go","code":"package main"}}
```

Языки: `go`, `javascript`, `typescript`, `python`, `json`, `bash`, `css`, `html`, `text`. Код никогда не выполняется. Текст/code — до 100000 символов.

```json
{"type":"table","config":{"columns":["Имя","Балл"],"rows":[["Анна","10"],["Борис","8"]]}}
```

1–20 колонок, 0–500 строк; ячейки — строки до 2000 символов. Длина каждой строки совпадает с числом колонок. Названия колонок до 120 символов.

```json
{"type":"notification","config":{"message":"Данные готовы","level":"success"}}
```

Уровни: `info`, `success`, `warning`, `error`; сообщение до 10000 символов. Конфигурация chart описана ниже.

## Математика

| Инструмент | Параметры | Поведение / результат |
|---|---|---|
| `math_create_problem` | kind?, difficulty?, title?, tab_id?, workspace_id? | Генерирует интерактивную задачу с hint и закрытым решением. По умолчанию medium quadratic. |
| `math_check_answer` | **widget_id, answer**, question_index?, workspace_id? | Точная рациональная проверка, сохранение попытки, `correct:bool` и обновлённый state. |
| `math_show_solution` | **widget_id**, question_index?, workspace_id? | Раскрывает/сохраняет проверенные шаги, возвращает `steps`. |
| `math_generate_quiz` | count?, kind?, difficulty?, title?, tab_id?, workspace_id? | 1–20 интерактивных задач, по умолчанию 5. Для проверок/решений обязателен question_index (с нуля). |

`kind`: `arithmetic`, `linear`, `quadratic`, `system`, `percent`, `fraction`.

`difficulty`: `easy`, `medium`, `hard` (разный диапазон генерируемых чисел).

```json
{"name":"math_create_problem","arguments":{"kind":"quadratic","difficulty":"medium","title":"Реши уравнение"}}
```

```json
{"name":"math_check_answer","arguments":{"widget_id":"ID_FROM_RESULT","answer":"3; 2"}}
```

При ожидаемых корнях 2 и 3 порядок не важен. Допустимы `x=3; x=2` и `3,2`. Одиночный десятичный ответ принимает точку или запятую: `0.5`, `0,5`. Дроби `1/2` и `2/4` равны точно. Для системы: `x=2; y=3`, `y=3; x=2` или `2;3`; порядок неназванных координат значим. Нулевой знаменатель, NaN, экспоненциальная запись и выражения с вычислениями отклоняются.

Ожидаемые ответы хранятся приватно и не передаются браузеру. До вызова `math_show_solution` публичные steps отсутствуют. Число попыток ограничено 100 на вопрос. Hint доступен карточке сразу; его раскрытие — локальное взаимодействие интерфейса.

## Графики

| Инструмент | Параметры | Поведение |
|---|---|---|
| `chart_create` | **title, config**, tab_id?, workspace_id? | Создаёт chart-виджет. |
| `chart_update` | **widget_id, config**, title?, workspace_id? | Полностью заменяет данные существующего графика. |
| `chart_delete` | **widget_id**, workspace_id? | Удаляет только chart, отклоняет другие типы. |

```json
{
  "kind":"line",
  "points":[{"x":-1,"y":1},{"x":0,"y":0},{"x":1,"y":1}],
  "x_label":"x",
  "y_label":"y"
}
```

`kind`: `line`, `bar`, `pie`, `scatter`. 1–500 точек. У каждой обязательны числовые x/y от −1e9 до 1e9, необязателен label до 120 символов. Для pie используются y как величины, label или x как название; y неотрицательны, сумма положительна. Подписи осей до 100 символов.

Произвольные формулы сервер не вычисляет: AI-клиент должен передать sampled points. Line соединяет точки в переданном порядке.

## Текст и документы

| Инструмент | Параметры | Поведение |
|---|---|---|
| `text_create` | **title, content**, tab_id?, workspace_id? | Создаёт Markdown-карточку. |
| `text_update` | **widget_id, content**, workspace_id? | Заменяет content существующего markdown-виджета. |
| `markdown_render` | **title, content**, tab_id?, workspace_id? | Создаёт безопасно отображаемый Markdown, возвращает карточку, а не raw HTML. |
| `code_create` | **title, code, language**, tab_id?, workspace_id? | Карточка исходного кода с лексической подсветкой и копированием. |

Предел content/code — 100000 символов. Raw HTML отображается как текст. Ссылки ограничены http/https, открываются с noreferrer/noopener. Изображения и произвольные URL-ресурсы AI не загружаются.

## Интерфейс

| Инструмент | Параметры | Поведение |
|---|---|---|
| `ui_show_notification` | **message**, level?, title?, tab_id?, workspace_id? | Сохранённая notification-карточка; уровень по умолчанию info. |
| `ui_create_tab` | **title** (до 100), workspace_id? | Создаёт и активирует вкладку; возвращает `tab`. Максимум 20 вкладок. |
| `ui_switch_tab` | **tab_id**, workspace_id? | Активирует существующую вкладку во всех браузерах пространства. |
| `ui_set_layout` | layout?, theme?, workspace_id? | Хотя бы один параметр: grid/list и/или light/dark/system. Сохраняется в SQLite. |

## События

Мутации публикуют `workspace.created/cleared/deleted/updated`, `widget.created/updated/deleted/moved`, `math.updated`, `ui.tab_created/tab_switched/layout_changed`. WebSocket при подключении получает `state.snapshot`. Полный публичный State находится в payload. Каждое событие содержит workspace_id и version; связанные с карточкой — также widget_id.

Недавняя история доступна через авторизованный `GET /api/workspaces/{id}/events`; она возвращает до 100 событий от новых к старым. После удаления workspace его история не выдаётся API, но остаётся локально в SQLite до отдельного обслуживания данных.

### Уведомления на Mac

`ui_show_notification` продолжает создавать постоянную карточку и через committed event отправляет тихое системное уведомление macOS, когда Go-сервер запущен на Mac. OpenAI API key не требуется. Доставка ограничена очередью и лимитом; ошибки ОС не откатывают созданную карточку. Проверка системной доставки — `./ai-workspace --notify-test`. Стиль и разрешения баннеров задаёт macOS, отправитель — Script Editor. Повторный вызов MCP с новым ID считается новым сообщением; транспортная доставка одного event version дедуплицируется.

### Изображения и локальный Codex

`widget_create` и `widget_update` теперь поддерживают тип `image`: config `{url,alt,caption?}`. URL — публичный HTTPS без credentials или управляемый `/api/media/{workspace}/{id}`. Для локального исходного файла используйте media_import; file:// и произвольные локальные сетевые URL запрещены. Картинка загружается браузером с no-referrer, сервер не служит произвольным HTTP-прокси.

Встроенный ассистент вызывает те же возможности через официальный App Server dynamic-tool adapter локального Codex. Внешние MCP endpoints сохранены. Теперь 28 инструментов, включая media_import. CLI авторизуется собственным `codex login`, отдельный OPENAI_API_KEY не требуется.

## Локальные файлы: media_import

Параметры: **path** (абсолютный путь, до 4096), workspace_id?, title?, alt?, tab_id?, attach? (по умолчанию true).

Импортирует PNG/JPEG/GIF/WebP, MP4/WebM, MP3/WAV/OGG/M4A или PDF из разрешённых корней, копирует в постоянное хранилище и создаёт image/video/audio/file. Возвращает `asset:{id,name,mime,size,url}`, `widget` и state/version. `attach:false` возвращает только asset без изменения состояния панели — для составной галереи. MIME определяется по байтам, размер ≤25 МБ. URL приватен для текущей локальной установки; публичный HTTPS не требуется. Повторный импорт одинаковых байтов переиспользует asset, но attach=true создаёт новую карточку: перечитайте список, если не уверены в прежнем результате.

```json
{"path":"/absolute/path/to/photo.png","title":"Фотография","attach":true}
```

По умолчанию корни — cwd сервера и ~/.codex/generated_images. Дополнительные корни: WORKSPACE_MEDIA_IMPORT_ROOTS. Symlink escape и чтение произвольных файлов вне корней отклоняются. Ошибка импорта не возвращается как фиктивный успех.

## Дополнительные типы виджетов

Все создаются инструментом `widget_create` с type/title/config и изменяются через widget_update. Ниже показан config; поля с ? необязательны.

| type | config | Поведение |
|---|---|---|
| gallery | images:[{url,alt,caption?}], 1–20 | Миниатюры, увеличение, локальные asset URL и HTTPS |
| video | url, caption? | Нативный проигрыватель; без autoplay |
| audio | url, caption? | Нативный аудиоплеер; запуск пользователем |
| file | url, filename, description? | Вложение/скачивание, например PDF |
| checklist | items:[{id,text,done}], 0–100 | Уникальные ID, интерактивные отметки сохраняются |
| metric | value:string, unit?, label?, change?, trend?:up/down/neutral | Число/показатель с подписью |
| progress | value:number (0–100), label?, detail? | Полоса прогресса и текст |

```json
{"type":"checklist","title":"План","config":{"items":[{"id":"one","text":"Проверить чертёж","done":false}]}}
```

Метаданные файлов сохраняются отдельно от виджетов. Удаление карточки не удаляет файл; квоты и резервное копирование описаны в README.
