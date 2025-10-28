package utils

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"strconv"
	"strings"

	"csz.net/tgstate/conf"
	"csz.net/tgstate/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TgFileData creates a FileReader for the Telegram Bot API.
func TgFileData(fileName string, fileData io.Reader) tgbotapi.FileReader {
	return tgbotapi.FileReader{
		Name:   fileName,
		Reader: fileData,
	}
} // End func TgFileData

// UpDocument uploads a file to the target chat and returns fileID, chatID, messageID, and error.
func UpDocument(fileData tgbotapi.FileReader) (string, int64, int, error) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("UpDocument: NewBotAPI error:", err)
		return "", 0, 0, err
	} // End if err != nil

	params := tgbotapi.Params{
		"chat_id": conf.ChannelName,
	}
	files := []tgbotapi.RequestFile{
		{
			Name: "document",
			Data: fileData,
		},
	}

	response, err := bot.UploadFiles("sendDocument", params, files)
	if err != nil {
		log.Println("UpDocument: UploadFiles error:", err)
		return "", 0, 0, err
	} // End if err != nil

	var msg tgbotapi.Message
	err = json.Unmarshal(response.Result, &msg) // Use response.Result directly
	if err != nil {
		log.Println("UpDocument: Unmarshal error:", err)
		return "", 0, 0, err
	} // End if err != nil

	if msg.MessageID == 0 || msg.Chat == nil {
		errMsg := "UpDocument: Failed to get MessageID or ChatID from Telegram response"
		log.Println(errMsg)
		return "", 0, 0, errors.New(errMsg)
	} // End if msg.MessageID == 0...

	var fileID string
	switch {
	case msg.Document != nil:
		fileID = msg.Document.FileID
	case msg.Audio != nil:
		fileID = msg.Audio.FileID
	case msg.Video != nil:
		fileID = msg.Video.FileID
	case msg.Sticker != nil:
		fileID = msg.Sticker.FileID
	// Consider adding Photo support here if needed for consistency, though UpDocument sends as Document
	// case msg.Photo != nil && len(msg.Photo) > 0:
	// 	fileID = msg.Photo[len(msg.Photo)-1].FileID
	default:
		errMsg := "UpDocument: No valid FileID found in Telegram response"
		log.Println(errMsg)
		// Still return ChatID and MessageID which might be useful for debugging
		return "", msg.Chat.ID, msg.MessageID, errors.New(errMsg)
	} // End switch

	// Success case
	return fileID, msg.Chat.ID, msg.MessageID, nil
} // End func UpDocument

// GetDownloadUrl retrieves the direct download link for a given fileID.
func GetDownloadUrl(fileID string) (string, bool) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("GetDownloadUrl: NewBotAPI error:", err)
		return "", false
	} // End if err != nil

	fileConfig := tgbotapi.FileConfig{FileID: fileID}
	file, err := bot.GetFile(fileConfig)
	if err != nil {
		log.Printf("GetDownloadUrl: GetFile failed for [%s]: %v\n", fileID, err)
		return "", false
	} // End if err != nil

	log.Printf("GetDownloadUrl: GetFile successful for [%s]\n", fileID)
	fileURL := file.Link(conf.BotToken)
	return fileURL, true
} // End func GetDownloadUrl

