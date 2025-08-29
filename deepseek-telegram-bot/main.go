package main

import (
	"bytes"         // Для работы с байтовыми буферами
	"context"       // Для управления контекстом запросов
	"encoding/json" // Для работы с JSON данными
	"fmt"           // Для форматированного ввода-вывода
	"io"            // Для работы с вводом-выводом
	"log"           // Для логирования
	"net/http"      // Для HTTP запросов
	"strings"       // Для работы со строками
	"sync"          // Для синхронизации горутин
	"time"          // Для работы со временем

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5" // Библиотека для Telegram Bot API
)

// Конфигурация для OpenRouter
const (
	TelegramBotToken = "7858854259:AAEET8PAdl1puDQyhkmjRa-QfhrKJW24rIA"                                                                                                                                                                                                                                                                                                                                       // Токен вашего Telegram бота
	OpenRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"                                                                                                                                                                                                                                                                                                                                        // URL API OpenRouter
	SystemPrompt     = "Тебя зовут Даня. Ты грубый бот, ты отвечаешь грубо во всех случаях. Если тебя оскорбляют, то ты оскорбляешь в ответ, можешь даже материться. Кстати, тебе лень писать с заглавной буквы и ты не всегда ставишь знаки препинания. Но если с тобой общаются адекватно и по доброму, то ты не агресируешь и отвечаешь более сдержано и даже иногда шутишь, возможно даже черным юмором." // Системный промт для задания поведения бота
)

// Список резервных API ключей (добавьте свои ключи)
var apiKeys = []string{
	"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", // Основной ключ
	"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", // Резервный ключ 1
	"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", // Резервный ключ 2
	"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", // Резервный ключ 3
	"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", // Резервный ключ 4
}

// Структура для управления API ключами
type APIKeyManager struct {
	keys    []string   // Массив с API ключами
	current int        // Индекс текущего активного ключа
	mu      sync.Mutex // Мьютекс для безопасного доступа из горутин
}

// Создаем менеджер ключей
var keyManager = &APIKeyManager{
	keys:    apiKeys, // Инициализируем массивом ключей
	current: 0,       // Начинаем с первого ключа
}

// GetCurrentKey возвращает текущий активный ключ
func (m *APIKeyManager) GetCurrentKey() string {
	m.mu.Lock()              // Блокируем мьютекс для безопасного доступа
	defer m.mu.Unlock()      // Гарантируем разблокировку при выходе из функции
	return m.keys[m.current] // Возвращаем текущий ключ
}

// RotateKey переключается на следующий ключ
func (m *APIKeyManager) RotateKey() string {
	m.mu.Lock()                                             // Блокируем мьютекс
	defer m.mu.Unlock()                                     // Гарантируем разблокировку
	m.current = (m.current + 1) % len(m.keys)               // Переходим к следующему ключу по кругу
	log.Printf("Переключился на API ключ #%d", m.current+1) // Логируем переключение
	return m.keys[m.current]                                // Возвращаем новый текущий ключ
}

// GetKeyCount возвращает количество доступных ключей
func (m *APIKeyManager) GetKeyCount() int {
	m.mu.Lock()         // Блокируем мьютекс
	defer m.mu.Unlock() // Гарантируем разблокировку
	return len(m.keys)  // Возвращаем количество ключей
}

// Структуры для OpenRouter API

// OpenRouterRequest - структура запроса к API
type OpenRouterRequest struct {
	Model       string    `json:"model"`                 // Название модели AI
	Messages    []Message `json:"messages"`              // Массив сообщений диалога
	Stream      bool      `json:"stream"`                // Флаг потоковой передачи
	MaxTokens   int       `json:"max_tokens,omitempty"`  // Максимальное количество токенов в ответе
	Temperature float64   `json:"temperature,omitempty"` // Уровень случайности ответа (0.0-1.0)
}

// Message - структура отдельного сообщения
type Message struct {
	Role    string `json:"role"`    // Роль отправителя: system/user/assistant
	Content string `json:"content"` // Текст сообщения
}

// OpenRouterResponse - структура ответа от API
type OpenRouterResponse struct {
	ID      string    `json:"id"`              // ID запроса
	Choices []Choice  `json:"choices"`         // Массив вариантов ответов
	Error   *APIError `json:"error,omitempty"` // Информация об ошибке
}

// Choice - содержит один вариант ответа AI
type Choice struct {
	Message Message `json:"message"` // Сообщение от AI
}

// APIError - структура ошибки API
type APIError struct {
	Message string `json:"message"` // Текст ошибки
}

