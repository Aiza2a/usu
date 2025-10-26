package store

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"time"

	_ "github.com/mattn/go-sqlite3" // 匯入 SQLite 驅動
)

var db *sql.DB

const shortIDLength = 6 // 您可以自訂短 ID 的長度
const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func init() {
	// 初始化隨機數生成器
	rand.New(rand.NewSource(time.Now().UnixNano()))
}

// InitDB 初始化資料庫連線
func InitDB(dataSourceName string) {
	var err error
	// 開啟或建立資料庫檔案
	db, err = sql.Open("sqlite3", dataSourceName)
	if err != nil {
		log.Fatalf("無法開啟資料庫: %v", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatalf("無法連線到資料庫: %v", err)
	}

	// 建立對應表
	createTableSQL := `CREATE TABLE IF NOT EXISTS shorts (
        "short_id" TEXT NOT NULL PRIMARY KEY,
        "file_id" TEXT NOT NULL UNIQUE
    );`
	if _, err = db.Exec(createTableSQL); err != nil {
		log.Fatalf("無法建立資料表: %v", err)
	}
	log.Println("資料庫初始化成功")
}

// generateShortID 產生一個隨機的短字串
func generateShortID() string {
	b := make([]byte, shortIDLength)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// GenerateAndSave 為一個 FileID 產生並儲存一個短 ID
// 它會先檢查 FileID 是否已存在，如果存在，則返回已有的 short_id
func GenerateAndSave(fileID string) (string, error) {
	// 1. 檢查 file_id 是否已經存在
	var existingShortID string
	err := db.QueryRow("SELECT short_id FROM shorts WHERE file_id = ?", fileID).Scan(&existingShortID)
	if err == nil {
		// 找到了，直接返回已有的 short_id
		return existingShortID, nil
	}
	if err != sql.ErrNoRows {
		// 發生了其他查詢錯誤
		return "", fmt.Errorf("查詢 file_id 時出錯: %v", err)
	}

	// 2. FileID 不存在，產生一個新的 short_id
	// 嘗試最多 10 次以避免潛在的無限迴圈（雖然碰撞機率極低）
	for i := 0; i < 10; i++ {
		shortID := generateShortID()

		// 檢查 short_id 是否碰撞
		var tempFileID string
		err := db.QueryRow("SELECT file_id FROM shorts WHERE short_id = ?", shortID).Scan(&tempFileID)
		
		if err == sql.ErrNoRows {
			// 這個 short_id 可用，插入新記錄
			_, insertErr := db.Exec("INSERT INTO shorts (short_id, file_id) VALUES (?, ?)", shortID, fileID)
			if insertErr != nil {
				return "", fmt.Errorf("插入新記錄時出錯: %v", insertErr)
			}
			// 成功，返回新的 short_id
			return shortID, nil
		}
		
		if err != nil {
			// 查詢 short_id 時發生錯誤
			return "", fmt.Errorf("檢查 short_id 碰撞時出錯: %v", err)
		}
		// 如果 err == nil，表示 short_id 發生碰撞，迴圈將繼續嘗試下一個
	}

	return "", fmt.Errorf("嘗試 10 次後仍無法產生唯一的 short_id")
}

// GetFileID 透過 short_id 查詢真實的 FileID
func GetFileID(shortID string) (string, error) {
	var fileID string
	err := db.QueryRow("SELECT file_id FROM shorts WHERE short_id = ?", shortID).Scan(&fileID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("找不到 short_id: %s", shortID)
		}
		return "", err
	}
	return fileID, nil
}