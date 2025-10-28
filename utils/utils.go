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

// TgFileData (此函數不變)
func TgFileData(fileName string, fileData io.Reader) tgbotapi.FileReader {
	return tgbotapi.FileReader{
		Name:   fileName,
		Reader: fileData,
	}
}

// UpDocument (此函數不變)
func UpDocument(fileData tgbotapi.FileReader) (string, int64, int, error) { 
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println(err)
		return "", 0, 0, err 
	}
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
	}
	var msg tgbotapi.Message
	err = json.Unmarshal([]byte(response.Result), &msg) 
	if err != nil {
		log.Println("Unmarshal error:", err)
		return "", 0, 0, err
	}

	if msg.MessageID == 0 || msg.Chat == nil {
		errMsg := "未能從 Telegram 響應中獲取 MessageID 或 ChatID"
		log.Println(errMsg)
		return "", 0, 0, errors.New(errMsg)
	}


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
	}
	
	return resp, msg.Chat.ID, msg.MessageID, nil
}

// GetDownloadUrl (此函數不變)
func GetDownloadUrl(fileID string) (string, bool) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("NewBotAPI error in GetDownloadUrl:", err)
		return "", false
	}
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Println("获取文件失败【" + fileID + "】")
		log.Println(err)
		return "", false
	}
	log.Println("获取文件成功【" + fileID + "】")
	fileURL := file.Link(conf.BotToken)
	return fileURL, true
}