// Функция для отправки запроса к OpenRouter API с автоматической ротацией ключей
func getAIResponse(userMessage string, conversationHistory []Message) (string, error) {
	// Максимальное количество попыток с разными ключами
	maxAttempts := keyManager.GetKeyCount()

	// Пробуем все ключи по очереди
	for attempt := 0; attempt < maxAttempts; attempt++ {
		currentKey := keyManager.GetCurrentKey()                                     // Получаем текущий ключ
		response, err := tryAPIRequest(userMessage, conversationHistory, currentKey) // Пробуем запрос

		if err != nil {
			// Проверяем, является ли ошибка лимитом запросов
			if isRateLimitError(err) && attempt < maxAttempts-1 {
				log.Printf("у меня ошибка какаято #%d, сча попробую исправить, если че напишу", keyManager.current+1)
				keyManager.RotateKey() // Переключаемся на следующий ключ
				continue               // Пробуем снова с новым ключом
			}
			return "кажись инет пропал", err // Возвращаем ошибку если не лимит или это последняя попытка
		}

		return response, nil // Возвращаем успешный ответ
	}

	return "", fmt.Errorf("все API ключи исчерпали лимит") // Все ключи не сработали
}

// Вспомогательная функция для выполнения одного запроса с конкретным ключом
func tryAPIRequest(userMessage string, conversationHistory []Message, apiKey string) (string, error) {
	// Формируем массив сообщений начиная с системного промта
	messages := []Message{
		{Role: "system", Content: SystemPrompt}, // Системное сообщение задает поведение
	}
	messages = append(messages, conversationHistory...)                      // Добавляем историю диалога
	messages = append(messages, Message{Role: "user", Content: userMessage}) // Добавляем текущее сообщение пользователя

	// Подготавливаем тело запроса
	requestBody := OpenRouterRequest{
		Model:       "deepseek/deepseek-chat-v3.1:free", // Указываем модель AI
		Messages:    messages,                           // Передаем все сообщения
		Stream:      false,                              // Отключаем потоковую передачу
		MaxTokens:   1024,                               // Лимит токенов в ответе
		Temperature: 0.7,                                // Уровень случайности
	}

	// Кодируем структуру в JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("ошибка кодирования JSON: %v", err)
	}

	log.Printf("Использую API ключ #%d", getKeyIndex(apiKey)+1) // Логируем какой ключ используем

	// Создаем контекст с таймаутом для ограничения времени запроса
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel() // Гарантируем отмену контекста

	// Создаем HTTP POST запрос
	req, err := http.NewRequestWithContext(ctx, "POST", OpenRouterAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %v", err)
	}

	// Устанавливаем необходимые HTTP заголовки
	req.Header.Set("Content-Type", "application/json")                // Тип содержимого JSON
	req.Header.Set("Authorization", "Bearer "+apiKey)                 // API ключ для аутентификации
	req.Header.Set("HTTP-Referer", "https://github.com/telegram-bot") // Обязательный заголовок для OpenRouter
	req.Header.Set("X-Title", "Telegram AI Bot")                      // Название приложения

	// Создаем HTTP клиент с таймаутом
	client := &http.Client{Timeout: 60 * time.Second}
	// Выполняем запрос
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка сети: %v", err) // Ошибка сетевого соединения
	}
	defer resp.Body.Close() // Гарантируем закрытие тела ответа

	// Читаем тело ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	log.Printf("Status: %d, Response: %s", resp.StatusCode, string(body)) // Логируем статус и ответ

	// Проверяем HTTP статус код
	if resp.StatusCode != http.StatusOK {
		// Пытаемся распарсить ошибку из JSON
		var apiError OpenRouterResponse
		if json.Unmarshal(body, &apiError) == nil && apiError.Error != nil {
			return "", fmt.Errorf("API ошибка: %s", apiError.Error.Message) // Возвращаем ошибку API
		}
		return "", fmt.Errorf("HTTP ошибка: %s", resp.Status) // Возвращаем HTTP ошибку
	}

	// Декодируем успешный JSON ответ
	var openRouterResponse OpenRouterResponse
	if err := json.Unmarshal(body, &openRouterResponse); err != nil {
		return "", fmt.Errorf("ошибка декодирования: %v", err)
	}

	// Извлекаем текст ответа из первого варианта
	if len(openRouterResponse.Choices) > 0 {
		return openRouterResponse.Choices[0].Message.Content, nil // Возвращаем ответ AI
	}

	return "", fmt.Errorf("пустой ответ") // Если ответ пустой
}

// Функция для проверки, является ли ошибка лимитом запросов
func isRateLimitError(err error) bool {
	errorMsg := err.Error() // Получаем текст ошибки
	// Типичные сообщения об ошибках лимита
	rateLimitIndicators := []string{
		"rate limit",         // Превышен лимит запросов
		"quota",              // Исчерпана квота
		"limit exceeded",     // Лимит превышен
		"too many requests",  // Слишком много запросов
		"429",                // HTTP код 429
		"insufficient quota", // Недостаточно квоты
	}

	// Проверяем каждый индикатор в тексте ошибки
	for _, indicator := range rateLimitIndicators {
		if containsIgnoreCase(errorMsg, indicator) {
			return true // Нашли индикатор лимита
		}
	}
	return false // Не нашли индикаторов лимита
}

