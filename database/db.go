package database

import (
	"database/sql"
	"fmt"
	dbBase "github.com/gameraccoon/telegram-bot-skeleton/database"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"sync"
)

type OneTimeShareDb struct {
	db    dbBase.Database
	mutex sync.Mutex
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func ConnectDb(path string) (database *OneTimeShareDb, err error) {
	database = &OneTimeShareDb{}

	err = database.db.Connect(path)

	if err != nil {
		return
	}

	database.db.Exec("CREATE TABLE IF NOT EXISTS" +
		" global_vars(name TEXT PRIMARY KEY" +
		",integer_value INTEGER" +
		",string_value TEXT" +
		")")

	database.db.Exec("CREATE TABLE IF NOT EXISTS" +
		" users(id INTEGER NOT NULL PRIMARY KEY" +
		",token TEXT NOT NULL" +
		",retention_limit_minutes INTEGER NOT NULL" +
		",max_size_bytes INTEGER NOT NULL" +
		",message_creation_limit_minutes INTEGER NOT NULL" +
		",last_message_creation_timestamp INTEGER" +
		")")

	database.db.Exec("CREATE TABLE IF NOT EXISTS" +
		" messages(id INTEGER NOT NULL PRIMARY KEY" +
		",message_token TEXT NOT NULL" +
		",expire_timestamp INTEGER NOT NULL" +
		",data TEXT NOT NULL" +
		")")

	database.db.Exec("CREATE INDEX IF NOT EXISTS" +
		" token_index ON users(token)")

	database.db.Exec("CREATE INDEX IF NOT EXISTS" +
		" token_index ON messages(token)")

	return
}

func (database *OneTimeShareDb) IsConnectionOpened() bool {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	return database.db.IsConnectionOpened()
}

func (database *OneTimeShareDb) Disconnect() {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	database.db.Disconnect()
}

func (database *OneTimeShareDb) GetDatabaseVersion() (version string) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	rows, err := database.db.Query("SELECT string_value FROM global_vars WHERE name='version'")

	if err != nil {
		log.Fatal(err.Error())
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	if rows.Next() {
		err := rows.Scan(&version)
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		// that means it's a new clean database
		version = latestVersion
	}

	return
}

func (database *OneTimeShareDb) SetDatabaseVersion(version string) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	database.db.Exec("DELETE FROM global_vars WHERE name='version'")

	safeVersion := dbBase.SanitizeString(version)
	database.db.Exec(fmt.Sprintf("INSERT INTO global_vars (name, string_value) VALUES ('version', '%s')", safeVersion))
}

func (database *OneTimeShareDb) SetUserLimits(token string, retentionLimitMinutes int, maxSizeBytes int, messageCreationLimitMinutes int) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	database.db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO users (token, retention_limit_minutes, max_size_bytes, message_creation_limit_minutes) VALUES ('%s', %d, %d, %d)", dbBase.SanitizeString(token), retentionLimitMinutes, maxSizeBytes, messageCreationLimitMinutes))
}

func (database *OneTimeShareDb) GetUserLimits(token string) (retentionLimitMinutes int, maxSizeBytes int, messageCreationLimitMinutes int) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	rows, err := database.db.Query(fmt.Sprintf("SELECT retention_limit_minutes, max_size_bytes, message_creation_limit_minutes FROM users WHERE token='%s'", dbBase.SanitizeString(token)))
	if err != nil {
		log.Fatal(err.Error())
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	if rows.Next() {
		err := rows.Scan(&retentionLimitMinutes, &maxSizeBytes, &messageCreationLimitMinutes)
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		err = rows.Err()
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("can't find limits for token '%s'", token)
	}

	return
}

func (database *OneTimeShareDb) DoesUserExist(token string) bool {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	rows, err := database.db.Query(fmt.Sprintf("SELECT id FROM users WHERE token='%s'", dbBase.SanitizeString(token)))
	if err != nil {
		log.Fatal(err.Error())
		return false
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	return rows.Next()
}

func (database *OneTimeShareDb) RemoveUserByToken(token string) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	database.db.Exec(fmt.Sprintf("DELETE FROM users WHERE token='%s'", dbBase.SanitizeString(token)))
}

func (database *OneTimeShareDb) SetUserLastMessageCreationTime(token string, timestamp int64) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	database.db.Exec(fmt.Sprintf("UPDATE users SET last_message_creation_timestamp=%d WHERE token='%s'", timestamp, dbBase.SanitizeString(token)))
}

func (database *OneTimeShareDb) GetUserLastMessageCreationTime(token string) (timestamp int64) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	rows, err := database.db.Query(fmt.Sprintf("SELECT last_message_creation_timestamp FROM users WHERE token='%s' AND last_message_creation_timestamp IS NOT NULL", dbBase.SanitizeString(token)))
	if err != nil {
		log.Fatal(err.Error())
		return
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	if rows.Next() {
		err := rows.Scan(&timestamp)
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		return 0
	}

	return
}

func (database *OneTimeShareDb) SaveMessage(messageToken string, expireTimestamp int64, data string) error {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	sanitizedToken := dbBase.SanitizeString(messageToken)

	// check if we have any messages with this message_token
	rows, err := database.db.Query(fmt.Sprintf("SELECT id FROM messages WHERE message_token='%s'", sanitizedToken))
	if err != nil {
		log.Fatal(err.Error())
		return err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	if rows.Next() {
		// we already have a message with this message_token
		return fmt.Errorf("message with message_token '%s' already exists", messageToken)
	}

	err = rows.Close()
	if err != nil {
		log.Fatal(err)
	}

	database.db.Exec(fmt.Sprintf("INSERT INTO messages (message_token, expire_timestamp, data) VALUES ('%s', %d, '%s')", sanitizedToken, expireTimestamp, dbBase.SanitizeString(data)))
	return nil
}

func (database *OneTimeShareDb) TryConsumeMessage(messageToken string) (data *string) {
	database.mutex.Lock()
	defer database.mutex.Unlock()

	rows, err := database.db.Query(fmt.Sprintf("SELECT id, data FROM messages WHERE message_token='%s'", dbBase.SanitizeString(messageToken)))
	if err != nil {
		log.Fatal(err.Error())
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Fatal(err.Error())
		}
	}(rows)

	id := -1
	if rows.Next() {
		err := rows.Scan(&id, &data)
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		return nil
	}

	err = rows.Close()
	if err != nil {
		log.Fatal(err)
	}

	if id != -1 {
		database.db.Exec(fmt.Sprintf("DELETE FROM messages WHERE id=%d", id))
	} else {
		log.Fatalf("can't find message for message_token '%s'", messageToken)
	}

	return
}
