# max-cli-example

Консольный пример приложения для [`github.com/szonov/max`](https://github.com/szonov/max).

## Что делает пример

Сессия хранится в файле:

```text
$HOME/.max-session.json
```

Команды:

```text
Usage: max-cli-example <command>

Commands:
  start   запустить клиент MAX, при необходимости авторизоваться через QR и печатать входящие события
  stop    подключиться с сохранённой сессией, выполнить Logout и удалить локальную сессию
```

Вывод оформлен через [`github.com/pterm/pterm`](https://github.com/pterm/pterm): цветной логгер, секции и маскированный ввод пароля 2FA.

CLI построен на [`github.com/urfave/cli/v3`](https://github.com/urfave/cli).
