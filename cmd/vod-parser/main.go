package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
)

type Result struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Error  string `json:"error,omitempty"`
}

func main() {
	jsonMode := flag.Bool("j", false, "JSON mode")
	helpJSON := flag.Bool("help-json", false, "Show capabilities in JSON format")
	flag.Parse()

	if *helpJSON {
		fmt.Println(`{
			"name": "vod-parser",
			"description": "Minimalist parser for MacCMS player_aaaa variable.",
			"capabilities": ["extract-m3u8"],
			"usage": "cat input.json | bin/vod-parser [-j]"
		}`)
		os.Exit(0)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "System Error: %v\n", err)
		os.Exit(1)
	}

	var data map[string]interface{}
	err = json.Unmarshal(input, &data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Contract/Validation Error: Invalid JSON input\n")
		os.Exit(2)
	}

	html, ok := data["html"].(string)
	if !ok {
		fmt.Fprintf(os.Stderr, "Contract/Validation Error: No html field\n")
		os.Exit(2)
	}

	// Regex to find the player_aaaa variable
	re := regexp.MustCompile(`var player_aaaa = ({.*?});`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		fmt.Println(`{"status":"unavailable","error":"player_aaaa not found"}`)
		os.Exit(4)
	}

	var playerData map[string]interface{}
	err = json.Unmarshal([]byte(match[1]), &playerData)
	if err != nil {
		fmt.Println(`{"status":"unavailable","error":"Failed to parse player_aaaa JSON"}`)
		os.Exit(4)
	}

	url, ok := playerData["url"].(string)
	if !ok {
		fmt.Println(`{"status":"unavailable","error":"URL not found in player_aaaa"}`)
		os.Exit(4)
	}

	if *jsonMode {
		fmt.Printf(`{"status":"ok","url":"%s"}`+"\n", url)
	} else {
		fmt.Println(url)
	}
	os.Exit(0)
}
