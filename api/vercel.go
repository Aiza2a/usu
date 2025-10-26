package api

import (
	"net/http"
	"os"
	"strings"
	"sync" // 1. 匯入 sync 套件

	"csz.net/tgstate/conf"
	"csz.net/tgstate/control"
	"csz.net/tgstate/store" // 2. 匯入 store 套件
)

// 3. 建立一個 once 變數，確保資料庫初始化只執行一次
var once sync.Once

// 4. 建立一個初始化函式
func initDB() {
	// 將資料庫檔案放在 Vercel 唯一可寫的 /tmp 目錄下
	store.InitDB("/tmp/tgstate.db")
}

func Vercel(w http.ResponseWriter, r *http.Request) {
	// 5. 在處理請求的最開始，執行一次初始化
	once.Do(initDB)

	conf.BotToken = os.Getenv("token")
	conf.ChannelName = os.Getenv("target")
	conf.Pass = os.Getenv("pass")
	conf.Mode = os.Getenv("mode")
	conf.BaseUrl = os.Getenv("url")
	// 獲取請求路徑
	path := r.URL.Path
	// 如果請求路徑以 "/d/" 開頭
	if strings.HasPrefix(path, conf.FileRoute) {
		control.D(w, r)
		return // 結束處理，確保不執行預設處理
	}
	switch path {
	case "/api":
		// 呼叫 control 套件中的 UploadImageAPI 處理函式
		control.Middleware(control.UploadImageAPI)(w, r)
	case "/pwd":
		control.Pwd(w, r)
	default:
		control.Middleware(control.Index)(w, r)
	}
}
