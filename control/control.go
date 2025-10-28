package control

import (
	"bytes" 
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	// "strconv" // 確保 strconv 沒有被意外地取消註解
	"strings"
	"time"

	"csz.net/tgstate/assets"
	"csz.net/tgstate/conf"
	"csz.net/tgstate/store"
	"csz.net/tgstate/utils"
)

// UploadImageAPI (此函數不變)
func UploadImageAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodPost {
		file, header, err := r.FormFile("image")
		if err != nil {
			errJsonMsg("Unable to get file", w)
			return
		}
		defer file.Close()

		if conf.Mode != "p" && r.ContentLength > 20*1024*1024 {
			errJsonMsg("File size exceeds 20MB limit", w)
			return
		}
		allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4", ".mov", ".avi"}
		ext := strings.ToLower(filepath.Ext(header.Filename))
		valid := false
		for _, allowedExt := range allowedExts {
			if ext == allowedExt {
				valid = true
				break
			}
		}
		isDriveMode := conf.Mode == "p"
		if !isDriveMode && !valid {
			errJsonMsg("檔案類型無效。僅允許圖片和常見影片格式。", w)
			return
		}

		res := conf.UploadResponse{
			Code:    0,
			Message: "error",
		}

		realFileID, chatID, messageID, uploadErr := utils.UpDocument(utils.TgFileData(header.Filename, file))

		if uploadErr != nil {
			log.Printf("上傳文件到 Telegram 失敗: %v", uploadErr)
			errMsg := "Failed to upload file to Telegram"
			if strings.Contains(uploadErr.Error(), "wrong file identifier") {
				errMsg = "Telegram error: Invalid file identifier or file too big."
			} else if strings.Contains(uploadErr.Error(), "timeout") {
				errMsg = "Telegram timeout during upload."
			}
			errJsonMsg(errMsg, w)
			return
		}
		
		if realFileID == "" {
		    log.Println("從 UpDocument 獲取的 realFileID 為空")
		    errJsonMsg("Failed to get file ID from Telegram after upload", w)
		    return
		}


		shortID, err := store.GenerateAndSave(realFileID)
		if err != nil {
			log.Printf("無法建立 short ID: %v", err)
			errJsonMsg("Failed to create short ID", w)
			return
		}

		fullShortLinkURL := ""
		baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
		if baseUrl != "" {
			fullShortLinkURL = baseUrl + conf.FileRoute + shortID
		} else {
			log.Println("警告：BaseUrl 未設定，無法生成完整連結用於 Caption")
		}

		if fullShortLinkURL != "" && chatID != 0 && messageID != 0 {
			go func() {
				err := utils.EditCaption(chatID, messageID, fullShortLinkURL)
				if err != nil {
					log.Printf("編輯 Caption 時發生非致命錯誤: %v", err)
				}
			}()
		} else {
            log.Printf("跳過編輯 Caption (BaseUrl: '%s', ChatID: %d, MessageID: %d)", conf.BaseUrl, chatID, messageID)
        }

		img := conf.FileRoute + shortID
		res = conf.UploadResponse{
			Code:    1,
			Message: img,                           
			ImgUrl:  fullShortLinkURL,             
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
		return
	}
	http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
}

// errJsonMsg (此函數不變)
func errJsonMsg(msg string, w http.ResponseWriter) {
	response := conf.UploadResponse{
		Code:    0,
		Message: msg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) 
	json.NewEncoder(w).Encode(response)
}

