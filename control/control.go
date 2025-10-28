package control

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"csz.net/tgstate/assets"
	"csz.net/tgstate/conf"
	"csz.net/tgstate/store"
	"csz.net/tgstate/utils"
)

// UploadImageAPI 上传图片api
func UploadImageAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodPost {
		file, header, err := r.FormFile("image")
		if err != nil {
			errJsonMsg("Unable to get file", w)
			return
		}
		defer file.Close()

		// --- 檢查大小和類型 (不變) ---
		if conf.Mode != "p" && r.ContentLength > 20*1024*1024 {
			errJsonMsg("File size exceeds 20MB limit", w)
			return
		}
		allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4", ".mov", ".avi"} // <-- 稍微放寬一點常見類型
		ext := strings.ToLower(filepath.Ext(header.Filename)) // <-- 轉小寫比較
		valid := false
		for _, allowedExt := range allowedExts {
			if ext == allowedExt {
				valid = true
				break
			}
		}
		// 如果是網盤模式 (p)，允許所有類型
		isDriveMode := conf.Mode == "p"
		if !isDriveMode && !valid {
			errJsonMsg("檔案類型無效。僅允許圖片和常見影片格式。", w)
			return
		}
		// --- 檢查結束 ---

		res := conf.UploadResponse{
			Code:    0,
			Message: "error",
		}

		// --- 修改流程：先上傳，再編輯 Caption ---
		// 1. 上傳文件到 Telegram 並獲取 realFileID, chatID, messageID
		realFileID, chatID, messageID, uploadErr := utils.UpDocument(utils.TgFileData(header.Filename, file))

		if uploadErr != nil {
			log.Printf("上傳文件到 Telegram 失敗: %v", uploadErr)
			// 根據錯誤類型決定返回給用戶的訊息
			errMsg := "Failed to upload file to Telegram"
			if strings.Contains(uploadErr.Error(), "wrong file identifier") { // 示例：捕捉特定TG錯誤
				errMsg = "Telegram error: Invalid file identifier or file too big."
			} else if strings.Contains(uploadErr.Error(), "timeout") {
				errMsg = "Telegram timeout during upload."
			}
			errJsonMsg(errMsg, w)
			return
		}
		
		// 如果 realFileID 為空（即使 uploadErr 是 nil，也可能發生）
		if realFileID == "" {
		    log.Println("從 UpDocument 獲取的 realFileID 為空")
		    errJsonMsg("Failed to get file ID from Telegram after upload", w)
		    return
		}


		// 2. 為這個 FileID 產生或獲取一個短 ID
		shortID, err := store.GenerateAndSave(realFileID)
		if err != nil {
			log.Printf("無法建立 short ID: %v", err)
			// 即使無法生成短ID，上傳本身是成功的，可以考慮只返回 FileID 或錯誤
			// 這裡我們還是報錯，因為短連結是核心功能
			errJsonMsg("Failed to create short ID", w)
			return
		}

		// 3. 構造完整的短連結 URL (確保 BaseUrl 已設定)
		fullShortLinkURL := ""
		baseUrl := strings.TrimSuffix(conf.BaseUrl, "/")
		if baseUrl != "" {
			fullShortLinkURL = baseUrl + conf.FileRoute + shortID
		} else {
			log.Println("警告：BaseUrl 未設定，無法生成完整連結用於 Caption")
			// 如果 BaseUrl 沒設定，Caption 就留空或只放 shortID
            // fullShortLinkURL = conf.FileRoute + shortID // 或者留空 ""
		}

		// 4. 呼叫 EditCaption 來添加連結 (僅當有完整連結時)
		if fullShortLinkURL != "" && chatID != 0 && messageID != 0 {
			// 在 goroutine 中執行，避免阻塞主請求
			go func() {
				err := utils.EditCaption(chatID, messageID, fullShortLinkURL)
				if err != nil {
					// 記錄錯誤，但不影響給用戶的返回
					log.Printf("編輯 Caption 時發生非致命錯誤: %v", err)
				}
			}()
		} else {
            log.Printf("跳過編輯 Caption (BaseUrl: '%s', ChatID: %d, MessageID: %d)", conf.BaseUrl, chatID, messageID)
        }


		// 5. 使用 shortID 構造返回給用戶的相對路徑
		img := conf.FileRoute + shortID
		res = conf.UploadResponse{
			Code:    1,
			Message: img,                           // 返回相對路徑
			ImgUrl:  fullShortLinkURL,             // 返回完整 URL (如果有的話)
		}
		// --- 修改結束 ---

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
		return
	}

	// 如果不是POST请求，返回错误响应
	http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
}
func errJsonMsg(msg string, w http.ResponseWriter) {
	response := conf.UploadResponse{
		Code:    0,
		Message: msg,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // 即使是錯誤，也可能返回 200 OK，讓前端能解析 JSON
	json.NewEncoder(w).Encode(response)
}
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
	if !ok { // <-- 檢查 GetDownloadUrl 是否成功
		http.Error(w, "Failed to get download URL from Telegram", http.StatusInternalServerError)
		return
	}

	resp, err := http.Get(fileUrl)
	if err != nil {
		log.Printf("從 Telegram 下載檔案失敗 (URL: %s): %v", fileUrl, err)
		http.Error(w, "Failed to fetch content from Telegram", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close() // <-- 確保 Body 被關閉

	// --- 檢查 Telegram 返回的狀態碼 ---
	if resp.StatusCode != http.StatusOK {
		log.Printf("Telegram 返回非 200 狀態碼: %d (URL: %s)", resp.StatusCode, fileUrl)
		// 嘗試讀取可能的錯誤訊息
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Println("Telegram 錯誤響應:", string(bodyBytes))
		
		// 根據狀態碼返回不同的錯誤
		if resp.StatusCode == http.StatusNotFound {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 Not Found: File expired or invalid on Telegram"))
		} else {
			http.Error(w, "Failed to fetch content from Telegram", http.StatusInternalServerError)
		}
		return
	}
	// --- 檢查結束 ---


	w.Header().Set("Content-Disposition", "inline")
	// -- 放寬 Content-Type 檢查 --
	// 舊檢查: if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/octet-stream")
	// TG 可能返回具體的 MIME 類型，例如 image/jpeg, video/mp4 等
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" { // 如果 TG 沒返回 Content-Type，嘗試猜測
		// 因為我們需要先讀取一部分來判斷是否是分塊，這裡先不設定
	} else {
		// 只要不是明顯的錯誤頁面類型，都先接受
		if strings.Contains(contentType, "text/html") {
			log.Printf("警告：Telegram 返回了 HTML Content-Type，可能是錯誤頁面 (URL: %s)", fileUrl)
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 Not Found or Access Denied on Telegram"))
			return
		}
		// 其他類型，先透傳給客戶端
		// w.Header().Set("Content-Type", contentType) // <-- 後面會根據是否分塊重新設定
	}
	// -- 檢查結束 --


	// 嘗試讀取第一部分來判斷是否是分塊標記
	peekBuffer := make([]byte, 12) // 只讀取標記長度
	nPeek, errPeek := io.ReadFull(resp.Body, peekBuffer)

	// 將 resp.Body 包裝成一個新的 Reader，它會先返回 peekBuffer 的內容，然後再讀取原始 resp.Body 的剩餘部分
	combinedReader := io.MultiReader(bytes.NewReader(peekBuffer[:nPeek]), resp.Body)
	
	isBlob := false
	if errPeek == nil && string(peekBuffer) == "tgstate-blob" {
		isBlob = true
	} else if errPeek != nil && errPeek != io.ErrUnexpectedEOF && errPeek != io.EOF {
		// 如果在讀取標記時就發生錯誤（非正常結束）
		log.Println("讀取響應標記時發生錯誤:", errPeek)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}
	// 注意：如果檔案小於 12 字節，errPeek 會是 io.ErrUnexpectedEOF 或 io.EOF


	if isBlob {
		// 讀取整個分塊標記檔案內容
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

		// --- 設定分塊下載的 Header ---
		w.Header().Set("Content-Type", "application/octet-stream") // 強制設為二進制流
		w.Header().Set("Content-Disposition", "attachment; filename=\""+lines[1]+"\"") // 強制下載
		if fileSize != "" {
			w.Header().Set("Content-Length", fileSize) // 設置總大小（如果有的話）
		} else {
			// 如果沒有大小信息，移除 Content-Length，讓客戶端自己處理流式下載
			w.Header().Del("Content-Length")
		}
		// --- 設定結束 ---

		for i := startLine; i < len(lines); i++ {
			chunkShortID := strings.TrimSpace(lines[i]) // 使用 TrimSpace 去掉可能的空格和換行符
			if chunkShortID == "" {
				continue
			}

			chunkRealFileID, err := store.GetFileID(chunkShortID)
			if err != nil {
				log.Printf("找不到分塊 FileID (shortID: %s): %v", chunkShortID, err)
				http.Error(w, "Failed to fetch content (invalid chunk ID)", http.StatusInternalServerError)
				return // 中斷下載
			}
			
			fileStatus := false
			var chunkUrl string
			var reTry = 0
			for !fileStatus && reTry < 3 { // <-- 增加重試次數限制
				if reTry > 0 {
					log.Printf("重試獲取分塊下載連結 (ShortID: %s, RealID: %s, Retry: %d)", chunkShortID, chunkRealFileID, reTry)
					time.Sleep(time.Duration(reTry*2) * time.Second) // 增加延遲
				}
				reTry = reTry + 1
				chunkUrl, fileStatus = utils.GetDownloadUrl(chunkRealFileID)
			}

			if !fileStatus {
				log.Printf("多次重試後仍無法獲取分塊下載連結 (ShortID: %s, RealID: %s)", chunkShortID, chunkRealFileID)
				http.Error(w, "Failed to get chunk download URL", http.StatusInternalServerError)
				return // 中斷下載
			}

			blobResp, err := http.Get(chunkUrl)
			if err != nil {
				log.Printf("下載分塊失敗 (URL: %s): %v", chunkUrl, err)
				http.Error(w, "Failed to fetch chunk content", http.StatusInternalServerError)
				return // 中斷下載
			}
			
			// --- 檢查分塊下載狀態碼 ---
			if blobResp.StatusCode != http.StatusOK {
				blobResp.Body.Close() // 關閉 Body
				log.Printf("下載分塊時 Telegram 返回非 200 狀態碼: %d (URL: %s)", blobResp.StatusCode, chunkUrl)
				http.Error(w, "Failed to fetch chunk content from Telegram", http.StatusInternalServerError)
				return // 中斷下載
			}
			// --- 檢查結束 ---

			_, err = io.Copy(w, blobResp.Body)
			blobResp.Body.Close() // <-- 確保每個分塊的 Body 都被關閉
			if err != nil {
				// 這裡的錯誤可能是客戶端斷開連接等，只記錄日誌，不一定需要返回 500
				log.Println("寫入分塊響應數據時發生錯誤:", err)
				return // 中斷下載
			}
		}
	} else {
		// 不是分塊文件，直接透傳內容
		
		// 重新讀取 peekBuffer (因為它已經被消耗了)
		// combinedReader 會先返回 peekBuffer 的內容
		
		// --- 根據內容設定 Content-Type ---
		// 因為 peekBuffer 可能不夠長，不足以判斷類型，我們需要一個能緩衝的 Reader
		// 不過，更簡單的方式是直接使用 resp.Header 裡的 Content-Type (如果 TG 返回了)
		finalContentType := contentType // 使用 TG 返回的 Content-Type
		if finalContentType == "" || finalContentType == "application/octet-stream"{
			// 如果 TG 沒返回或返回通用類型，我們嘗試根據已讀的 peekBuffer 猜測
			// 注意：這裡只用了最多 12 字節，可能不準確
			finalContentType = http.DetectContentType(peekBuffer[:nPeek])
		}
		w.Header().Set("Content-Type", finalContentType)
		// --- 設定結束 ---

		// 寫回 peek 的部分
		_, errWritePeek := w.Write(peekBuffer[:nPeek])
        if errWritePeek != nil {
            log.Println("寫入 peek buffer 時發生錯誤:", errWritePeek)
            // 可能客戶端已斷開
            return
        }

		// 複製剩餘的內容
		_, errCopyRest := io.Copy(w, resp.Body) // <-- 直接用 resp.Body，因為 MultiReader 裡面的已經被消耗了
		if errCopyRest != nil {
			log.Println("複製剩餘響應數據時發生錯誤:", errCopyRest)
			// 可能客戶端已斷開
			return
		}
	}
}

// Index 首页 (此函數不變)
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

// Pwd 密碼處理 (此函數不變)
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
	    HttpOnly: true, // <-- 增加安全性
        Secure:   true, // <-- 僅限 HTTPS
        SameSite: http.SameSiteLaxMode, // <-- CSRF 防護
        Path:     "/",
	}
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Middleware 密碼中間件 (此函數不變)
func Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if conf.Pass != "" && conf.Pass != "none" {
			// API 路徑檢查 pass 參數
			if strings.HasPrefix(r.URL.Path, "/api") {
				if r.URL.Query().Get("pass") == conf.Pass {
					next(w, r) // API 密碼正確，繼續處理
					return
				}
				// API 密碼錯誤或未提供
				w.WriteHeader(http.StatusUnauthorized)
				errJsonMsg("Unauthorized: Invalid or missing password parameter for API", w)
				return
			}
			
			// 非 API 路徑檢查 Cookie
			cookie, err := r.Cookie("p")
			if err != nil || cookie.Value != conf.Pass {
				// 如果是請求 /pwd 頁面本身，則允許訪問
				if r.URL.Path == "/pwd" {
					next(w,r)
					return
				}
				// 否則重定向到密碼頁面
				http.Redirect(w, r, "/pwd", http.StatusSeeOther)
				return
			}
		}
		// 不需要密碼或密碼驗證通過
		next(w, r)
	}
}