// BotDo listens for and processes Telegram updates.
func BotDo() {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("BotDo: Failed to start Bot:", err)
		return
	} // End if err != nil
	log.Println("BotDo: Bot started successfully, listening for updates...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updatesChan := bot.GetUpdatesChan(u)

	// Determine target user ID if ChannelName is a numeric ID
	var targetUserID int64 = 0
	isTargetUserMode := false
	if !strings.HasPrefix(conf.ChannelName, "@") && conf.ChannelName != "" {
		parsedID, parseErr := strconv.ParseInt(conf.ChannelName, 10, 64)
		if parseErr == nil {
			targetUserID = parsedID
			isTargetUserMode = true
			log.Printf("BotDo: Target is specific user ID: %d\n", targetUserID)
		} else { // End if parseErr == nil
			log.Printf("BotDo: Warning: ChannelName '%s' is not '@...' and not a valid user ID. Direct Bot uploads will be ignored.\n", conf.ChannelName)
		} // End else (parseErr != nil)
	} else { // End if !strings.HasPrefix...
		log.Printf("BotDo: Target is channel/group: %s. Direct Bot uploads will be ignored.\n", conf.ChannelName)
	} // End else (is channel/group)

	// Process updates from the channel
	for update := range updatesChan {
		var msg *tgbotapi.Message
		// Determine the relevant message object
		if update.Message != nil {
			msg = update.Message
		} else if update.ChannelPost != nil { // End if update.Message != nil
			msg = update.ChannelPost
		} // End else if update.ChannelPost != nil

		// Skip if no message object found
		if msg == nil {
			continue
		} // End if msg == nil

		// --- Case 1: Handle 'get' command ---
		if msg.Text == "get" && msg.ReplyToMessage != nil {
			var fileID string
			// Extract fileID from the replied message
			switch {
			case msg.ReplyToMessage.Document != nil && msg.ReplyToMessage.Document.FileID != "":
				fileID = msg.ReplyToMessage.Document.FileID
			case msg.ReplyToMessage.Video != nil && msg.ReplyToMessage.Video.FileID != "":
				fileID = msg.ReplyToMessage.Video.FileID
			case msg.ReplyToMessage.Sticker != nil && msg.ReplyToMessage.Sticker.FileID != "":
				fileID = msg.ReplyToMessage.Sticker.FileID
			case msg.ReplyToMessage.Audio != nil && msg.ReplyToMessage.Audio.FileID != "": // Added Audio
				fileID = msg.ReplyToMessage.Audio.FileID
			case msg.ReplyToMessage.Photo != nil && len(msg.ReplyToMessage.Photo) > 0:
				// Get the largest photo size
				fileID = msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileID
			} // End switch

			if fileID != "" {
				// Permission Check: Only target user can use 'get' in user mode
				allowGet := !isTargetUserMode // Allow if not user mode
				if isTargetUserMode {
					if msg.From != nil && msg.From.ID == targetUserID {
						allowGet = true
					} else { // End if msg.From != nil...
						log.Printf("BotDo: Ignoring 'get' command from non-target user (%v)\n", msg.From)
					} // End else (user mismatch)
				} // End if isTargetUserMode

				if allowGet {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("BotDo: Failed to create shortID for 'get' (FileID: %s): %v\n", fileID, err)
						replyText = "建立短連結失敗"
					} else { // End if err != nil
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "/d/" + shortID + " (請設定 BaseUrl 以獲取完整連結)"
						} else { // End if baseUrl == ""
							replyText = baseUrl + "/d/" + shortID
						} // End else (baseUrl != "")
					} // End else (err == nil)

					// Prepare and send reply
					replyMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					replyMsg.ReplyToMessageID = msg.MessageID
					_, errSend := bot.Send(replyMsg)
					if errSend != nil {
						log.Printf("BotDo: Failed to reply to 'get' command (ChatID: %d): %v\n", msg.Chat.ID, errSend)
					} // End if errSend != nil
				} // End if allowGet
			} // End if fileID != ""
		} // End if msg.Text == "get"

		// --- Case 2: Handle direct photo/document messages (excluding commands) ---
		else if (msg.Photo != nil || msg.Document != nil) && !strings.HasPrefix(msg.Text, "/") {
			// Permission Check: Only target user can upload directly via Bot
			allowUpload := false
			if isTargetUserMode {
				if msg.From != nil && msg.From.ID == targetUserID {
					allowUpload = true
				} else { // End if msg.From != nil...
					log.Printf("BotDo: Ignoring direct upload from non-target user (%v)\n", msg.From)
				} // End else (user mismatch)
			} else { // End if isTargetUserMode
				// Ignore direct uploads if target is not a specific user ID
				log.Printf("BotDo: Ignoring direct upload in non-user mode (ChatID: %d)\n", msg.Chat.ID)
			} // End else (not user mode)

			if allowUpload {
				var fileID string
				// Extract fileID from photo or document
				if msg.Photo != nil && len(msg.Photo) > 0 {
					fileID = msg.Photo[len(msg.Photo)-1].FileID // Largest photo
					log.Printf("BotDo: Received photo from target user (%d), FileID: %s\n", msg.From.ID, fileID)
				} else if msg.Document != nil { // End if msg.Photo != nil...
					fileID = msg.Document.FileID
					log.Printf("BotDo: Received document from target user (%d), FileID: %s\n", msg.From.ID, fileID)
				} // End else if msg.Document != nil

				if fileID != "" {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("BotDo: Failed to create shortID for direct upload (FileID: %s): %v\n", fileID, err)
						replyText = "處理檔案失敗，無法建立短連結。"
					} else { // End if err != nil
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "處理成功，但 BaseUrl 未設定，無法生成完整連結。\nShortID: /d/" + shortID
						} else { // End if baseUrl == ""
							replyText = baseUrl + "/d/" + shortID
						} // End else (baseUrl != "")
					} // End else (err == nil)

					// Prepare and send reply
					replyMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					replyMsg.ReplyToMessageID = msg.MessageID // Reply to the original message
					_, errSend := bot.Send(replyMsg)
					if errSend != nil {
						log.Printf("BotDo: Failed to reply to direct upload (ChatID: %d): %v\n", msg.Chat.ID, errSend)
					} // End if errSend != nil
				} // End if fileID != ""
			} // End if allowUpload
		} // End else if (msg.Photo...)

		// --- Add more 'else if' blocks here to handle other commands or message types ---

	} // End for update := range updatesChan
} // End func BotDo

// EditCaption adds or modifies the caption of a specific message.
func EditCaption(chatID int64, messageID int, caption string) error {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("EditCaption: NewBotAPI error:", err)
		return err
	} // End if err != nil

	editMsg := tgbotapi.NewEditMessageCaption(chatID, messageID, caption)
	// Optionally add ParseMode if your caption uses Markdown or HTML
	// editMsg.ParseMode = tgbotapi.ModeMarkdown // or tgbotapi.ModeHTML

	_, err = bot.Send(editMsg)
	if err != nil {
		// Log the error but don't necessarily treat it as fatal for the upload process
		log.Printf("EditCaption: Failed to edit caption (ChatID: %d, MessageID: %d): %v\n", chatID, messageID, err)
		// Consider specific error handling, e.g., if caption is identical, Telegram might return an error
	} else { // End if err != nil
		log.Printf("EditCaption: Successfully edited caption (ChatID: %d, MessageID: %d)\n", chatID, messageID)
	} // End else (err == nil)
	return nil // Return nil even if editing fails, as upload succeeded
} // End func EditCaption
