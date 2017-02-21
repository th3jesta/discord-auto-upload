package main

import (
  "fmt"
  "strings"
  "github.com/pborman/getopt"
  "path/filepath"
  "os"
  "time"
  "net/http"
  "log"
  "io"
  "bytes"
  "mime/multipart"
  "encoding/json"
  "io/ioutil"
)

var current_version = "0.4"

var last_check     = time.Now()
var new_last_check = time.Now()

var webhook_url  string
var username     string

func main() {
  webhook_opt, path, watch, username_opt := parse_options()
  webhook_url = webhook_opt
  username    = username_opt

  check_path(path)
  check_updates()

  log.Print("Waiting for images to appear in ", path)

  // wander the path, forever
  for {
    err := filepath.Walk(path, check_file)
    if err != nil { log.Fatal("could not watch path", err) }
    last_check = new_last_check
    time.Sleep(time.Duration(watch)*time.Second)
  }
}

func check_path(path string) {
  src, err := os.Stat(path)
  if err != nil {
    log.Fatal(err)
  }
  if !src.IsDir() {
    log.Fatal(path, " is not a directory")
    os.Exit(1)
  }
}

func check_updates() {

  type GithubRelease struct {
    Html_url string
    Tag_name string
    Name     string
    Body     string
  }

  client := &http.Client{ Timeout: time.Second * 5 }
  resp, err := client.Get("https://api.github.com/repos/tardisx/discord-auto-upload/releases/latest")
  if (err != nil) {
    log.Fatal("could not check for updates:", err)
  }
  defer resp.Body.Close()
  body, err := ioutil.ReadAll(resp.Body)
  if (err != nil) {
    log.Fatal("could not check read update response")
  }

  var latest GithubRelease
  err = json.Unmarshal(body, &latest)

  if (err != nil) {
    log.Fatal("could not parse JSON: ", err)
  }

  if (current_version < latest.Tag_name) {
    fmt.Printf("You are currently on version %s, but version %s is available\n", current_version, latest.Tag_name)
    fmt.Println("----------- Release Info -----------")
    fmt.Println(latest.Body)
    fmt.Println("------------------------------------")
  }

}

func parse_options() (webhook_url string, path string, watch int, username string) {

  // Declare the flags to be used
  // helpFlag    := getopt.Bool('h', "display help")
  webhookFlag  := getopt.StringLong("webhook",   'w', "", "discord webhook URL")
  pathFlag     := getopt.StringLong("directory", 'd', "", "directory to scan, optional, defaults to current directory")
  watchFlag    := getopt.Int16Long ("watch",     's', 10, "time between scans")
  usernameFlag := getopt.StringLong("username",  'u', "", "username for the bot upload")
  helpFlag     := getopt.BoolLong  ("help",      'h', "help")
  versionFlag  := getopt.BoolLong  ("version",   'v', "show version")
  getopt.SetParameters("")

  getopt.Parse()

  if (*helpFlag) {
    getopt.PrintUsage(os.Stderr)
    os.Exit(0)
  }

  if (*versionFlag) {
    fmt.Printf("Version: %s\n", current_version)
    os.Exit(0)
  }

  if ! getopt.IsSet("directory") {
    *pathFlag = "./"
    log.Println("Defaulting to current directory")
  }

  if ! getopt.IsSet("webhook") {
    log.Fatal("ERROR: You must specify a --webhook URL")
  }

  return *webhookFlag, *pathFlag, int(*watchFlag), *usernameFlag
}

func check_file(path string, f os.FileInfo, err error) error {

  if f.ModTime().After(last_check) && f.Mode().IsRegular() {

    if file_eligible(path) {
      // process file
      process_file(path)
    }

    if new_last_check.Before(f.ModTime()) {
      new_last_check = f.ModTime()
    }
  }

  return nil
}

func file_eligible(file string) (bool) {
  extension := strings.ToLower(filepath.Ext(file))
  if extension == ".png" || extension == ".jpg" || extension == ".gif" {
    return true
  }
  return false
}

func process_file(file string) {
  log.Print("Uploading ", file)

  extraParams := map[string]string{ }

  if (username != "") {
    extraParams["username"] = username
  }

  type DiscordAPIResponseAttachment struct {
    Url string
    Proxy_url string
    Size  int
    Width int
    Height int
    Filename string
  }

  type DiscordAPIResponse struct {
    Attachments []DiscordAPIResponseAttachment
    Id int64 `json:",string"`
  }

  request, err := newfileUploadRequest(webhook_url, extraParams, "file", file)
  if err != nil {
    log.Fatal(err)
  }
  start := time.Now()
  client := &http.Client{ Timeout: time.Second * 30 }
  resp, err := client.Do(request)
  if err != nil {

    log.Fatal("Error performing request:", err)

  } else {

    if (resp.StatusCode != 200) {
      log.Print("Bad response from server:", resp.StatusCode)
      return
    }

    res_body, err := ioutil.ReadAll(resp.Body)
    if (err != nil) {
      log.Fatal("could not deal with body", err)
    }
    resp.Body.Close()

    var res DiscordAPIResponse
    err = json.Unmarshal(res_body, &res)

    if (err != nil) {
      log.Print("could not parse JSON: ", err)
      fmt.Println("Response was:", string(res_body[:]))
      return
    }
    if (len(res.Attachments) < 1) {
      log.Print("bad response - no attachments?")
      return
    }
    var a = res.Attachments[0]
    elapsed := time.Since(start)
    rate := float64(a.Size) / elapsed.Seconds() / 1024.0

    log.Printf("Uploaded to %s %dx%d", a.Url, a.Width, a.Height)
    log.Printf("id: %d, %d bytes transferred in %.2f seconds (%.2f KiB/s)", res.Id, a.Size, elapsed.Seconds(), rate)
  }

}

func newfileUploadRequest(uri string, params map[string]string, paramName, path string) (*http.Request, error) {
  file, err := os.Open(path)
  if err != nil {
      return nil, err
  }
  defer file.Close()

  body := &bytes.Buffer{}
  writer := multipart.NewWriter(body)
  part, err := writer.CreateFormFile(paramName, filepath.Base(path))
  if err != nil {
    return nil, err
  }
  _, err = io.Copy(part, file)

  for key, val := range params {
    _ = writer.WriteField(key, val)
  }
  err = writer.Close()
  if err != nil {
    return nil, err
  }

  req, err := http.NewRequest("POST", uri, body)
  req.Header.Set("Content-Type", writer.FormDataContentType())
  return req, err
}
