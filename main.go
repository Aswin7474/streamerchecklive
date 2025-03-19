package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sync"

	"github.com/joho/godotenv"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type YouTubeResponse struct {
	Items []struct {
		ID struct {
			ChannelID string `json:"channelId"`
		} `json:"id"`
	} `json:"items"`
}

func makeYTCall(ChannelName string, channelID string, wg *sync.WaitGroup, mu *sync.Mutex, results chan <- map[string]string) {
	defer wg.Done()
	mu.Lock()
	defer mu.Unlock()
	apiKey := os.Getenv("apiKey")

	ctx := context.Background()
	service, err := youtube.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatalf("Error creating YouTube service: %v", err)
	}

	// Search for live videos from a specific channel
	call := service.Search.List([]string{"id", "snippet"}).
		ChannelId(channelID).
		EventType("live"). // Filters only live videos
		Type("video").
		MaxResults(5)

	response, err := call.Do()
	if err != nil {
		log.Fatalf("API request error: %v", err)
	}

	// Print live video results
	
	for _, item := range response.Items {
		fmt.Println("Live Videos from the Channel:")
		fmt.Printf("Channel: %s, Title: %s, URL: https://www.youtube.com/watch?v=%s\n",
			ChannelName, item.Snippet.Title, item.Id.VideoId)
			 results <- map[string]string{ChannelName: item.Id.VideoId}
			 return

	}
	
	

	if len(response.Items) == 0 {
		// fmt.Printf("%s is not live.\n", ChannelName)
	}

}

func watchChannelMap(channelMap map[string]string) {
	var cname string
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Who would you like to watch: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}

	cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=" + channelMap[cname])
	cmd.Start()
}

func resolveChannelIDtoStreamLink(ChannelName string, channelID string) string {
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan map[string]string, 1)

	wg.Add(1)
	go makeYTCall(ChannelName, channelID, &wg, &mu, results)
	
	go func() {
		wg.Wait()
		close(results)
	}()

	result := <-results

	return result[ChannelName]
	
}

func watchChannelArray(channels [][]string) {
	var cname string
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Who would you like to watch: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}

	var id string

	for _, channel := range channels {
		if cname == channel[0] {
			id = channel[1]
		}
	}

	stream_link := resolveChannelIDtoStreamLink(cname, id)

	if len(stream_link) == 0 {
		log.Fatal("Streamer is not live\nExitting Program!")
	}

	cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=" + stream_link)
	cmd.Start()
	
}

func appendCSV(ChannelName string, cID string) {
	file, err := os.OpenFile("channels.csv", os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		log.Fatal("Couldn't open file to write.")
	}

	defer file.Close()

	writer := csv.NewWriter(file)

	newRecord := []string{ChannelName, cID}

	if err := writer.Write(newRecord); err != nil {
		fmt.Println("Something went wrong in writing to csv", err)
		return
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		fmt.Println("Something went wrong flushing the writer", err)
	}
}

func addChannel() ([]string, error){
	var cname string
	apiKey := os.Getenv("apiKey")
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Add Channel: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}


	apiURL := "https://www.googleapis.com/youtube/v3/search?" + url.Values{
		"part": {"snippet"},
		"q": {cname},
		"type": {"channel"},
		"key" : {apiKey},
	}.Encode()

	resp, err := http.Get(apiURL)
	if err != nil {
		log.Fatal("Could not look up channel", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var ytResponse YouTubeResponse
	if err := json.Unmarshal(body, &ytResponse); err != nil {
		return []string{}, err
	}

	if len(ytResponse.Items) > 0 {
		appendCSV(cname,  ytResponse.Items[0].ID.ChannelID)
	}

	return []string{}, fmt.Errorf("channel not found")
}

func openChannels() [][]string {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	return records

}

func checkLive() {
	channels := openChannels()

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan map[string]string, len(channels))

	for _, pair := range channels {
		wg.Add(1)
		go makeYTCall(pair[0], pair[1], &wg, &mu, results)
	}
	
	go func() {
		wg.Wait()
		close(results)
	}()

	finalResults := make(map[string]string)

	for result := range results {
		for key, value := range result {
			fmt.Printf("%s = %s\n", finalResults[key], value)
			finalResults[key] = value
		}
	}

	fmt.Println("Available Channels:")

	for key, _ := range finalResults {
		fmt.Println(key)
	}

	watchChannelMap(finalResults)

}

func watchStream() {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	for _, channel := range records {
		fmt.Println(channel[0])
	}

	watchChannelArray(records)
}


func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading API key. Check .env file")
	}
	// apiKey := os.Getenv("apiKey")
	// fmt.Printf("%s\n", apiKey)

	// channels := openChannels()
	// for _, pair := range channels {
	// 	fmt.Printf("%s, %s\n", pair[0], pair[1])
	// }

	// cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=uHZ3Qy2taIk")
	// cmd.Start()

	

	var choice int
	for choice != 4 {
		fmt.Println("1.Add Channel.\n2.Check who is live.\n3.Watch Stream\n4.Exit")
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			addChannel()
		case 2:
			checkLive()
		case 3:
			watchStream()
		case 4:
			os.Exit(0)
		}

	}
	
	

	

	
}
