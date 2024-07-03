package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gameraccoon/one-time-share/database"
	"github.com/google/uuid"
	"log"
	"net/http"
	"os"
	"time"
)

var globalStaticData StaticData

type StaticData struct {
	// static html page that will be sent when accessing '/' path
	defaultIndexHtml string
	// html page that will be sent when accessing '/shared/' path
	sharedHtml []byte
	// limits of the default user that we use with '/' page (more users can be added to the db manually)
	defaultUserLimits UserLimits
	// database connection, always open while the server is running
	database *database.OneTimeShareDb
	// static configuration read from the config file
	config Config
}

type UserLimits struct {
	// how long the message is going to be available (zero means no limit)
	RetentionLimitMinutes int
	// max size of the message in bytes (zero means no limit)
	MaxMessageSizeBytes int
	// how often a new message can be created (zero means no limit)
	MessageCreationLimitMinutes int
}

type Config struct {
	// port that the server will listen to
	Port string
	// path to the database file
	DatabasePath string

	// if true, http will be used instead of https
	ForceUnprotectedHttp bool
	// path to the certificate file (ignored if ForceUnprotectedHttp is true)
	CertPath string
	// path to the key file (ignored if ForceUnprotectedHttp is true)
	KeyPath string

	// default retention limit in minutes
	DefaultRetentionLimitMinutes int
	// default max message size in bytes
	DefaultMaxMessageSizeBytes int
	// default message creation limit in minutes
	DefaultMessageCreationLimitMinutes int
}

func readConfig(filePath string) error {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error while reading config file: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(file))
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&globalStaticData.config)
	if err != nil {
		return fmt.Errorf("error while parsing config file: %w", err)
	}

	return nil
}

func setDefaultUserLimits() error {
	globalStaticData.database.SetUserLimits("default", globalStaticData.config.DefaultRetentionLimitMinutes, globalStaticData.config.DefaultMaxMessageSizeBytes, globalStaticData.config.DefaultMessageCreationLimitMinutes)

	globalStaticData.defaultUserLimits = UserLimits{
		RetentionLimitMinutes:       globalStaticData.config.DefaultRetentionLimitMinutes,
		MaxMessageSizeBytes:         globalStaticData.config.DefaultMaxMessageSizeBytes,
		MessageCreationLimitMinutes: globalStaticData.config.DefaultMessageCreationLimitMinutes,
	}

	return nil
}

func setupStaticPages() error {
	{
		// read the index.html file
		indexHtml, err := os.ReadFile("index.html")
		if err != nil {
			log.Fatal("Error while reading index.html: ", err)
			return err
		}

		indexHtml = bytes.ReplaceAll(indexHtml, []byte("{{.MessageLimitBytes}}"), []byte(fmt.Sprintf("%d", globalStaticData.defaultUserLimits.MaxMessageSizeBytes)))
		indexHtml = bytes.ReplaceAll(indexHtml, []byte("{{.RetentionLimitMinutes}}"), []byte(fmt.Sprintf("%d", globalStaticData.defaultUserLimits.RetentionLimitMinutes)))

		globalStaticData.defaultIndexHtml = string(indexHtml)
	}

	{
		// read the shared.html file
		sharedHtml, err := os.ReadFile("shared.html")
		if err != nil {
			log.Fatal("Error while reading shared.html: ", err)
			return err
		}

		globalStaticData.sharedHtml = sharedHtml
	}
	return nil
}