// D (此函數不變)
func D(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	shortID := strings.TrimPrefix(path, conf.FileRoute)
	if shortID == "" {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 Not Found"))
		return
	}

	realFileID, err := store.GetFileID(shortID)
	if err != nil {
		log.Printf("查詢 FileID 失敗 (shortID: %s): %v", shortID, err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("404 Not Found: Invalid ID"))
		return
	}

	fileUrl, ok := utils.GetDownloadUrl(realFileID)
	if !ok { 
		http.Error(w, "Failed to get download URL from Telegram", http.StatusInternalServerError)
		return
	}

	resp, err := http.Get(fileUrl)
	if err != nil {
		log.Printf("從 Telegram 下載檔案失敗 (URL: %s): %v", fileUrl, err)
		http.Error(w, "Failed to fetch content from Telegram", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close() 

	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram 返回非 200 狀態碼: %d (URL: %s)", resp.StatusCode, fileUrl)
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Println("Telegram 錯誤響應:", string(bodyBytes))
		
		if resp.StatusCode == http.StatusNotFound {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 Not Found: File expired or invalid on Telegram"))
		} else {
			http.Error(w, "Failed to fetch content from Telegram", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Disposition", "inline")
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && strings.Contains(contentType, "text/html") {
			log.Printf("警告：Telegram 返回了 HTML Content-Type，可能是錯誤頁面 (URL: %s)", fileUrl)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 Not Found or Access Denied on Telegram"))
			return
	}

	peekBuffer := make([]byte, 12)
	nPeek, errPeek := io.ReadFull(resp.Body, peekBuffer)
	combinedReader := io.MultiReader(bytes.NewReader(peekBuffer[:nPeek]), resp.Body)
	
	isBlob := false
	if errPeek == nil && string(peekBuffer) == "tgstate-blob" {
		isBlob = true
	} else if errPeek != nil && errPeek != io.ErrUnexpectedEOF && errPeek != io.EOF {
		log.Println("讀取響應標記時發生錯誤:", errPeek)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	if isBlob {
		blobContentBytes, errReadBlob := io.ReadAll(combinedReader)
		if errReadBlob != nil {
			log.Println("讀取分塊標記檔案內容時發生錯誤:", errReadBlob)
			http.Error(w, "Failed to read blob metadata", http.StatusInternalServerError)
			return
		}
		content := string(blobContentBytes)
		lines := strings.Split(content, "\n")
		if len(lines) < 3 {
			log.Println("分塊標記檔案格式錯誤:", content)
			http.Error(w, "Invalid blob metadata format", http.StatusInternalServerError)
			return
		}

		log.Println("分塊文件:" + lines[1])
		var fileSize string
		var startLine = 2
		if strings.HasPrefix(lines[2], "size") {
			fileSize = lines[2][len("size"):]
			startLine = startLine + 1
		}

		w.Header().Set("Content-Type", "application/octet-stream") 
		w.Header().Set("Content-Disposition", "attachment; filename=\""+lines[1]+"\"") 
		if fileSize != "" {
			w.Header().Set("Content-Length", fileSize) 
		} else {
			w.Header().Del("Content-Length")
		}

		for i := startLine; i < len(lines); i++ {
			chunkShortID := strings.TrimSpace(lines[i]) 
			if chunkShortID == "" {
				continue
			}

			chunkRealFileID, err := store.GetFileID(chunkShortID)
			if err != nil {
				log.Printf("找不到分塊 FileID (shortID: %s): %v", chunkShortID, err)
				http.Error(w, "Failed to fetch content (invalid chunk ID)", http.StatusInternalServerError)
				return 
			}
			
			fileStatus := false
			var chunkUrl string
			var reTry = 0
			for !fileStatus && reTry < 3 { 
				if reTry > 0 {
					log.Printf("重試獲取分塊下載連結 (ShortID: %s, RealID: %s, Retry: %d)", chunkShortID, chunkRealFileID, reTry)
					time.Sleep(time.Duration(reTry*2) * time.Second) 
				}
				reTry = reTry + 1
				chunkUrl, fileStatus = utils.GetDownloadUrl(chunkRealFileID)
			}

			if !fileStatus {
				log.Printf("多次重試後仍無法獲取分塊下載連結 (ShortID: %s, RealID: %s)", chunkShortID, chunkRealFileID)
				http.Error(w, "Failed to get chunk download URL", http.StatusInternalServerError)
				return 
			}

			blobResp, err := http.Get(chunkUrl)
			if err != nil {
				log.Printf("下載分塊失敗 (URL: %s): %v", chunkUrl, err)
				http.Error(w, "Failed to fetch chunk content", http.StatusInternalServerError)
				return 
			}
			
			if blobResp.StatusCode != http.StatusOK {
				blobResp.Body.Close() 
				log.Printf("下載分塊時 Telegram 返回非 200 狀態碼: %d (URL: %s)", blobResp.StatusCode, chunkUrl)
				http.Error(w, "Failed to fetch chunk content from Telegram", http.StatusInternalServerError)
				return 
			}

			_, err = io.Copy(w, blobResp.Body)
			blobResp.Body.Close() 
			if err != nil {
				log.Println("寫入分塊響應數據時發生錯誤:", err)
				return 
			}
		}
	} else {
		finalContentType := contentType 
		if finalContentType == "" || finalContentType == "application/octet-stream"{
			finalContentType = http.DetectContentType(peekBuffer[:nPeek])
		}
		w.Header().Set("Content-Type", finalContentType)

		_, errWritePeek := w.Write(peekBuffer[:nPeek])
        if errWritePeek != nil {
            log.Println("寫入 peek buffer 時發生錯誤:", errWritePeek)
            return
        }

		_, errCopyRest := io.Copy(w, resp.Body) 
		if errCopyRest != nil {
			log.Println("複製剩餘響應數據時發生錯誤:", errCopyRest)
			return
		}
	}
}

// Index (此函數不變)
func Index(w http.ResponseWriter, r *http.Request) {
	htmlPath := "templates/images.tmpl"
	if conf.Mode == "p" {
		htmlPath = "templates/files.tmpl"
	}
	file, err := assets.Templates.ReadFile(htmlPath)
	if err != nil {
		http.Error(w, "HTML file not found", http.StatusNotFound)
		return
	}
	headerFile, err := assets.Templates.ReadFile("templates/header.tmpl")
	if err != nil {
		http.Error(w, "Header template not found", http.StatusNotFound)
		return
	}
	footerFile, err := assets.Templates.ReadFile("templates/footer.tmpl")
	if err != nil {
		http.Error(w, "Footer template not found", http.StatusNotFound)
		return
	}
	tmpl := template.New("html")
	tmpl, err = tmpl.Parse(string(headerFile))
	if err != nil {
		http.Error(w, "Error parsing header template", http.StatusInternalServerError)
		return
	}
	tmpl, err = tmpl.Parse(string(file))
	if err != nil {
		http.Error(w, "Error parsing HTML template", http.StatusInternalServerError)
		return
	}
	tmpl, err = tmpl.Parse(string(footerFile))
	if err != nil {
		http.Error(w, "Error parsing footer template", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, "Error rendering HTML template", http.StatusInternalServerError)
	}
}

// Pwd (此函數不變)
func Pwd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		file, err := assets.Templates.ReadFile("templates/pwd.tmpl")
		if err != nil {
			http.Error(w, "HTML file not found", http.StatusNotFound)
			return
		}
		headerFile, err := assets.Templates.ReadFile("templates/header.tmpl")
		if err != nil {
			http.Error(w, "Header template not found", http.StatusNotFound)
			return
		}
		tmpl := template.New("html")
		if tmpl, err = tmpl.Parse(string(headerFile)); err != nil {
			http.Error(w, "Error parsing Header template", http.StatusInternalServerError)
			return
		}
		if tmpl, err = tmpl.Parse(string(file)); err != nil {
			http.Error(w, "Error parsing File template", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if err := tmpl.Execute(w, nil); err != nil {
			http.Error(w, "Error rendering HTML template", http.StatusInternalServerError)
		}
		return
	}
	cookie := http.Cookie{
		Name:  "p",
		Value: r.FormValue("p"),
	    HttpOnly: true, 
        Secure:   r.TLS != nil, 
        SameSite: http.SameSiteLaxMode, 
        Path:     "/",
	}
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- 修改 Middleware ---
func Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 只有當密碼設定了才需要檢查
		if conf.Pass != "" && conf.Pass != "none" {
			
			// 先檢查 Cookie (適用於所有路徑，包括 /api)
			cookie, err := r.Cookie("p")
			isAuthenticatedByCookie := err == nil && cookie.Value == conf.Pass
			
			// 如果是 API 路徑
			if strings.HasPrefix(r.URL.Path, "/api") {
				// 檢查 URL 中的 pass 參數
				isAuthenticatedByParam := r.URL.Query().Get("pass") == conf.Pass
				
				// 如果 Cookie 或 URL 參數任一驗證通過，則繼續
				if isAuthenticatedByCookie || isAuthenticatedByParam {
					next(w, r)
					return
				} else {
					// API 請求且兩種驗證都失敗
					w.WriteHeader(http.StatusUnauthorized)
					errJsonMsg("Unauthorized: Invalid or missing password parameter/cookie for API", w)
					return
				}
			} else { // 如果不是 API 路徑 (例如 /, /pwd 等)
				// 只檢查 Cookie
				if !isAuthenticatedByCookie {
					// 如果是請求 /pwd 頁面本身，允許訪問以顯示登入框
					if r.URL.Path == "/pwd" {
						next(w, r)
						return
					}
					// 其他頁面，重定向到密碼頁面
					http.Redirect(w, r, "/pwd", http.StatusSeeOther)
					return
				}
				// Cookie 驗證通過，繼續訪問網頁
				next(w, r)
				return
			}
		} else { // 如果沒有設定密碼，直接繼續
			next(w, r)
		}
	}
}
// --- 修改結束 ---