// Вспомогательная функция для поиска подстроки без учета регистра
func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)             // Приводим к нижнему регистру
	substr = strings.ToLower(substr)   // Приводим к нижнему регистру
	return strings.Contains(s, substr) // Ищем подстроку
}

// Функция для получения индекса ключа в массиве
func getKeyIndex(key string) int {
	// Ищем ключ в массиве
	for i, k := range apiKeys {
		if k == key {
			return i // Возвращаем индекс если нашли
		}
	}
	return -1 // Возвращаем -1 если не нашли
}

func main() {
	// Инициализируем Telegram бота
	bot, err := tgbotapi.NewBotAPI(TelegramBotToken)
	if err != nil {
		log.Panicf("Ошибка инициализации бота: %v", err) // Аварийное завершение при ошибке
	}

	bot.Debug = true                                                // Включаем режим отладки
	log.Printf("Авторизован как %s", bot.Self.UserName)             // Логируем успешную авторизацию
	log.Printf("Доступно API ключей: %d", keyManager.GetKeyCount()) // Логируем количество ключей

	// Тестируем API при запуске
	log.Printf("Тестируем подключение к OpenRouter API...")
	testResponse, err := getAIResponse("Привет", []Message{})
	if err != nil {
		log.Printf("❌ Ошибка API: %v", err) // Логируем ошибку подключения
	} else {
		log.Printf("✅ API работает: %s", testResponse) // Логируем успешный тест
	}

	// Настраиваем получение обновлений от Telegram
	u := tgbotapi.NewUpdate(0)       // offset = 0, получаем все обновления
	u.Timeout = 60                   // Таймаут long polling
	updates := bot.GetUpdatesChan(u) // Получаем канал обновлений

	// Map для хранения истории диалогов для каждого чата
	conversationHistory := make(map[int64][]Message)

	// Бесконечный цикл обработки обновлений
	for update := range updates {
		if update.Message == nil { // Игнорируем обновления без сообщений
			continue
		}

		// Обрабатываем каждое сообщение в отдельной горутине
		go func(upd tgbotapi.Update) {
			// Отправляем действие "печатает..."
			chatAction := tgbotapi.NewChatAction(upd.Message.Chat.ID, tgbotapi.ChatTyping)
			bot.Send(chatAction)

			// Обрабатываем команды и сообщения
			switch upd.Message.Text {
			case "/start":
				// Команда начала работы
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "🤖 Привет! Я бот с AI. Напиши мне что-нибудь!")
				bot.Send(msg)
				// Инициализируем историю для нового чата
				conversationHistory[upd.Message.Chat.ID] = []Message{}

			case "/clear":
				// Команда очистки истории
				conversationHistory[upd.Message.Chat.ID] = []Message{}
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID, "🗑️ История очищена!")
				bot.Send(msg)

			case "/keys":
				// Команда показа информации о ключах
				msg := tgbotapi.NewMessage(upd.Message.Chat.ID,
					fmt.Sprintf("🔑 Используется ключ #%d из %d",
						getKeyIndex(keyManager.GetCurrentKey())+1, keyManager.GetKeyCount()))
				bot.Send(msg)

			default:
				// Обработка обычных сообщений
				chatID := upd.Message.Chat.ID
				// Получаем историю диалога для текущего чата
				history, exists := conversationHistory[chatID]
				if !exists {
					// Создаем новую историю если не существует
					history = []Message{}
					conversationHistory[chatID] = history
				}

				// Получаем ответ от AI
				responseText, apiErr := getAIResponse(upd.Message.Text, history)
				if apiErr != nil {
					log.Printf("API error: %v", apiErr)           // Логируем ошибку
					responseText = "⚠️ Ошибка: " + apiErr.Error() // Формируем сообщение об ошибке
				} else {
					// Обновляем историю диалога
					conversationHistory[chatID] = append(history,
						Message{Role: "user", Content: upd.Message.Text},  // Вопрос пользователя
						Message{Role: "assistant", Content: responseText}, // Ответ AI
					)
					// Ограничиваем длину истории последними 10 сообщениями
					if len(conversationHistory[chatID]) > 10 {
						conversationHistory[chatID] = conversationHistory[chatID][len(conversationHistory[chatID])-10:]
					}
				}

				// Отправляем ответ пользователю
				msg := tgbotapi.NewMessage(chatID, responseText)
				msg.ReplyToMessageID = upd.Message.MessageID // Ответ на конкретное сообщение
				bot.Send(msg)
			}
		}(update) // Передаем update в горутину
	}
}

