package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/pterm/pterm"
	"github.com/szonov/max/maxclient"
	"github.com/szonov/max/protocol"
	"github.com/urfave/cli/v3"
)

const sessionFileName = ".max-session.json"

var logger = pterm.DefaultLogger.WithLevel(pterm.LogLevelInfo)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := &cli.Command{
		Name:      "max-cli-example",
		Usage:     "консольный пример приложения на github.com/szonov/max",
		UsageText: "max-cli-example <command>",
		Commands: []*cli.Command{
			{
				Name:  "start",
				Usage: "запустить клиент MAX, при необходимости авторизоваться через QR и печатать входящие события",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sessionPath, err := defaultSessionPath()
					if err != nil {
						return err
					}
					return runStart(ctx, sessionPath)
				},
			},
			{
				Name:  "stop",
				Usage: "подключиться с сохранённой сессией, выполнить Logout и удалить локальную сессию",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sessionPath, err := defaultSessionPath()
					if err != nil {
						return err
					}
					return runStop(ctx, sessionPath)
				},
			},
		},
	}

	if err := app.Run(ctx, os.Args); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Программа завершилась с ошибкой", logger.Args("error", err))
		os.Exit(1)
	}
}

func defaultSessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить домашний каталог пользователя: %w", err)
	}
	return filepath.Join(home, sessionFileName), nil
}

func newClient(sessionPath string) *maxclient.Client {
	return maxclient.New(
		maxclient.WithSessionFileStore(sessionPath),
		maxclient.WithPasswordFunc(readPassword),
	)
}

func runStart(ctx context.Context, sessionPath string) error {
	logger.Info("Запускаем MAX клиент", logger.Args("session", sessionPath))

	client := newClient(sessionPath)

	client.Events.AuthRequired.Subscribe(func(ctx context.Context) {
		logger.Warn("Сохранённой рабочей сессии нет: нужна авторизация через QR-код")
		logger.Info("Откройте мобильное приложение MAX и отсканируйте QR-код, который появится ниже")

		go func() {
			if _, err := client.LoginViaQr(ctx); err != nil {
				client.Stop(err)
			}
		}()
	})

	var cancelQR context.CancelFunc

	client.Events.QrCode.Subscribe(func(ctx context.Context, qr maxclient.QrCode) {
		if cancelQR != nil {
			cancelQR()
		}

		qrCtx, cancel := context.WithCancel(ctx)
		cancelQR = cancel

		go showQRCode(qrCtx, qr.Link, qr.ExpiresAt)
		//logger.Info(
		//	"Получен QR-код для входа",
		//	logger.Args("действителен_до", qr.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST")),
		//)
		//pterm.DefaultSection.Println("QR-код для авторизации в MAX")
		//qrterminal.GenerateWithConfig(qr.Link, qrterminal.Config{
		//	Level:     qrterminal.L,
		//	Writer:    os.Stdout,
		//	BlackChar: qrterminal.BLACK,
		//	WhiteChar: qrterminal.WHITE,
		//	QuietZone: 1,
		//})
		//pterm.Println()
	})

	client.Events.Ready.Subscribe(func(ctx context.Context, raw json.RawMessage) {
		if cancelQR != nil {
			cancelQR()
		}
		logger.Info("Успешное подключение к MAX, начинаем слушать входящие события")
	})

	client.Events.Message.Subscribe(func(ctx context.Context, msg protocol.Message) {
		printMessage(msg)
	})

	client.Events.Error.Subscribe(func(ctx context.Context, err error) {
		logger.Error("Клиент сообщил об ошибке", logger.Args("error", err))
	})

	return client.Start(ctx)
}

func runStop(ctx context.Context, sessionPath string) error {
	logger.Info("Запускаем MAX клиент для выхода из аккаунта", logger.Args("session", sessionPath))

	client := newClient(sessionPath)
	done := make(chan error, 1)

	client.Events.AuthRequired.Subscribe(func(ctx context.Context) {
		client.Stop(errors.New("не удалось выполнить logout: сохранённая сессия отсутствует или недействительна"))
	})

	client.Events.Ready.Subscribe(func(ctx context.Context, raw json.RawMessage) {
		go func() {
			logger.Info("Сессия успешно подключена, выполняем Logout")
			err := client.Logout(ctx)
			if err == nil {
				logger.Info("Logout выполнен успешно, локальная сессия удалена")
			}
			client.Stop(err)
			done <- err
		}()
	})

	client.Events.Error.Subscribe(func(ctx context.Context, err error) {
		logger.Error("Клиент сообщил об ошибке", logger.Args("error", err))
	})

	startErr := make(chan error, 1)
	go func() {
		startErr <- client.Start(ctx)
	}()

	select {
	case err := <-done:
		return err
	case err := <-startErr:
		return err
	case <-ctx.Done():
		client.Stop(ctx.Err())
		return ctx.Err()
	}
}
func showQRCode(ctx context.Context, link string, expiresAt time.Time) {
	pterm.Println()
	pterm.Info.Println("Для входа откройте приложение MAX и отсканируйте QR-код:")
	pterm.Println()

	qrterminal.GenerateWithConfig(link, qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 1,
	})

	pterm.Println()

	area, err := pterm.DefaultArea.Start()
	if err != nil {
		pterm.Error.Printf("Не удалось создать область вывода: %v\n", err)
		return
	}
	defer func(area *pterm.AreaPrinter) {
		area.Stop()
		pterm.Println()
		pterm.Println()
	}(area)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		remaining := time.Until(expiresAt)

		if remaining <= 0 {
			area.Update(
				pterm.Warning.Sprintf(
					"⚠️ Время действия QR-кода истекло",
				),
			)
			return
		}

		area.Update(
			pterm.Sprintf(
				"⏳ QR-код действителен ещё %s",
				pterm.LightGreen(formatRemaining(remaining)),
			),
		)

		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
		}
	}
}

func formatRemaining(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}

	minutes := total / 60
	seconds := total % 60

	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func readPassword(ctx context.Context, hint string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	logger.Warn("Для аккаунта включена двухфакторная авторизация")
	if hint != "" {
		logger.Info("Подсказка пароля", logger.Args("hint", hint))
	}

	password, err := pterm.DefaultInteractiveTextInput.
		WithMask("*").
		Show("Введите пароль 2FA")
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать пароль 2FA: %w", err)
	}
	if password == "" {
		return "", errors.New("пароль 2FA не введён")
	}
	return password, nil
}

func printMessage(msg protocol.Message) {
	logger.Info(
		"Получено входящее событие MAX",
		logger.Args(
			"cmd", msg.Cmd,
			"opcode", msg.Opcode,
			"seq", msg.Seq,
			"ver", msg.Ver,
		),
	)

	payload := formatJSON(msg.Payload)
	if payload == "" {
		logger.Warn("В событии пустой payload")
		return
	}

	pterm.DefaultBasicText.Println(payload)
}

func formatJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(data)
}
