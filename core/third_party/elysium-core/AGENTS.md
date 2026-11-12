# AGENTS.md — что здесь наше и что нельзя ломать

Этот репозиторий — форк `legiz-ru/Prizrak-Core`, который сам тянет upstream
`MetaCubeX/mihomo` (ветка `Alpha`). Часть кода пришла из форка/upstream, а часть
(`zapret` и UDP `fake`) — наша. При апстримах и рефакторингах нижеперечисленное
удалять, переименовывать и «упрощать» нельзя.

## Наши изменения (не из upstream и не из форка)

### 1. `transport/zapret/` — пакетный TCP/UDP-десинк как в zapret

- `config.go` — разбор блока `zapret:` (`tcp`/`udp`, `ports`, `desync`,
  `split-pos`, `seqovl`, `repeats`, `cutoff`, `ttl`, `fooling`, `fake-tls`,
  `fake`). Поддерживаются `fake`, `split`, `multisplit`; `disorder`, `oob`,
  `ipfrag`, `badsum`, `badseq` — нет.
- `packet.go` — сборка/разбор IPv4/IPv6 TCP/UDP через
  `github.com/metacubex/gopacket`; `layers.Init()` вызывается через
  `ensureLayers()`.
- `strategy.go` — fake, split, multisplit, `seqovl` (pattern перед сегментом,
  как `--dpi-desync-split-seqovl`), TTL-фулинг.
- `engine.go` — `Engine`, `RegisterTCP`, `RegisterUDP`, `zapretConn`,
  `zapretPacketConn`, `ErrUnsupported`.
- `engine_windows.go` — Windows amd64: WinDivert, один handle на поток,
  `QUEUE_SIZE=256 KiB`, `QUEUE_LENGTH=512`, fallback на `fragment/finalmask`
  на Windows ARM64.
- `windivert_windows.go` — загрузка `WinDivert.dll` через `syscall`
  (без cgo), `WinDivertOpen/Recv/Send/Shutdown/Close`, `WINDIVERT_ADDRESS`.
- `engine_stub.go` — не-Windows: `newBackend()` возвращает `ErrUnsupported`.

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

- `FinalMask *FinalMaskOption` (`finalmask`) и `Zapret *zapret.Option`
  (`zapret`) — отдельные поля.
- `freedom` обязан сохранять старое поведение `fragment`/`finalmask`, когда
  `zapret` не активен.
- `zapret` активен только на Windows amd64. Если движок создан, он заменяет
  `fragment`/`finalmask` (в лог пишется warning).
- На не-Windows и на Windows ARM64 `zapret` игнорируется с warning, и
  используются `fragment`/`finalmask`. Конфиг при этом **не должен падать**.
- На Windows amd64 отсутствие `WinDivert.dll`/`WinDivert64.sys` или прав
  администратора — это ошибка конфига, а не тихий fallback.

### 4. Регистрация маски

`transport/finalmask/option/option.go`: `fake` зарегистрирован в
`BuildUDPMask`. Если убрать — сломается `finalmask.udp: [{type: fake}]`.

### 5. Документация и тесты

- `docs/config.yaml` — секция `zapret:` и встроенные payload’ы.
- Тесты: `transport/zapret/*_test.go`, `transport/finalmask/fake/fake_test.go`,
  `adapter/outbound/freedom_udp_test.go`,
  `adapter/outbound/freedom_zapret_test.go`.

## Жёсткие правила при апстриме

1. **Не удалять и не переименовывать** `transport/zapret/`,
   `transport/finalmask/fake/`, поля `zapret`/`finalmask` в `freedom`,
   регистрацию `fake` в `finalmask/option`.
2. **Не убирать fallback** `ErrUnsupported` → `fragment`/`finalmask`
   на не-Windows и Windows ARM64. Ошибка `WinDivert has no ARM64 driver`
   должна оставаться обёрнутой в `ErrUnsupported`.
3. **Не делать `zapret` обязательным** для конфигов: старые конфиги с
   `fragment`/`finalmask` должны работать без изменений.
4. **Не совмещать** `zapret` и `fragment`/`finalmask` на Windows amd64:
   zapret имеет приоритет, остальное игнорируется с warning.
5. `go.mod` содержит критичный replace:
   `github.com/metacubex/utls => github.com/legiz-ru/prizrak-utls ...`.
   При конфликтах `go.sum` оставлять хеши `legiz-ru/prizrak-utls`, а не
   оригинального `metacubex/utls`.
6. `common/convert`: `vmess`/`vless` `httpupgrade` должен давать
   `network: ws` и `v2ray-http-upgrade: true`. `ed`:
   - `ws` → `max-early-data` + `early-data-header-name`;
   - `httpupgrade` → `v2ray-http-upgrade-fast-open`.
   Это правило уже ломалось при мерджах — проверять тестами
   `common/convert`.
7. WinDivert-код не должен требовать cgo: сборка идёт с `CGO_ENABLED=0`.

## Проверка после апстрима

```bash
go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=windows GOARCH=arm64 go build ./...

go test ./transport/zapret/ ./transport/finalmask/... ./adapter/outbound/ ./common/convert/
```

Проверка конфига (не-Windows: `zapret` должен дать warning и не упасть):

```bash
go run . -t -f path/to/config.yaml
```

Быстрый grep, что ничего не потеряно:

```bash
grep -R "case \"fake\"" transport/finalmask/option/option.go
grep -R "zapret" adapter/outbound/freedom.go
ls transport/zapret transport/finalmask/fake
```

## Сборка Windows

```bash
make windows-amd64-v3 windows-amd64-compatible windows-arm64
```

- x64: `zapret` работает, нужны `WinDivert.dll` + `WinDivert64.sys` рядом с
  exe и запуск от администратора.
- ARM64: WinDivert не имеет ARM64-драйвера, `zapret` автоматически
  игнорируется, работают `fragment`/`finalmask`.
- Сборки `with_low_memory`/`with_powersave` сейчас не требуются.

## Git

- `origin` — `legiz-ru/Prizrak-Core`.
- `upstream` — `MetaCubeX/mihomo`, синхронизация обычно через `upstream/Alpha`
  или тег `v1.19.34-rX`.
- Не делать force-push и не переписывать общую ветку без явной просьбы.