func homePage(w http.ResponseWriter, r *http.Request) {
	// check if the request is a GET request
	if r.Method != "GET" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, err := fmt.Fprint(w, globalStaticData.defaultIndexHtml)
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func createNewMessage(w http.ResponseWriter, r *http.Request) {
	// check if the request is a POST request
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return
	}

	userToken := r.Form.Get("user_token")
	if userToken == "" {
		http.Error(w, "user_token is empty", http.StatusBadRequest)
		return
	}

	if !globalStaticData.database.DoesUserExist(userToken) {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	retentionLimitMinutes, maxSizeBytes, messageCreationLimitMinutes := globalStaticData.database.GetUserLimits(userToken)

	if messageCreationLimitMinutes > 0 {
		// check if the user can create a new message
		lastCreationTime := globalStaticData.database.GetUserLastMessageCreationTime(userToken)
		// if there was a message created before
		if lastCreationTime > 0 {
			timePassedFromLastCreation := time.Now().Sub(time.Unix(lastCreationTime, 0))
			if timePassedFromLastCreation.Minutes() < float64(messageCreationLimitMinutes) {
				minutesLeft := messageCreationLimitMinutes - int(timePassedFromLastCreation.Minutes())
				http.Error(w, "Message creation limit reached. Wait for "+fmt.Sprintf("%d", minutesLeft)+" minute(s) and repeat", http.StatusBadRequest)
				return
			}
		}
	}

	messageData := r.Form.Get("message_data")
	if messageData == "" {
		http.Error(w, "message_data is empty", http.StatusBadRequest)
		return
	}

	if maxSizeBytes > 0 && len(messageData) > maxSizeBytes {
		http.Error(w, "Message is too big", http.StatusBadRequest)
		return
	}

	requestedRetentionLimitText := r.Form.Get("retention")
	requestedRetentionLimitMinutes := -1
	if requestedRetentionLimitText != "" {
		requestedRetentionLimitMinutes, err = fmt.Sscanf(requestedRetentionLimitText, "%d", &requestedRetentionLimitMinutes)
		if err != nil {
			http.Error(w, "Can't parse retention limit", http.StatusBadRequest)
			return
		}
	}

	if requestedRetentionLimitMinutes < 0 {
		http.Error(w, "Invalid retention limit", http.StatusBadRequest)
	}

	if requestedRetentionLimitMinutes == 0 && retentionLimitMinutes > 0 {
		http.Error(w, "Can't set unlimited retention limit, not allowed", http.StatusBadRequest)
	}

	if requestedRetentionLimitMinutes > 0 && retentionLimitMinutes > 0 && requestedRetentionLimitMinutes > retentionLimitMinutes {
		http.Error(w, "Requested retention limit is bigger than allowed", http.StatusBadRequest)
		return
	}

	globalStaticData.database.SetUserLastMessageCreationTime(userToken, time.Now().Unix())

	messageToken := uuid.New().String()
	var expireTimestamp int64 = 0
	if requestedRetentionLimitMinutes > 0 {
		expireTimestamp = time.Now().Add(time.Duration(requestedRetentionLimitMinutes) * time.Minute).Unix()
	}

	err = globalStaticData.database.SaveMessage(messageToken, expireTimestamp, messageData)
	if err != nil {
		log.Println("Error while saving message: ", err)
		http.Error(w, "Can't save message. Try again", http.StatusInternalServerError)
		return
	}

	// to ensure the message is sent encrypted we need to use https
	urlToShare := "https://" + r.Host + "/shared/" + messageToken

	_, err = fmt.Fprint(w, urlToShare)
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func sharedPage(w http.ResponseWriter, r *http.Request) {
	// check if the request is a GET request
	if r.Method != "GET" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// read the token from the URL
	token := r.URL.Path[len("/shared/"):]
	if token == "" {
		http.Error(w, "Token is empty", http.StatusBadRequest)
		return
	}

	htmlResponse := globalStaticData.sharedHtml
	htmlResponse = bytes.ReplaceAll(htmlResponse, []byte("{{.MessageToken}}"), []byte(token))

	_, err := fmt.Fprint(w, string(htmlResponse))
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func tryConsumeExistingMessage(w http.ResponseWriter, r *http.Request) {
	// check if the request is a POST request
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Can't parse form", http.StatusBadRequest)
		return
	}

	messageToken := r.Form.Get("message_token")
	if messageToken == "" {
		http.Error(w, "message_token is empty", http.StatusBadRequest)
		return
	}

	message, expireTimestamp := globalStaticData.database.TryConsumeMessage(messageToken)

	// we don't distinguish between not found and expired messages since this wouldn't be reliable
	if message != nil && (expireTimestamp != 0 && time.Now().Unix() < expireTimestamp) {
		// sanitize the message to escape newlines, quotes and other special characters
		sanitizedMessage := ""
		for _, char := range *message {
			if char == '\n' {
				sanitizedMessage += "\\n"
			} else if char == '"' {
				sanitizedMessage += "\\\""
			} else if char == '\\' {
				sanitizedMessage += "\\\\"
			} else {
				sanitizedMessage += string(char)
			}
		}
		_, err = fmt.Fprintf(w, `{"status": "ok", "message": "%s"}`, sanitizedMessage)
		if err != nil {
			log.Println("Error while writing response: ", err)
			return
		}
	} else {
		_, err = fmt.Fprint(w, `{"status": "not-found"}`)
		if err != nil {
			log.Println("Error while writing response: ", err)
			return
		}
	}
}

func getLimits(w http.ResponseWriter, r *http.Request) {
	// check if the request is a GET request
	if r.Method != "GET" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// write messageLimitBytes and retentionLimitMinutes to the JSON response
	messageLimitBytes := globalStaticData.defaultUserLimits.MaxMessageSizeBytes
	retentionLimitMinutes := globalStaticData.defaultUserLimits.RetentionLimitMinutes

	_, err := fmt.Fprintf(w, `{"messageLimitBytes": %d, "retentionLimitMinutes": %d}`, messageLimitBytes, retentionLimitMinutes)
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func handleRequests() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/save", createNewMessage)
	http.HandleFunc("/consume", tryConsumeExistingMessage)
	http.HandleFunc("/limits", getLimits)
	http.HandleFunc("/shared/", sharedPage)

	addr := ":" + globalStaticData.config.Port
	if globalStaticData.config.ForceUnprotectedHttp {
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		cert := globalStaticData.config.CertPath
		key := globalStaticData.config.KeyPath
		err := http.ListenAndServeTLS(addr, cert, key, nil)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func startOldMessagesCleaner(db *database.OneTimeShareDb) {
	clearFrequency := time.Minute

	db.ClearExpiredMessages(time.Now().Unix())

	go func() {
		for {
			time.Sleep(clearFrequency)

			// this won't prevent from a race when trying to get data from already closed connection,
			// but it is a way to gracefully stop the thread
			if !db.IsConnectionOpened() {
				break
			}

			db.ClearExpiredMessages(time.Now().Unix())
		}
	}()
}

func main() {
	err := readConfig("app-config.json")
	if err != nil {
		log.Fatal("Error while reading config: ", err)
		return
	}

	db, err := database.ConnectDb(globalStaticData.config.DatabasePath)
	defer db.Disconnect()

	if err != nil {
		log.Fatal("Can't connect to database: ", err)
		return
	}

	database.UpdateVersion(db)
	globalStaticData.database = db

	err = setDefaultUserLimits()
	if err != nil {
		log.Fatal("Error while reading user defaultUserLimits: ", err)
		return
	}

	err = setupStaticPages()
	if err != nil {
		log.Fatal("Error while setting up static pages: ", err)
		return
	}

	startOldMessagesCleaner(db)
	handleRequests()
}
