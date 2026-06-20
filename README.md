# Pidorometr3000

Telegram-бот для ежедневного шуточного розыгрыша участника дня в группе.

## Что умеет

- SQLite без отдельного сервера БД: данные лежат в файле `pidorometr3000.db`.
- Автоматическая регистрация участников, которые пишут в чат.
- Ручная регистрация через `/register`.
- Ежедневный розыгрыш по времени из настроек.
- Выбор без повторов: бот старается выбирать тех, кто давно не выигрывал.
- Упоминание через `@username`, если username есть, иначе через `tg://user?id=...`.
- Рейтинг, история, список участников.
- Исключение админов из розыгрыша.
- Настройка названия конкурса и времени прямо из Telegram.

## Команды

```text
/help — помощь
/register — записаться
/leave — выйти из розыгрыша
/list — список участников
/today — показать или провести сегодняшний розыгрыш
/force — попробовать провести розыгрыш сегодня
/rating или /top — рейтинг
/history — история
/settings — настройки
/settime 09:00 — изменить время ежедневного розыгрыша
/settitle Герой дня — изменить название конкурса
/excludeadmins on|off — исключать админов
/autoregister on|off — авторегистрация участников, которые пишут в чат
```

## Настройка бота в Telegram

1. Добавь бота в группу.
2. Дай право отправлять сообщения.
3. Через BotFather отключи Privacy Mode:

```text
/privacy
@pidorometr3000_bot
Disable
```

Без отключения Privacy Mode бот будет видеть команды, но не все обычные сообщения. Тогда авторегистрация по обычным сообщениям работать не будет, и участникам надо будет писать `/register`.

## Локальный запуск

```bash
cp .env .env
nano .env

go mod tidy
go run ./cmd/pidorometr3000
```

## Сборка под сервер

Linux x86_64:

```bash
GOOS=linux GOARCH=amd64 go build -o pidorometr3000 ./cmd/pidorometr3000
```

Linux ARM64:

```bash
GOOS=linux GOARCH=arm64 go build -o pidorometr3000 ./cmd/pidorometr3000
```

## Деплой на сервер без Docker

```bash
sudo mkdir -p /opt/pidorometr3000
sudo cp pidorometr3000 /opt/pidorometr3000/
sudo cp .env /opt/pidorometr3000/
sudo chmod +x /opt/pidorometr3000/pidorometr3000
sudo cp deploy/pidorometr3000.service /etc/systemd/system/pidorometr3000.service
sudo systemctl daemon-reload
sudo systemctl enable pidorometr3000
sudo systemctl start pidorometr3000
sudo systemctl status pidorometr3000
```

Логи:

```bash
journalctl -u pidorometr3000 -f
```

## Важно про токен

Не храни реальный токен в Git. Если токен был отправлен в чат или кому-то показан, перевыпусти его через BotFather.