// --- 修改 BotDo 函數 ---
func BotDo() {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("啟動 Bot 失敗:", err) // <-- 增加啟動失敗日誌
		return
	}
	log.Println("Bot 啟動成功，正在監聽更新...") // <-- 增加啟動成功日誌

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updatesChan := bot.GetUpdatesChan(u)

	// --- 獲取目標 Chat ID (如果是數字 ID) ---
	var targetUserID int64 = 0
	isTargetUserMode := false
	if !strings.HasPrefix(conf.ChannelName, "@") {
		parsedID, parseErr := strconv.ParseInt(conf.ChannelName, 10, 64)
		if parseErr == nil {
			targetUserID = parsedID
			isTargetUserMode = true
			log.Printf("Bot 目標設定為特定用戶 ID: %d", targetUserID)
		} else {
             log.Printf("警告：ChannelName '%s' 不是有效的用戶 ID，將允許來自任何人的 Bot 上傳。", conf.ChannelName)
        }
	} else {
         log.Printf("Bot 目標設定為頻道/群組: %s (注意：目前 Bot 上傳功能對目標用戶模式更友好)", conf.ChannelName)
    }
	// --- 獲取結束 ---


	for update := range updatesChan {
		var msg *tgbotapi.Message
		// 優先處理 edited_message 或 edited_channel_post (如果需要的話)
        // if update.EditedMessage != nil { msg = update.EditedMessage }
        // else if update.EditedChannelPost != nil { msg = update.EditedChannelPost }
        if update.Message != nil { msg = update.Message }
        else if update.ChannelPost != nil { msg = update.ChannelPost }
        
        // 如果訊息為空，跳過本次更新
		if msg == nil {
			continue
		}

		// --- 情況一：處理 'get' 回覆命令 (邏輯不變) ---
		if msg.Text == "get" && msg.ReplyToMessage != nil {
			var fileID string
			// ... (提取 fileID 的 switch 邏輯不變) ...
			switch {
			case msg.ReplyToMessage.Document != nil && msg.ReplyToMessage.Document.FileID != "":
				fileID = msg.ReplyToMessage.Document.FileID
			case msg.ReplyToMessage.Video != nil && msg.ReplyToMessage.Video.FileID != "":
				fileID = msg.ReplyToMessage.Video.FileID
			case msg.ReplyToMessage.Sticker != nil && msg.ReplyToMessage.Sticker.FileID != "":
				fileID = msg.ReplyToMessage.Sticker.FileID
			case msg.ReplyToMessage.Photo != nil && len(msg.ReplyToMessage.Photo) > 0: // <-- 也允許 get 圖片
				fileID = msg.ReplyToMessage.Photo[len(msg.ReplyToMessage.Photo)-1].FileID
			}


			if fileID != "" {
				// --- 權限檢查：只有目標用戶才能使用 get 命令 ---
				allowGet := false
				if isTargetUserMode {
					// From 可能為 nil (例如來自頻道)
					if msg.From != nil && msg.From.ID == targetUserID { 
						allowGet = true
					} else {
                         log.Printf("忽略來自非目標用戶 (%d) 的 'get' 命令", msg.From.ID)
                    }
				} else {
					allowGet = true // 非用戶模式下允許任何人用 get
				}
				// --- 權限檢查結束 ---

				if allowGet {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("為 'get' 命令建立 shortID 失敗 (FileID: %s): %v", fileID, err)
						replyText = "建立短連結失敗"
					} else {
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "/d/" + shortID + " (請設定 BaseUrl 以獲取完整連結)"
						} else {
							replyText = baseUrl + "/d/" + shortID
						}
					}
					newMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					newMsg.ReplyToMessageID = msg.MessageID
					_, errSend := bot.Send(newMsg)
					if errSend != nil {
						log.Printf("回覆 'get' 命令失敗 (ChatID: %d): %v", msg.Chat.ID, errSend)
					}
				}
			}
		} 
		// --- 情況二：處理直接發送的圖片或文件 ---
		//    (排除是 '/start', '/help' 等 Bot 命令的情況)
		else if (msg.Photo != nil || msg.Document != nil) && !strings.HasPrefix(msg.Text, "/") { 
			// --- 權限檢查：只允許目標用戶直接上傳 ---
			allowUpload := false
			if isTargetUserMode {
				// From 可能為 nil (例如來自頻道)
				if msg.From != nil && msg.From.ID == targetUserID {
					allowUpload = true
				} else {
                    log.Printf("忽略來自非目標用戶 (%d) 的直接上傳", msg.From.ID)
                }
			} else {
                // 非用戶模式下，目前不允許任何人直接透過 Bot 上傳，避免濫用
                // 如果你需要在群組/頻道中允許，需要更複雜的邏輯來驗證成員身份或 Bot 權限
                log.Printf("忽略來自非目標用戶模式下的直接上傳 (ChatID: %d)", msg.Chat.ID)
				allowUpload = false 
			}
			// --- 權限檢查結束 ---

			if allowUpload {
				var fileID string
				// var fileName string // (目前沒用到檔名)

				if msg.Photo != nil && len(msg.Photo) > 0 {
					fileID = msg.Photo[len(msg.Photo)-1].FileID
					// fileName = "photo.jpg" 
					log.Printf("收到來自目標用戶 (%d) 的圖片 FileID: %s", msg.From.ID, fileID)
				} else if msg.Document != nil {
					fileID = msg.Document.FileID
					// fileName = msg.Document.FileName
					log.Printf("收到來自目標用戶 (%d) 的文件 FileID: %s", msg.From.ID, fileID)
				}

				if fileID != "" {
					shortID, err := store.GenerateAndSave(fileID)
					var replyText string
					if err != nil {
						log.Printf("為 Bot 上傳檔案建立 shortID 失敗 (FileID: %s): %v", fileID, err)
						replyText = "處理檔案失敗，無法建立短連結。"
					} else {
						baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
						if baseUrl == "" {
							replyText = "處理成功，但 BaseUrl 未設定，無法生成完整連結。\nShortID: /d/" + shortID
						} else {
							replyText = baseUrl + "/d/" + shortID
						}
					}

					newMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
					newMsg.ReplyToMessageID = msg.MessageID // 回覆包含圖片/文件的訊息
					_, errSend := bot.Send(newMsg)
					if errSend != nil {
						log.Printf("回覆 Bot 上傳訊息失敗 (ChatID: %d): %v", msg.Chat.ID, errSend)
					}
				}
			}
		}
		// --- 可以繼續添加 else if 來處理其他類型的訊息或命令 ---

	} // end for loop
}
// --- 修改結束 ---


// EditCaption (此函數不變)
func EditCaption(chatID int64, messageID int, caption string) error {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("NewBotAPI error in EditCaption:", err)
		return err
	}
	editMsg := tgbotapi.NewEditMessageCaption(chatID, messageID, caption)
	_, err = bot.Send(editMsg) 
	if err != nil {
		log.Printf("修改 Caption 失敗 (ChatID: %d, MessageID: %d): %v", chatID, messageID, err)
	} else {
        log.Printf("成功修改 Caption (ChatID: %d, MessageID: %d)", chatID, messageID)
    }
    return nil 
} 
