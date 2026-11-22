# AGENTS.md — что здесь наше и что нельзя ломать

Этот репозиторий — форк `legiz-ru/Prizrak-Core`, который сам тянет upstream
`MetaCubeX/mihomo` (ветка `Alpha`). Часть кода пришла из форка/upstream, а часть
(`zapret2` и UDP `fake`) — наша. При апстримах и рефакторингах нижеперечисленное
удалять, переименовывать и «упрощать» нельзя.

## Наши изменения (не из upstream и не из форка)

### 1. `transport/zapret2/` — пакетная обработка TCP/UDP

- `type: zapret2` — отдельный outbound на Windows x64. WinDivert захватывает
  только точный кортеж сокета, выбранного правилами или группой прокси.
- Устройство и пример YAML описаны в [документации](docs/zapret2-architecture.md).

### 2. `transport/finalmask/fake/` — UDP-маска `fake`

- `fake.go` — decoy-датаграммы перед реальной; `repeats`, `cutoff`, `ttl`
  (с восстановлением TTL), `delay`.
- `option.go` — `payloads` с `builtin`, `file`, `hex`, `base64`, `text`,
  `packet`, `random`, `ports`.
- `payloads.go` — встроенные payload’ы: `discord-udp`, `discord`, `quic`,
  `stun`, `dtls`, `tls-google`, `tls`, `tls-microsoft`.
  `discord-udp` — точный QUIC Initial из zapret (`ACTIVE_DISCORD_UDP.bin`,
  1200 байт). Экспортируется через `fake.Builtin(name)`.

### 3. `adapter/outbound/freedom.go`

- `freedom` сохраняет поведение `fragment`/`finalmask`. После удаления
  движка Zapret v1 в r6 старый YAML-блок `zapret:` игнорируется с предупреждением.

### 4. Регистрация маски

`transport/finalmask/option/option.go`: `fake` зарегистрирован в
`BuildUDPMask`. Если убрать — сломается `finalmask.udp: [{type: fake}]`.

### 5. Документация и тесты

- `docs/config.yaml` — примеры `zapret2` и встроенные payload’ы UDP `fake`.
- Тесты: `transport/zapret2/*_test.go`, `transport/finalmask/fake/fake_test.go`,
  `adapter/outbound/freedom_udp_test.go`, `adapter/parser_freedom_legacy_test.go`.

## Жёсткие правила при апстриме

1. **Не удалять и не переименовывать** `transport/zapret2/`,
   `transport/finalmask/fake/`, поле `finalmask` в `freedom` и
   регистрацию `fake` в `finalmask/option`.
2. **Не расширять перехват Zapret2** за пределы сокетов выбранного outbound.
3. `go.mod` содержит критичный replace:
   `github.com/metacubex/utls => github.com/legiz-ru/prizrak-utls ...`.
   При конфликтах `go.sum` оставлять хеши `legiz-ru/prizrak-utls`, а не
   оригинального `metacubex/utls`.
4. `common/convert`: `vmess`/`vless` `httpupgrade` должен давать
   `network: ws` и `v2ray-http-upgrade: true`. `ed`:
   - `ws` → `max-early-data` + `early-data-header-name`;
   - `httpupgrade` → `v2ray-http-upgrade-fast-open`.
   Это правило уже ломалось при мерджах — проверять тестами
   `common/convert`.
5. WinDivert-код не должен требовать cgo: сборка идёт с `CGO_ENABLED=0`.

## Проверка после апстрима

```bash
go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=windows GOARCH=arm64 go build ./...

go test ./transport/zapret2/ ./transport/finalmask/... ./adapter/ ./adapter/outbound/ ./common/convert/
```

Проверка старого конфига (блок Zapret v1 игнорируется с предупреждением):

```bash
go run . -t -f path/to/config.yaml
```

Быстрый grep, что ничего не потеряно:

```bash
grep -R "case \"fake\"" transport/finalmask/option/option.go
ls transport/zapret2 transport/finalmask/fake
```

## Сборка Windows

```bash
make windows-amd64-v3 windows-amd64-compatible windows-arm64
```

- x64: для Zapret2 нужны `WinDivert.dll` + `WinDivert64.sys` рядом с
  exe и запуск от администратора.
- ARM64: `type: zapret2` недоступен, `freedom` с `fragment`/`finalmask` работает.
- Сборки `with_low_memory`/`with_powersave` сейчас не требуются.

## Git

- `origin` — `legiz-ru/Prizrak-Core`.
- `upstream` — `MetaCubeX/mihomo`, синхронизация обычно через `upstream/Alpha`
  или тег `v1.19.34-rX`.
- Не делать force-push и не переписывать общую ветку без явной просьбы.

## graphify

- Перед изменением кода используй `graphify query "<вопрос>"`, если нужно найти
  зависимости, вызывающий код или связанные компоненты и уже есть
  `graphify-out/graph.json`. Для связи между двумя узлами используй
  `graphify path "<A>" "<B>"`. Найденные связи сверяй с исходниками.
- Для простых задач, при отсутствии графа или если граф не дал нужного
  контекста, используй обычный поиск по репозиторию.
- Используй существующий граф без автоматической перестройки при запросах
  и правках. Явные команды `/graphify` выполняй по инструкции навыка.
