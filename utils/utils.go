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

// TgFileData (This function is unchanged)
func TgFileData(fileName string, fileData io.Reader) tgbotapi.FileReader {
	return tgbotapi.FileReader{
		Name:   fileName,
		Reader: fileData,
	}
} // ** Closing brace for func TgFileData **

// UpDocument (This function is unchanged - ensure it returns error)
func UpDocument(fileData tgbotapi.FileReader) (string, int64, int, error) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println(err)
		return "", 0, 0, err
	} // ** Closing brace for if err != nil **
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
		log.Println("UploadFiles error:", err)
		return "", 0, 0, err
	} // ** Closing brace for if err != nil **
	var msg tgbotapi.Message
	err = json.Unmarshal([]byte(response.Result), &msg)
	if err != nil {
		log.Println("Unmarshal error:", err)
		return "", 0, 0, err
	} // ** Closing brace for if err != nil **

	if msg.MessageID == 0 || msg.Chat == nil {
		errMsg := "未能從 Telegram 響應中獲取 MessageID 或 ChatID"
		log.Println(errMsg)
		return "", 0, 0, errors.New(errMsg)
	} // ** Closing brace for if msg.MessageID == 0... **

	var resp string
	switch {
	case msg.Document != nil:
		resp = msg.Document.FileID
	case msg.Audio != nil:
		resp = msg.Audio.FileID
	case msg.Video != nil:
		resp = msg.Video.FileID
	case msg.Sticker != nil:
		resp = msg.Sticker.FileID
	default:
		errMsg := "Telegram 響應中未找到有效的 FileID"
		log.Println(errMsg)
		return "", msg.Chat.ID, msg.MessageID, errors.New(errMsg)
	} // ** Closing brace for switch **

	return resp, msg.Chat.ID, msg.MessageID, nil
} // ** Closing brace for func UpDocument **

// GetDownloadUrl (This function is unchanged)
func GetDownloadUrl(fileID string) (string, bool) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("NewBotAPI error in GetDownloadUrl:", err)
		return "", false
	} // ** Closing brace for if err != nil **
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Println("获取文件失败【" + fileID + "】")
		log.Println(err)
		return "", false
	} // ** Closing brace for if err != nil **
	log.Println("获取文件成功【" + fileID + "】")
	fileURL := file.Link(conf.BotToken)
	return fileURL, true
} // ** Closing brace for func GetDownloadUrl **

