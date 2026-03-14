	package main

	import (
		"bytes"
		"database/sql"
		"encoding/json"
		"fmt"
		"log"
		"net/http"
		"os"
		"os/exec"
		"strings"
		"sync"
		"strconv"
		tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
		_ "github.com/mattn/go-sqlite3"
		"math/rand"
	)

	var db *sql.DB
	var bot *tgbotapi.BotAPI
	var generationMutex sync.Mutex

	var servers = map[string][]string{
		"USA": {"exmaple.com:port"},
		"NL": {
			"example1.com:port",
			"example2.com:port",
		},
		"RU": {"example3.com:port"},
	}

	func main() {
		botToken := os.Getenv("TELEGRAM_BOT_TOKEN")

		var err error
		bot, err = tgbotapi.NewBotAPI(botToken)
		if err != nil {
			log.Fatal(err)
		}

		db, err = sql.Open("sqlite3", "./warp.db")
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()
		initDB()

		http.HandleFunc("/", webhookHandler)

		log.Println("Starting server on :8080")
		log.Fatal(http.ListenAndServe(":8080", nil))
	}

	// --------------------- Init DB ---------------------

	func initDB() {
		sqlStmt := `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER UNIQUE NOT NULL,
			full_name TEXT,
			username TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS warp_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			country TEXT NOT NULL,
			config TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(user_id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS cfl_keys (
			key_id INTEGER PRIMARY KEY,
			id TEXT NOT NULL,
			token TEXT NOT NULL,
			FOREIGN KEY(key_id) REFERENCES warp_keys(id) ON DELETE CASCADE
		);
		`
		_, err := db.Exec(sqlStmt)
		if err != nil {
			log.Fatal(err)
		}
	}

	// --------------------- Webhook ---------------------

	func webhookHandler(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var update tgbotapi.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		go handleUpdate(update)
		w.WriteHeader(http.StatusOK)
	}

	func handleUpdate(update tgbotapi.Update) {
		if update.Message != nil {
			fullName := update.Message.From.FirstName + " " + update.Message.From.LastName
			ensureUserExists(update.Message.From.ID, fullName, update.Message.From.UserName)

			if update.Message.IsCommand() {
				switch update.Message.Command() {
				case "start":
					sendMainMenu(update.Message.Chat.ID)
				}
			} else {
				switch update.Message.Text {
				case "🌍 Создать ключ":
					sendCountrySelection(update.Message.Chat.ID)
				case "📄 Мои ключи":
					sendUserKeys(update.Message.Chat.ID, update.Message.From.ID)
				case "📱 Мобильные приложения":
					sendMobileApps(update.Message.Chat.ID)
				}
			}
			return
		}

		if update.CallbackQuery != nil {
			handleCallback(update.CallbackQuery)
		}
	}


	func ensureUserExists(userID int64, fullName, username string) {
		_, _ = db.Exec(`INSERT OR IGNORE INTO users (user_id, full_name, username) VALUES (?, ?, ?)`, userID, fullName, username)
	}

	// --------------------- Main menu ---------------------

	func sendMainMenu(chatID int64) {
		text := `Добро пожаловать!

	Этот бот создаёт персональную конфигурацию для защищённого интернет-подключения.
	Выберите страну сервера ниже, и бот автоматически сгенерирует ваш ключ доступа.
	Для локаций с белыми списками рекомендовано использовать RU.
	Временное ограничение: 5 ключей на аккаунт`

		inlineKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("NL", "country_nl"),
				tgbotapi.NewInlineKeyboardButtonData("RU", "country_ru"),
				tgbotapi.NewInlineKeyboardButtonData("USA", "country_usa"),
			),
		)
		// Отправляем только фото с caption и кнопками выбора страны
		photo := tgbotapi.NewPhoto(chatID, tgbotapi.FilePath("welcome.jpg"))
		photo.Caption = text
		photo.ReplyMarkup = inlineKeyboard
		bot.Send(photo)
		replyKeyboard := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("🌍 Создать ключ"),
				tgbotapi.NewKeyboardButton("📄 Мои ключи"),
				tgbotapi.NewKeyboardButton("📱 Мобильные приложения"),
			),
		)

		appKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🤖 Android", "https://play.google.com/store/apps/details?id=com.zaneschepke.wireguardautotunnel&hl=ru"),
				tgbotapi.NewInlineKeyboardButtonURL(" iOS", "https://apps.apple.com/ru/app/defaultvpn/id6744725017"),
			),
		)
		msg2 := tgbotapi.NewMessage(chatID, "Скачайте приложение для вашего устройства:")
		msg2.ReplyMarkup = appKeyboard
		bot.Send(msg2)

		msg3 := tgbotapi.NewMessage(chatID, "Вы также можете использовать кнопки ниже для быстрого доступа:")
		msg3.ReplyMarkup = replyKeyboard
		bot.Send(msg3)

	}

	// --------------------- Inline Keyboard ---------------------

	func sendCountrySelection(chatID int64) {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("NL", "country_nl"),
				tgbotapi.NewInlineKeyboardButtonData("RU", "country_ru"),
				tgbotapi.NewInlineKeyboardButtonData("USA", "country_usa"),
			),
		)
		sendUserInlineMessage(chatID, "Выберите страну:", keyboard, "")
	}

	// --------------------- Keys ------------------------------------

	func sendUserKeys(chatID int64, userID int64) {
		rows, err := db.Query("SELECT id, country, created_at FROM warp_keys WHERE user_id=? ORDER BY id DESC", userID)
		if err != nil {
			bot.Send(tgbotapi.NewMessage(chatID, "Ошибка получения ключей"))
			return
		}
		defer rows.Close()

		var buttons [][]tgbotapi.InlineKeyboardButton
		found := false

		for rows.Next() {
			var id int
			var country, created string

			if err := rows.Scan(&id, &country, &created); err != nil {
				log.Println("scan error:", err)
				continue
			}

			text := fmt.Sprintf("%s | %s", country, created)

			buttons = append(buttons,
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData(text, fmt.Sprintf("key_%d", id)),
					tgbotapi.NewInlineKeyboardButtonData("❌", fmt.Sprintf("delete_%d", id)),
				),
			)
			found = true
		}

		if !found {
			sendUserInlineMessage(chatID, "У вас нет ключей", tgbotapi.NewInlineKeyboardMarkup(), "")
			return
		}

		keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons...)
		sendUserInlineMessage(chatID, "Ваши ключи:", keyboard, "")
	}

	// --------------------- Gen Keys ------------------------------

	func getOrGenerateKey(userID int64, country string) (string, error) {
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM warp_keys WHERE user_id=?", userID).Scan(&count)
		if err != nil {
			return "", err
		}

		if count >= 5 {
			return "", fmt.Errorf("🚫 Лимит ключей достигнут\nМаксимум: 5 ключей на аккаунт\nУдалите старые ключи в 'Мои ключи'")
		}

		endpoints := servers[country]
		if len(endpoints) == 0 {
			return "", fmt.Errorf("нет серверов для страны %s", country)
		}

		endpoint := endpoints[rand.Intn(len(endpoints))]
		generationMutex.Lock()
		defer generationMutex.Unlock()
		
		cmd := exec.Command(
			"./warp_generator.sh",
			strconv.FormatInt(userID, 10),
			country,
			endpoint,
		)

		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("warp generator failed: %v\n%s", err, out.String())
		}

		return out.String(), nil
	}


	func sendActionButtons(chatID int64, keyID int) {
		keyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Скачать .conf", fmt.Sprintf("download_%d", keyID)),
				tgbotapi.NewInlineKeyboardButtonData("Показать", fmt.Sprintf("show_%d", keyID)),
			),
		)
		sendUserInlineMessage(chatID, "Конфигурация готова:", keyboard, "")
	}

	// --------------------- Callback ---------------------

	func handleCallback(cq *tgbotapi.CallbackQuery) {
		chatID := cq.Message.Chat.ID
		userID := cq.From.ID
		data := cq.Data

		switch {
		case strings.HasPrefix(data, "country_"):

			country := strings.ToUpper(strings.TrimPrefix(data, "country_"))

			_, err := getOrGenerateKey(userID, country)
			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Ошибка генерации:\n%v", err)))
				return
			}

			var keyID int
			err = db.QueryRow(
				"SELECT id FROM warp_keys WHERE user_id=? ORDER BY id DESC LIMIT 1",
				userID,
			).Scan(&keyID)

			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Ошибка получения ключа"))
				return
			}

			sendActionButtons(chatID, keyID)

		case strings.HasPrefix(data, "download_"):

			idStr := strings.TrimPrefix(data, "download_")
			keyID, _ := strconv.Atoi(idStr)

			var config string

			err := db.QueryRow(
				"SELECT config FROM warp_keys WHERE id=? AND user_id=?",
				keyID, userID,
			).Scan(&config)

			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Конфигурация не найдена"))
				return
			}

			tmpFile := fmt.Sprintf("%d.conf", keyID)
			os.WriteFile(tmpFile, []byte(config), 0644)

			bot.Send(tgbotapi.NewDocument(chatID, tgbotapi.FilePath(tmpFile)))

			os.Remove(tmpFile)

		case data == "my_keys":
			sendUserKeys(chatID, userID)

		case strings.HasPrefix(data, "delete_"):
			id := strings.TrimPrefix(data, "delete_")
			var cfID, cfToken string
			db.QueryRow("SELECT id, token FROM cfl_keys WHERE key_id=?", id).Scan(&cfID, &cfToken)

			if cfID != "" && cfToken != "" {
				if err := deleteCloudflareKey(cfID, cfToken); err != nil {
					log.Println("Ошибка удаления ключа с Cloudflare:", err)
				}
			}

			db.Exec("DELETE FROM warp_keys WHERE id=? AND user_id=?", id, userID)
			db.Exec("DELETE FROM cfl_keys WHERE key_id=?", id)

			bot.Send(tgbotapi.NewMessage(chatID, "Ключ удален"))
			sendUserKeys(chatID, userID)

		case strings.HasPrefix(data, "key_"):

			idStr := strings.TrimPrefix(data, "key_")
			keyID, _ := strconv.Atoi(idStr)

			var config string
			err := db.QueryRow(
				"SELECT config FROM warp_keys WHERE id=? AND user_id=?",
				keyID, userID,
			).Scan(&config)

			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Ключ не найден"))
				return
			}

			sendActionButtons(chatID, keyID)

		case strings.HasPrefix(data, "show_"):

			idStr := strings.TrimPrefix(data, "show_")
			keyID, _ := strconv.Atoi(idStr)

			var config string

			err := db.QueryRow(
				"SELECT config FROM warp_keys WHERE id=? AND user_id=?",
				keyID, userID,
			).Scan(&config)

			if err != nil {
				bot.Send(tgbotapi.NewMessage(chatID, "Конфигурация не найдена"))
				return
			}

			keyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("⬅ Назад", fmt.Sprintf("back_%d", keyID)),
				),
			)

			sendUserInlineMessage(chatID, fmt.Sprintf("```\n%s\n```", config), keyboard, "MarkdownV2")

		case strings.HasPrefix(data, "back_"):

			idStr := strings.TrimPrefix(data, "back_")
			keyID, _ := strconv.Atoi(idStr)

			sendActionButtons(chatID, keyID)
		}
	}

	var lastUserMessage = make(map[int64]int)

	func sendUserInlineMessage(chatID int64, text string, keyboard tgbotapi.InlineKeyboardMarkup, parseMode string) (*tgbotapi.Message, error) {
		if msgID, ok := lastUserMessage[chatID]; ok {
			del := tgbotapi.DeleteMessageConfig{
				ChatID:    chatID,
				MessageID: msgID,
			}
			_, _ = bot.Request(del)
		}

		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = keyboard
		if parseMode != "" {
			msg.ParseMode = parseMode
		}

		sentMsg, err := bot.Send(msg)
		if err != nil {
			return nil, err
		}

		lastUserMessage[chatID] = sentMsg.MessageID
		return &sentMsg, nil
	}

	func sendMobileApps(chatID int64) {
		appKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonURL("🤖 Android", "https://play.google.com/store/apps/details?id=com.zaneschepke.wireguardautotunnel&hl=ru"),
				tgbotapi.NewInlineKeyboardButtonURL(" iOS", "https://apps.apple.com/ru/app/defaultvpn/id6744725017"),
			),
		)

		msg := tgbotapi.NewMessage(chatID, "Скачайте приложение для вашего устройства:")
		msg.ReplyMarkup = appKeyboard
		bot.Send(msg)
	}

	func deleteCloudflareKey(id string, token string) error {
		url := fmt.Sprintf("https://api.cloudflareclient.com/v0i1909051800/reg/%s", id)

		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "okhttp/3.12.1")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 && resp.StatusCode != 204 {
			return fmt.Errorf("cloudflare returned %d", resp.StatusCode)
		}

		return nil
	}