package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
)

// index.html that we are going to show to the user
var globalStaticData StaticData

type UserLimits struct {
	// how long the share is going to be available (zero means no limit)
	RetentionLimitMinutes int
	// max size of the share in bytes (zero means no limit)
	MaxSizeBytes int
	// how often a new share can be created (zero means no limit)
	ShareCreationLimitMinutes int
}

type Share struct {
	Data string
}

type StaticData struct {
	// index.html that we are going to show to the user
	indexHtml string
	limits    UserLimits
}

func readUserLimits() error {
	globalStaticData.limits = UserLimits{
		RetentionLimitMinutes: 60,
		MaxSizeBytes:          1000,
	}

	return nil
}

func setupStaticPages() error {
	// read the index.html file
	indexHtml, err := os.ReadFile("index.html")
	if err != nil {
		log.Fatal("Error while reading index.html: ", err)
		return err
	}

	indexHtml = bytes.ReplaceAll(indexHtml, []byte("{{.ShareLimitBytes}}"), []byte(fmt.Sprintf("%d", globalStaticData.limits.MaxSizeBytes)))
	indexHtml = bytes.ReplaceAll(indexHtml, []byte("{{.RetentionLimitMinutes}}"), []byte(fmt.Sprintf("%d", globalStaticData.limits.RetentionLimitMinutes)))

	globalStaticData.indexHtml = string(indexHtml)

	return nil
}

func homePage(w http.ResponseWriter, r *http.Request) {
	// check if the request is a GET request
	if r.Method != "GET" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, err := fmt.Fprintf(w, globalStaticData.indexHtml)
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func createNewShare(w http.ResponseWriter, r *http.Request) {
	// check if the request is a POST request
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// return test data
	_, err := fmt.Fprintf(w, "Test share data")
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func tryReadExistingShare(w http.ResponseWriter, r *http.Request) {
	_, err := fmt.Fprintf(w, "Welcome to the HomePage!")
	if err != nil {
		return
	}
	fmt.Println("Endpoint Hit: homePage")
}

func getLimits(w http.ResponseWriter, r *http.Request) {
	// check if the request is a GET request
	if r.Method != "GET" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// write shareLimitBytes and retentionLimitMinutes to the JSON response
	shareLimitBytes := globalStaticData.limits.MaxSizeBytes
	retentionLimitMinutes := globalStaticData.limits.RetentionLimitMinutes

	_, err := fmt.Fprintf(w, `{"shareLimitBytes": %d, "retentionLimitMinutes": %d}`, shareLimitBytes, retentionLimitMinutes)
	if err != nil {
		log.Println("Error while writing response: ", err)
		return
	}
}

func handleRequests() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/share", createNewShare)
	http.HandleFunc("/read", tryReadExistingShare)
	http.HandleFunc("/get-limits", getLimits)
	log.Fatal(http.ListenAndServe(":10000", nil))
}

func main() {
	err := readUserLimits()
	if err != nil {
		log.Fatal("Error while reading user limits: ", err)
		return
	}

	err = setupStaticPages()
	if err != nil {
		log.Fatal("Error while setting up static pages: ", err)
		return
	}

	handleRequests()
}