// --- BotDo Function with correct braces ---
func BotDo() {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("啟動 Bot 失敗:", err)
		return
	} // ** Closing brace for if err != nil **
	log.Println("Bot 啟動成功，正在監聽更新...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updatesChan := bot.GetUpdatesChan(u)

	// Determine target user ID if applicable
	var targetUserID int64 = 0
	isTargetUserMode := false
	if !strings.HasPrefix(conf.ChannelName, "@") {
		parsedID, parseErr := strconv.ParseInt(conf.ChannelName, 10, 64)
		if parseErr == nil {
			targetUserID = parsedID
			isTargetUserMode = true
			log.Printf("Bot 目標設定為特定用戶 ID: %d", targetUserID)
		} else { // ** Closing brace for if parseErr == nil **
			log.Printf("警告：ChannelName '%s' 不是有效的用戶 ID，將忽略來自 Bot 的直接上傳。", conf.ChannelName)
		} // ** Closing brace for else **
	} else { // ** Closing brace for if !strings.HasPrefix... **
		log.Printf("Bot 目標設定為頻道/群組: %s (將忽略來自 Bot 的直接上傳)", conf.ChannelName)
	} // ** Closing brace for else **

	for update := range updatesChan {
		var msg *tgbotapi.Message
		if update.Message != nil {
			msg = update.Message
		} else if update.ChannelPost != nil { // ** Closing brace for if update.Message... **
			msg = update.ChannelPost
		} // ** Closing brace for else if update.ChannelPost... **

		if msg == nil {
			continue
		} // ** Closing brace for if msg == nil **

		// --- Case 1: Handle 'get' command ---
		if msg.Text == "get" && msg.ReplyToMessage != nil {
			var fileID string
			switch {
			case msg.ReplyToMessage.Document != nil && msg.ReplyToMessage.Document.FileID != "":
				fileID = msg.ReplyToMessage.Document.FileID
			case msg.ReplyToMessage.Video != nil && msg.ReplyToMessage.Video.FileID != "":
				fileID = msg.ReplyToMessage.Video.FileID
			case msg.ReplyToMessage.Sticker != nil && msg.ReplyToMessage.Sticker.FileID != "":
				fileID = msg.ReplyToMessage.Sticker.FileID
			case msg.ReplyToMessage.Photo != nil && len(msg.ReplyToMessage.Photo) > 0:
				fileID = msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileID
			} // ** Closing brace for switch **

			if fileID != "" {
				// Permission check for 'get'
				allowGet := false
				if isTargetUserMode {
					if msg.From != nil && msg.From.ID == targetUserID {
						allowGet = true
					} else if msg.From != nil { // ** Closing brace for if msg.From != nil... **
						log.Printf("忽略來自非目標用戶 (%d) 的 'get' 命令", msg.From.ID)
					} else { // ** Closing brace for else if msg.From != nil **
						log.Printf("忽略來自未知用戶的 'get' 命令 (可能是頻道)")
					} // ** Closing brace for else **
				} else { // ** Closing brace for if isTargetUserMode **
					allowGet = true // Allow anyone in non-user mode
				} // ** Closing brace for else **

				if allowGet {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("為 'get' 命令建立 shortID 失敗 (FileID: %s): %v", fileID, err)
						replyText = "建立短連結失敗"
					} else { // ** Closing brace for if err != nil **
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "/d/" + shortID + " (請設定 BaseUrl 以獲取完整連結)"
						} else { // ** Closing brace for if baseUrl == "" **
							replyText = baseUrl + "/d/" + shortID
						} // ** Closing brace for else **
					} // ** Closing brace for else **
					newMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					newMsg.ReplyToMessageID = msg.MessageID
					_, errSend := bot.Send(newMsg)
					if errSend != nil {
						log.Printf("回覆 'get' 命令失敗 (ChatID: %d): %v", msg.Chat.ID, errSend)
					} // ** Closing brace for if errSend != nil **
				} // ** Closing brace for if allowGet **
			} // ** Closing brace for if fileID != "" **
		} // ** Closing brace for if msg.Text == "get" **

		// --- Case 2: Handle direct photo/document messages ---
		else if (msg.Photo != nil || msg.Document != nil) && !strings.HasPrefix(msg.Text, "/") {
			// Permission check for direct upload
			allowUpload := false
			if isTargetUserMode {
				if msg.From != nil && msg.From.ID == targetUserID {
					allowUpload = true
				} else if msg.From != nil { // ** Closing brace for if msg.From != nil... **
					log.Printf("忽略來自非目標用戶 (%d) 的直接上傳", msg.From.ID)
				} else { // ** Closing brace for else if msg.From != nil **
					log.Printf("忽略來自未知用戶的直接上傳 (可能是頻道)")
				} // ** Closing brace for else **
			} else { // ** Closing brace for if isTargetUserMode **
				log.Printf("忽略來自非目標用戶模式下的直接上傳 (ChatID: %d)", msg.Chat.ID)
				allowUpload = false
			} // ** Closing brace for else **

			if allowUpload {
				var fileID string
				if msg.Photo != nil && len(msg.Photo) > 0 {
					fileID = msg.Photo[len(msg.Photo)-1].FileID
					log.Printf("收到來自目標用戶 (%d) 的圖片 FileID: %s", msg.From.ID, fileID)
				} else if msg.Document != nil { // ** Closing brace for if msg.Photo... **
					fileID = msg.Document.FileID
					log.Printf("收到來自目標用戶 (%d) 的文件 FileID: %s", msg.From.ID, fileID)
				} // ** Closing brace for else if msg.Document... **

				if fileID != "" {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("為 Bot 上傳檔案建立 shortID 失敗 (FileID: %s): %v", fileID, err)
						replyText = "處理檔案失敗，無法建立短連結。"
					} else { // ** Closing brace for if err != nil **
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "處理成功，但 BaseUrl 未設定，無法生成完整連結。\nShortID: /d/" + shortID
						} else { // ** Closing brace for if baseUrl == "" **
							replyText = baseUrl + "/d/" + shortID
						} // ** Closing brace for else **
					} // ** Closing brace for else **

					newMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					newMsg.ReplyToMessageID = msg.MessageID
					_, errSend := bot.Send(newMsg)
					if errSend != nil {
						log.Printf("回覆 Bot 上傳訊息失敗 (ChatID: %d): %v", msg.Chat.ID, errSend)
					} // ** Closing brace for if errSend != nil **
				} // ** Closing brace for if fileID != "" **
			} // ** Closing brace for if allowUpload **
		} // ** Closing brace for else if (msg.Photo...) **
	} // ** Closing brace for for update := range... **
} // ** Closing brace for func BotDo **
// --- Replacement End ---

// EditCaption (This function is unchanged)
func EditCaption(chatID int64, messageID int, caption string) error {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("NewBotAPI error in EditCaption:", err)
		return err
	} // ** Closing brace for if err != nil **
	editMsg := tgbotapi.NewEditMessageCaption(chatID, messageID, caption)
	_, err = bot.Send(editMsg)
	if err != nil {
		log.Printf("修改 Caption 失敗 (ChatID: %d, MessageID: %d): %v", chatID, messageID, err)
	} else { // ** Closing brace for if err != nil **
		log.Printf("成功修改 Caption (ChatID: %d, MessageID: %d)", chatID, messageID)
	} // ** Closing brace for else **
	return nil
} // ** Closing brace for func EditCaption **
