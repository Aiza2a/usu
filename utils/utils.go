package utils

import (
	"encoding/json"
	"errors" // <-- 新增：匯入 errors 套件
	"io"
	"log"
	"strconv"
	"strings"

	"csz.net/tgstate/conf"
	"csz.net/tgstate/store"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TgFileData(fileName string, fileData io.Reader) tgbotapi.FileReader {
	return tgbotapi.FileReader{
		Name:   fileName,
		Reader: fileData,
	}
}

// UpDocument 上傳文件並返回 fileID, chatID, messageID
func UpDocument(fileData tgbotapi.FileReader) (string, int64, int, error) { // <-- 修改返回類型
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println(err)
		return "", 0, 0, err // <-- 返回錯誤
	}
	// Upload the file to Telegram
	params := tgbotapi.Params{
		"chat_id": conf.ChannelName, // Replace with the chat ID where you want to send the file
	}
	files := []tgbotapi.RequestFile{
		{
			Name: "document",
			Data: fileData,
		},
	}
	response, err := bot.UploadFiles("sendDocument", params, files)
	if err != nil {
		log.Println("UploadFiles error:", err) // <-- 修改：打印錯誤而不是 panic
		return "", 0, 0, err                   // <-- 返回錯誤
	}
	var msg tgbotapi.Message
	err = json.Unmarshal([]byte(response.Result), &msg) // <-- 檢查 Unmarshal 錯誤
	if err != nil {
		log.Println("Unmarshal error:", err)
		return "", 0, 0, err
	}

	// 檢查是否有 MessageID 和 Chat
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
	default: // <-- 新增：處理找不到 FileID 的情況
	    errMsg := "Telegram 響應中未找到有效的 FileID"
		log.Println(errMsg)
		return "", msg.Chat.ID, msg.MessageID, errors.New(errMsg)
	}
	
	// <-- 修改：返回 fileID, chatID, messageID 和 nil 錯誤
	return resp, msg.Chat.ID, msg.MessageID, nil
}

// GetDownloadUrl 獲取下載連結 (此函數不變)
func GetDownloadUrl(fileID string) (string, bool) {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		// log.Panic(err) // <-- 修改：避免 Panic
		log.Println("NewBotAPI error in GetDownloadUrl:", err)
		return "", false
	}
	// 使用 getFile 方法获取文件信息
	file, err := bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Println("获取文件失败【" + fileID + "】")
		log.Println(err)
		return "", false
	}
	log.Println("获取文件成功【" + fileID + "】")
	// 获取文件下载链接
	fileURL := file.Link(conf.BotToken)
	return fileURL, true
}

// BotDo 處理 Telegram Bot 的更新 (此函數不變)
func BotDo() {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println(err)
		return
	}
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updatesChan := bot.GetUpdatesChan(u)
	for update := range updatesChan {
		var msg *tgbotapi.Message
		if update.Message != nil {
			msg = update.Message
		}
		if update.ChannelPost != nil {
			msg = update.ChannelPost
		}
		if msg != nil && msg.Text == "get" && msg.ReplyToMessage != nil {
			var fileID string
			switch {
			case msg.ReplyToMessage.Document != nil && msg.ReplyToMessage.Document.FileID != "":
				fileID = msg.ReplyToMessage.Document.FileID
			case msg.ReplyToMessage.Video != nil && msg.ReplyToMessage.Video.FileID != "":
				fileID = msg.ReplyToMessage.Video.FileID
			case msg.ReplyToMessage.Sticker != nil && msg.ReplyToMessage.Sticker.FileID != "":
				fileID = msg.ReplyToMessage.Sticker.FileID
			}
			if fileID != "" {
				// 為 FileID 產生或獲取 shortID
				shortID, err := store.GenerateAndSave(fileID)
				var replyText string
				if err != nil {
					log.Printf("為 'get' 命令建立 shortID 失敗: %v", err)
					replyText = "建立短連結失敗"
				} else {
					// 檢查 BaseUrl 是否設定
					baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
					if baseUrl == "" {
						replyText = "/d/" + shortID + " (請設定 BaseUrl 以獲取完整連結)"
					} else {
						replyText = baseUrl + "/d/" + shortID
					}
				}

				newMsg := tgbotapi.NewMessage(msg.Chat.ID, replyText)
				newMsg.ReplyToMessageID = msg.MessageID
				if !strings.HasPrefix(conf.ChannelName, "@") {
					if man, err := strconv.Atoi(conf.ChannelName); err == nil && int(msg.Chat.ID) == man {
						_, err := bot.Send(newMsg) // <-- 檢查 Send 錯誤
						if err != nil {
							log.Printf("回覆 'get' 命令失敗: %v", err)
						}
					}
				} else {
					_, err := bot.Send(newMsg) // <-- 檢查 Send 錯誤
					if err != nil {
						log.Printf("回覆 'get' 命令失敗: %v", err)
					}
				}
			}
		}
	}
}

// --- 新增 EditCaption 函數 ---
// EditCaption 修改指定訊息的 Caption
func EditCaption(chatID int64, messageID int, caption string) error {
	bot, err := tgbotapi.NewBotAPI(conf.BotToken)
	if err != nil {
		log.Println("NewBotAPI error in EditCaption:", err)
		return err
	}

	editMsg := tgbotapi.NewEditMessageCaption(chatID, messageID, caption)

	_, err = bot.Send(editMsg) // 使用 Send 方法發送編輯請求
	if err != nil {
		log.Printf("修改 Caption 失敗 (ChatID: %d, MessageID: %d): %v", chatID, messageID, err)
		// 不返回錯誤，因為上傳本身是成功的，只是附加說明失敗
        // return err
	} else {
        log.Printf("成功修改 Caption (ChatID: %d, MessageID: %d)", chatID, messageID)
    }
    return nil // 即使失敗也返回 nil，避免影響主流程
}
// --- 新增結束 ---